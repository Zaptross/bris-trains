package gtfs

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultStaticURL is Translink SEQ's static GTFS zip.
const DefaultStaticURL = "https://gtfsrt.api.translink.com.au/GTFS/SEQ_GTFS.zip"

const (
	metaName    = "meta.json"
	indexPrefix = "index-"
	indexSuffix = ".gob"
)

// renameWindow bounds how long atomicWrite retries a rename that another
// process blocks by holding the target open (Windows). A variable for tests.
var renameWindow = 2 * time.Second

// Cache manages the on-disk index cache shared by every `trains` process (SPEC §3.3, §4.3).
//
// The directory holds meta.json (ETag, last check time, gob file name) and
// one index-<etag>.gob. Every write goes to a temp file in Dir and is renamed
// into place, so no process ever reads a partial file. A Cache holds no
// in-memory state; distinct processes coordinate only through Dir.
type Cache struct {
	Dir    string           // e.g. os.UserCacheDir()/trains
	URL    string           // static GTFS zip URL; default DefaultStaticURL when empty
	Client *http.Client     // default http.DefaultClient
	MaxAge time.Duration    // how long a successful check is trusted; default 1h
	Logger *slog.Logger     // default slog.Default()
	Now    func() time.Time // default time.Now; for tests
}

// meta is the JSON record beside the gob.
type meta struct {
	ETag      string    `json:"etag"`
	CheckedAt time.Time `json:"checkedAt"`
	File      string    `json:"file"`
}

func (c *Cache) url() string {
	if c.URL == "" {
		return DefaultStaticURL
	}
	return c.URL
}

func (c *Cache) client() *http.Client {
	if c.Client == nil {
		return http.DefaultClient
	}
	return c.Client
}

func (c *Cache) maxAge() time.Duration {
	if c.MaxAge <= 0 {
		return time.Hour
	}
	return c.MaxAge
}

func (c *Cache) logger() *slog.Logger {
	if c.Logger == nil {
		return slog.Default()
	}
	return c.Logger
}

func (c *Cache) now() time.Time {
	if c.Now == nil {
		return time.Now()
	}
	return c.Now()
}

// Load returns the cached index, checking upstream first if the last check is older than MaxAge
// (or there is no cache). Conditional GET with If-None-Match; 304 -> keep gob, record check time;
// 200 -> download to a temp file in Dir, Parse, write gob atomically, update metadata, delete the zip.
// Network/HTTP failure with a usable gob -> log a warning and return the gob. No gob and no network -> error.
func (c *Cache) Load(ctx context.Context) (*Index, error) {
	m, cached := c.readCached()
	if cached != nil && c.now().Sub(m.CheckedAt) < c.maxAge() {
		return cached, nil
	}
	ix, _, err := c.Refresh(ctx, cached)
	if err != nil {
		if cached != nil {
			c.logger().Warn("gtfs: static feed check failed; using cached index", "etag", cached.ETag, "err", err)
			return cached, nil
		}
		return nil, fmt.Errorf("load static index: no usable cache in %s: %w", c.Dir, err)
	}
	return ix, nil
}

// Refresh forces a conditional check against current.ETag. Returns (newIndex, true, nil) when the feed changed,
// (current, false, nil) on 304.
//
// current may be nil, which makes the request unconditional. On any failure
// it returns (current, false, err).
func (c *Cache) Refresh(ctx context.Context, current *Index) (*Index, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(), nil)
	if err != nil {
		return current, false, fmt.Errorf("static feed request: %w", err)
	}
	if current != nil && current.ETag != "" {
		req.Header.Set("If-None-Match", current.ETag)
	}
	resp, err := c.client().Do(req)
	if err != nil {
		return current, false, fmt.Errorf("static feed fetch: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotModified && current != nil:
		c.recordCheck(current.ETag)
		return current, false, nil
	case resp.StatusCode != http.StatusOK:
		return current, false, fmt.Errorf("static feed fetch: unexpected status %s", resp.Status)
	}

	ix, err := c.download(resp.Body)
	if err != nil {
		return current, false, err
	}
	ix.ETag = resp.Header.Get("ETag")
	c.store(ix)
	return ix, true, nil
}

// download streams the zip body to a temp file in Dir, parses it, and
// removes the file.
func (c *Cache) download(body io.Reader) (*Index, error) {
	if err := os.MkdirAll(c.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	f, err := os.CreateTemp(c.Dir, "download-*.zip")
	if err != nil {
		return nil, fmt.Errorf("create download file: %w", err)
	}
	defer func() {
		f.Close()
		os.Remove(f.Name())
	}()
	n, err := io.Copy(f, body)
	if err != nil {
		return nil, fmt.Errorf("download static feed: %w", err)
	}
	ix, err := Parse(f, n)
	if err != nil {
		return nil, fmt.Errorf("parse static feed: %w", err)
	}
	return ix, nil
}

// store writes ix's gob and then the metadata pointing at it. A failure
// leaves the previous cache in place and is only logged: the caller still has
// the freshly parsed index in memory.
func (c *Cache) store(ix *Index) {
	file := indexPrefix + sanitize(ix.ETag) + indexSuffix
	if err := atomicWrite(c.Dir, file, ix.Encode); err != nil {
		c.logger().Warn("gtfs: could not write index cache; keeping previous", "file", file, "err", err)
		return
	}
	m := meta{ETag: ix.ETag, CheckedAt: c.now(), File: file}
	if err := writeMeta(c.Dir, m); err != nil {
		c.logger().Warn("gtfs: could not write cache metadata; keeping previous", "err", err)
		os.Remove(filepath.Join(c.Dir, file))
		return
	}
	old, _ := filepath.Glob(filepath.Join(c.Dir, indexPrefix+"*"+indexSuffix))
	for _, p := range old {
		if filepath.Base(p) != file {
			os.Remove(p) // may fail while another process reads it; harmless
		}
	}
}

// recordCheck stamps the check time after a 304, keeping the gob reference.
func (c *Cache) recordCheck(etag string) {
	m, err := readMeta(c.Dir)
	if err != nil || m.ETag != etag {
		// Another process moved the cache on, or there is none; describe ours.
		m = meta{ETag: etag, File: indexPrefix + sanitize(etag) + indexSuffix}
		if _, err := os.Stat(filepath.Join(c.Dir, m.File)); err != nil {
			return // nothing on disk to vouch for
		}
	}
	m.CheckedAt = c.now()
	if err := writeMeta(c.Dir, m); err != nil {
		c.logger().Warn("gtfs: could not record static feed check", "err", err)
	}
}

// readCached returns the metadata and the index it points at, or a nil index
// when there is no usable cache.
func (c *Cache) readCached() (meta, *Index) {
	m, err := readMeta(c.Dir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.logger().Warn("gtfs: unreadable cache metadata", "err", err)
		}
		return meta{}, nil
	}
	b, err := os.ReadFile(filepath.Join(c.Dir, m.File))
	if err != nil {
		c.logger().Warn("gtfs: unreadable index cache", "file", m.File, "err", err)
		return meta{}, nil
	}
	ix, err := Decode(bytes.NewReader(b))
	if err != nil {
		c.logger().Warn("gtfs: corrupt or incompatible index cache", "file", m.File, "err", err)
		return meta{}, nil
	}
	return m, ix
}

func readMeta(dir string) (meta, error) {
	var m meta
	b, err := os.ReadFile(filepath.Join(dir, metaName))
	if err != nil {
		return m, fmt.Errorf("read cache metadata: %w", err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("parse cache metadata: %w", err)
	}
	if m.File == "" || filepath.Base(m.File) != m.File {
		return m, fmt.Errorf("parse cache metadata: bad file name %q", m.File)
	}
	return m, nil
}

func writeMeta(dir string, m meta) error {
	return atomicWrite(dir, metaName, func(w io.Writer) error {
		return json.NewEncoder(w).Encode(m)
	})
}

// atomicWrite writes dir/name via a synced temp file in dir renamed into
// place. While another process holds the target open, Windows refuses the
// rename; it is retried with backoff for up to renameWindow, after which the
// temp file is removed and the old target is left untouched.
func atomicWrite(dir, name string, write func(io.Writer) error) (err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create cache dir: %w", err)
	}
	f, err := os.CreateTemp(dir, name+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp for %s: %w", name, err)
	}
	tmp := f.Name()
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(tmp)
		}
	}()
	bw := bufio.NewWriterSize(f, 1<<16)
	if err := write(bw); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := bw.Flush(); err != nil {
		return fmt.Errorf("write %s: %w", name, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}

	target := filepath.Join(dir, name)
	deadline := time.Now().Add(renameWindow)
	for backoff := 5 * time.Millisecond; ; backoff = min(backoff*2, 200*time.Millisecond) {
		err = os.Rename(tmp, target)
		if err == nil {
			return nil
		}
		if time.Now().Add(backoff).After(deadline) {
			return fmt.Errorf("replace %s: %w", name, err)
		}
		time.Sleep(backoff)
	}
}

// sanitize turns an ETag into a file-name fragment.
func sanitize(etag string) string {
	s := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '.':
			return r
		}
		return '_'
	}, etag)
	s = strings.Trim(s, "_.")
	if len(s) > 64 {
		s = s[:64]
	}
	if s == "" {
		return "noetag"
	}
	return s
}
