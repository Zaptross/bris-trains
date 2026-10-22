package gtfs

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// feedServer serves the fixture zip with a fixed ETag, honouring If-None-Match.
type feedServer struct {
	*httptest.Server
	etag     string
	fail     atomic.Bool
	requests atomic.Int32
	mu       sync.Mutex
	inm      []string // If-None-Match of each request
}

func newFeedServer(t *testing.T) *feedServer {
	t.Helper()
	body, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatal(err)
	}
	s := &feedServer{etag: `"0x8DF2FA80250FEA8"`}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests.Add(1)
		s.mu.Lock()
		s.inm = append(s.inm, r.Header.Get("If-None-Match"))
		s.mu.Unlock()
		if s.fail.Load() {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		if r.Header.Get("If-None-Match") == s.etag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", s.etag)
		w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *feedServer) lastINM() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inm[len(s.inm)-1]
}

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newCache(dir, url string, clk *clock) *Cache {
	return &Cache{Dir: dir, URL: url, MaxAge: time.Hour, Logger: quietLogger(), Now: clk.now}
}

func TestCacheLifecycle(t *testing.T) {
	srv := newFeedServer(t)
	dir := t.TempDir()
	clk := &clock{t: time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)}
	c := newCache(dir, srv.URL, clk)
	ctx := context.Background()

	// 1. Cold: downloads, parses, writes gob + meta, removes the zip.
	ix, err := c.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if srv.requests.Load() != 1 || srv.lastINM() != "" {
		t.Fatalf("cold load: %d requests, If-None-Match %q", srv.requests.Load(), srv.lastINM())
	}
	if ix.ETag != srv.etag || len(ix.Trips) != 1508 {
		t.Fatalf("cold load: etag %q, %d trips", ix.ETag, len(ix.Trips))
	}
	m, err := readMeta(dir)
	if err != nil {
		t.Fatal(err)
	}
	if m.ETag != srv.etag || !m.CheckedAt.Equal(clk.t) || m.File != "index-0x8DF2FA80250FEA8.gob" {
		t.Fatalf("meta = %+v", m)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("cache dir holds %v, want only meta and gob", names)
	}

	// 2. Warm, within MaxAge: no request at all.
	clk.t = clk.t.Add(59 * time.Minute)
	ix2, err := c.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if srv.requests.Load() != 1 {
		t.Fatalf("warm load within MaxAge made a request")
	}
	if ix2.ETag != srv.etag || len(ix2.StopTimes) != len(ix.StopTimes) {
		t.Fatal("warm load returned a different index")
	}

	// 3. Past MaxAge: conditional GET, 304, check time recorded.
	clk.t = clk.t.Add(2 * time.Minute)
	ix3, err := c.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if srv.requests.Load() != 2 || srv.lastINM() != srv.etag {
		t.Fatalf("stale load: %d requests, If-None-Match %q", srv.requests.Load(), srv.lastINM())
	}
	if len(ix3.Trips) != 1508 {
		t.Fatal("304 load lost the index")
	}
	if m, _ := readMeta(dir); !m.CheckedAt.Equal(clk.t) {
		t.Fatalf("304 did not record check time: %v", m.CheckedAt)
	}
	if _, err := c.Load(ctx); err != nil || srv.requests.Load() != 2 {
		t.Fatalf("load after 304 re-checked: %d requests, %v", srv.requests.Load(), err)
	}

	// 4. Refresh against the current ETag: 304, unchanged.
	got, changed, err := c.Refresh(ctx, ix3)
	if err != nil || changed || got != ix3 {
		t.Fatalf("Refresh = %p, %v, %v; want current, false, nil", got, changed, err)
	}

	// 5. Upstream changed: Refresh re-downloads and replaces the gob.
	srv.etag = `"v2"`
	got, changed, err = c.Refresh(ctx, ix3)
	if err != nil || !changed || got.ETag != `"v2"` {
		t.Fatalf("Refresh after change = %v, %v", changed, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "index-0x8DF2FA80250FEA8.gob")); !os.IsNotExist(err) {
		t.Errorf("old gob not removed: %v", err)
	}
	if m, _ := readMeta(dir); m.ETag != `"v2"` || m.File != "index-v2.gob" {
		t.Errorf("meta after change = %+v", m)
	}
}

func TestCacheUpstreamFailureUsesGob(t *testing.T) {
	srv := newFeedServer(t)
	dir := t.TempDir()
	clk := &clock{t: time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)}
	c := newCache(dir, srv.URL, clk)
	if _, err := c.Load(context.Background()); err != nil {
		t.Fatal(err)
	}

	srv.fail.Store(true)
	clk.t = clk.t.Add(2 * time.Hour)
	ix, err := c.Load(context.Background())
	if err != nil {
		t.Fatalf("500 with gob: %v", err)
	}
	if len(ix.Trips) != 1508 || srv.requests.Load() != 2 {
		t.Fatalf("got %d trips after %d requests", len(ix.Trips), srv.requests.Load())
	}

	// Server gone entirely: still served from the gob.
	srv.Close()
	if ix, err := c.Load(context.Background()); err != nil || len(ix.Trips) != 1508 {
		t.Fatalf("server down with gob: %v", err)
	}

	// Refresh reports the failure but hands back the current index.
	if got, changed, err := c.Refresh(context.Background(), ix); err == nil || changed || got != ix {
		t.Fatalf("Refresh with server down = %p, %v, %v", got, changed, err)
	}
}

func TestCacheNoGobNoNetwork(t *testing.T) {
	srv := newFeedServer(t)
	srv.Close()
	clk := &clock{t: time.Now()}
	c := newCache(t.TempDir(), srv.URL, clk)
	if ix, err := c.Load(context.Background()); err == nil {
		t.Fatalf("Load = %v, want error", ix)
	}
}

func TestCacheIgnoresCorruptGob(t *testing.T) {
	srv := newFeedServer(t)
	dir := t.TempDir()
	clk := &clock{t: time.Now()}
	c := newCache(dir, srv.URL, clk)
	if _, err := c.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	m, _ := readMeta(dir)
	if err := os.WriteFile(filepath.Join(dir, m.File), []byte("garbage"), 0o644); err != nil {
		t.Fatal(err)
	}
	ix, err := c.Load(context.Background())
	if err != nil || len(ix.Trips) != 1508 {
		t.Fatalf("Load with corrupt gob = %v", err)
	}
	if srv.requests.Load() != 2 || srv.lastINM() != "" {
		t.Fatalf("corrupt gob should force an unconditional download")
	}
}

// TestAtomicWriteRace races readers decoding the cache file against a writer
// replacing it. A reader may fail to open (Windows refuses opens of a file
// mid-rename) and retries, but must never decode a partial index.
func TestAtomicWriteRace(t *testing.T) {
	ix := loadFixture(t)
	dir := t.TempDir()
	const name = "index-race.gob"
	if err := atomicWrite(dir, name, ix.Encode); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)

	var stop atomic.Bool
	var decodes, writes, writeFails atomic.Int32
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for !stop.Load() {
				var b []byte
				for {
					var err error
					if b, err = os.ReadFile(path); err == nil {
						break
					}
					time.Sleep(time.Millisecond)
				}
				got, err := Decode(bytes.NewReader(b))
				if err != nil {
					t.Errorf("reader decoded a broken index: %v", err)
					stop.Store(true)
					return
				}
				if len(got.StopTimes) != len(ix.StopTimes) {
					t.Errorf("reader got %d stop times, want %d", len(got.StopTimes), len(ix.StopTimes))
				}
				decodes.Add(1)
			}
		})
	}
	wg.Go(func() {
		defer stop.Store(true)
		for range 40 {
			if err := atomicWrite(dir, name, ix.Encode); err != nil {
				writeFails.Add(1) // tolerated: previous file kept
			}
			writes.Add(1)
		}
	})
	wg.Wait()
	t.Logf("%d writes (%d kept previous), %d decodes", writes.Load(), writeFails.Load(), decodes.Load())
	if decodes.Load() == 0 {
		t.Fatal("readers never decoded")
	}
	if tmps, _ := filepath.Glob(filepath.Join(dir, "*.tmp-*")); len(tmps) != 0 {
		t.Errorf("temp files left behind: %v", tmps)
	}
}

// TestAtomicWriteHeldOpen checks the Windows failure path: while a reader
// holds the target open the rename cannot succeed, so the writer gives up
// after the retry window, cleans its temp file, and leaves the old content.
func TestAtomicWriteHeldOpen(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("rename over an open file succeeds on this OS")
	}
	defer func(d time.Duration) { renameWindow = d }(renameWindow)
	renameWindow = 100 * time.Millisecond

	dir := t.TempDir()
	write := func(s string) func(io.Writer) error {
		return func(w io.Writer) error { _, err := io.WriteString(w, s); return err }
	}
	if err := atomicWrite(dir, "f", write("old")); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(dir, "f"))
	if err != nil {
		t.Fatal(err)
	}
	err = atomicWrite(dir, "f", write("new"))
	f.Close()
	if err == nil {
		t.Fatal("rename over an open file succeeded")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "f")); string(b) != "old" {
		t.Errorf("content = %q, want old", b)
	}
	if tmps, _ := filepath.Glob(filepath.Join(dir, "*.tmp-*")); len(tmps) != 0 {
		t.Errorf("temp files left behind: %v", tmps)
	}
	// Once the reader is gone the write goes through.
	if err := atomicWrite(dir, "f", write("new")); err != nil {
		t.Fatal(err)
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		`"0x8DF2FA80250FEA8"`: "0x8DF2FA80250FEA8",
		`W/"a b/c"`:           "W__a_b_c",
		"":                    "noetag",
		`".."`:                "noetag",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}
