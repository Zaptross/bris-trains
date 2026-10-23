// Package serve implements `trains serve`: the HTTP API and embedded web UI.
package serve

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"trains/gtfsrail"
	"trains/gtfsrail/wire"
	"trains/internal/version"
	"trains/web"
)

// FlagNames lists serve-mode flag names without dashes.
func FlagNames() []string { return []string{"addr", "dev", "cache", "static-url", "feeds-url"} }

// clockFromEnv returns the server's clock. TRAINS_NOW (RFC3339) starts the
// clock at that instant and lets it advance in real time; CI uses it to serve
// the committed fixture, whose timetable is dated. Deliberately undocumented.
func clockFromEnv() (func() time.Time, bool, error) {
	v := os.Getenv("TRAINS_NOW")
	if v == "" {
		return time.Now, false, nil
	}
	start, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return nil, false, fmt.Errorf("TRAINS_NOW: %w", err)
	}
	began := time.Now()
	return func() time.Time { return start.Add(time.Since(began)) }, true, nil
}

func defaultCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "trains")
}

// Run parses serve flags from args and serves until ctx is done.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, queryFlags []string) int {
	fset := flag.NewFlagSet("trains serve", flag.ContinueOnError)
	fset.SetOutput(io.Discard)
	addr := fset.String("addr", "localhost:8080", "listen address")
	dev := fset.Bool("dev", false, "allow CORS from http://localhost:* and http://127.0.0.1:* (for the Vite dev server)")
	cache := fset.String("cache", defaultCacheDir(), "timetable cache directory")
	staticURL := fset.String("static-url", gtfsrail.DefaultStaticURL, "static GTFS zip `URL`")
	feedsURL := fset.String("feeds-url", gtfsrail.DefaultFeedsURL, "GTFS-RT base `URL`; feeds are read from <URL>/TripUpdates/Rail, /VehiclePositions/Rail and /Alerts")
	usage := func(w io.Writer) {
		fmt.Fprintln(w, "Usage: trains serve [--addr host:port] [--dev] [--cache dir] [--static-url URL] [--feeds-url URL]")
		fmt.Fprintln(w)
		fset.SetOutput(w)
		fset.PrintDefaults()
		fset.SetOutput(io.Discard)
	}
	for _, a := range args {
		if a == "--" || !strings.HasPrefix(a, "-") {
			continue
		}
		name, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "=")
		if slices.Contains(queryFlags, name) && !slices.Contains(FlagNames(), name) {
			fmt.Fprintf(stderr, "trains: --%s is a query flag: trains <origin> <destination> --%s\n", name, name)
			return 2
		}
	}
	if err := fset.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			usage(stdout)
			return 0
		}
		fmt.Fprintf(stderr, "trains serve: %v\n", err)
		usage(stderr)
		return 2
	}
	if fset.NArg() > 0 {
		fmt.Fprintf(stderr, "trains serve: unexpected argument %q\n", fset.Arg(0))
		usage(stderr)
		return 2
	}

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	now, pinned, err := clockFromEnv()
	if err != nil {
		logger.Error("bad clock override", "err", err)
		return 2
	}
	if pinned {
		logger.Warn("clock pinned by TRAINS_NOW", "now", now().Format(time.RFC3339))
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		logger.Error("cannot listen", "addr", *addr, "err", err)
		return 1
	}
	defer ln.Close()

	svc, err := gtfsrail.Open(ctx, gtfsrail.Config{
		CacheDir: *cache, StaticURL: *staticURL, Feeds: gtfsrail.FeedsAt(*feedsURL),
		Live: true, ReservedWords: []string{"serve"}, Logger: logger, Now: now,
	})
	if err != nil {
		logger.Error("cannot open timetable", "err", err)
		return 1
	}
	defer svc.Close()
	for _, w := range svc.Warnings() {
		logger.Warn("station data", "warning", w)
	}

	srv := &http.Server{
		Handler:           newHandler(svc, web.FS(), web.Built(), *dev, logger, now),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	logger.Info("listening on http://" + ln.Addr().String())

	select {
	case <-ctx.Done():
	case err := <-errc:
		logger.Error("server failed", "err", err)
		return 1
	}
	sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		logger.Warn("shutdown", "err", err)
	}
	return 0
}

// backend is the subset of *gtfsrail.Service the handler uses.
type backend interface {
	Stations() []gtfsrail.Station
	Station(id gtfsrail.StationID) (gtfsrail.Station, bool)
	Resolve(query string) (gtfsrail.Station, error)
	Journeys(ctx context.Context, origin, dest gtfsrail.StationID, at time.Time, opt gtfsrail.JourneyOptions) ([]gtfsrail.Journey, gtfsrail.Freshness, error)
	Leg(ctx context.Context, trip gtfsrail.TripID, date gtfsrail.Date, opt gtfsrail.LegOptions) (*gtfsrail.Leg, gtfsrail.Freshness, error)
	Shape(id gtfsrail.ShapeID) ([]gtfsrail.Point, bool)
	Health() gtfsrail.Health
}

type handler struct {
	b   backend
	log *slog.Logger
	now func() time.Time
}

const (
	cacheNoStore   = "no-store"
	cacheImmutable = "public, max-age=31536000, immutable"
)

// newHandler builds the HTTP handler; now defaults to time.Now.
func newHandler(b backend, ui fs.FS, uiBuilt bool, dev bool, logger *slog.Logger, now func() time.Time) http.Handler {
	if now == nil {
		now = time.Now
	}
	h := &handler{b: b, log: logger, now: now}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/stations", h.stations)
	mux.HandleFunc("GET /api/journeys", h.journeys)
	mux.HandleFunc("GET /api/legs/{tripId}", h.leg)
	mux.HandleFunc("GET /api/shapes/{shapeId}", h.shape)
	mux.HandleFunc("GET /api/healthz", h.health)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, wire.ErrNotFound, "no such API endpoint")
	})
	for _, p := range []string{"/api/stations", "/api/journeys", "/api/legs/{tripId}", "/api/shapes/{shapeId}", "/api/healthz"} {
		mux.HandleFunc(p, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed, wire.ErrMethod, r.Method+" not allowed")
		})
	}

	if uiBuilt {
		files := http.FileServerFS(ui)
		index := func(w http.ResponseWriter, r *http.Request) {
			data, err := fs.ReadFile(ui, "index.html")
			if err != nil {
				h.log.Error("read index.html", "err", err)
				http.Error(w, "index.html missing", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache")
			w.Write(data)
		}
		mux.HandleFunc("GET /{$}", index)
		mux.HandleFunc("GET /trip/", index)
		mux.HandleFunc("GET /gallery", index)
		mux.HandleFunc("GET /assets/", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", cacheImmutable)
			files.ServeHTTP(w, r)
		})
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			p := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
			if p == "index.html" {
				index(w, r)
				return
			}
			if st, err := fs.Stat(ui, p); err != nil || st.IsDir() {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Cache-Control", "no-cache")
			files.ServeHTTP(w, r)
		})
	} else {
		notBuilt := func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", cacheNoStore)
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, notBuiltPage)
		}
		mux.HandleFunc("GET /{$}", notBuilt)
		mux.HandleFunc("GET /trip/", notBuilt)
		mux.HandleFunc("GET /gallery", notBuilt)
	}

	var root http.Handler = mux
	if dev {
		root = cors(root)
	}
	return accessLog(logger, root)
}

const notBuiltPage = `<!doctype html>
<html><head><meta charset="utf-8"><title>trains: UI not built</title></head>
<body>
<h1>The web UI is not built</h1>
<p>This binary was compiled without the web UI. The JSON API under <code>/api/</code> still works.</p>
<p>To build the UI, run:</p>
<pre>npm --prefix web ci &amp;&amp; npm --prefix web run build</pre>
<p>then rebuild the binary (<code>go build ./cmd/trains</code>).</p>
</body></html>
`

func accessLog(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		logger.Debug("request", "method", r.Method, "path", r.URL.Path, "dur", time.Since(start))
	})
}

func allowedOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.Path != "" {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1"
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && allowedOrigin(origin) {
			hd := w.Header()
			hd.Set("Access-Control-Allow-Origin", origin)
			hd.Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				hd.Set("Access-Control-Allow-Methods", "GET, OPTIONS")
				if req := r.Header.Get("Access-Control-Request-Headers"); req != "" {
					hd.Set("Access-Control-Allow-Headers", req)
				}
				hd.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, cacheControl string, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", cacheControl)
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string, candidates ...wire.Station) {
	writeJSON(w, status, cacheNoStore, wire.ErrorResponse{Error: code, Message: msg, Candidates: candidates})
}

func (h *handler) internal(w http.ResponseWriter, r *http.Request, err error) {
	h.log.Error("request failed", "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, wire.ErrInternal, "internal server error")
}

func (h *handler) stations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, "public, max-age=3600", wire.StationsResponse{Stations: wire.FromStations(h.b.Stations())})
}

// station resolves a query parameter; on failure it writes the error response and returns false.
func (h *handler) station(w http.ResponseWriter, param, value string) (gtfsrail.Station, bool) {
	if value == "" {
		writeError(w, http.StatusBadRequest, wire.ErrBadRequest, "missing "+param)
		return gtfsrail.Station{}, false
	}
	if st, ok := h.b.Station(gtfsrail.StationID(value)); ok {
		return st, true
	}
	st, err := h.b.Resolve(value)
	if err == nil {
		return st, true
	}
	var amb *gtfsrail.AmbiguousError
	var unk *gtfsrail.UnknownError
	switch {
	case errors.As(err, &amb):
		writeError(w, http.StatusBadRequest, wire.ErrAmbiguous, err.Error(), wire.FromStations(amb.Candidates)...)
	case errors.As(err, &unk):
		writeError(w, http.StatusNotFound, wire.ErrUnknownStation, err.Error(), wire.FromStations(unk.Suggestions)...)
	default:
		writeError(w, http.StatusBadRequest, wire.ErrBadRequest, err.Error())
	}
	return gtfsrail.Station{}, false
}

func (h *handler) parseAt(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	clock, err := time.Parse("15:04", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid at %q: want RFC3339 or HH:MM", s)
	}
	now := h.now().In(gtfsrail.Brisbane)
	return time.Date(now.Year(), now.Month(), now.Day(), clock.Hour(), clock.Minute(), 0, 0, gtfsrail.Brisbane), nil
}

func (h *handler) journeys(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opt := gtfsrail.JourneyOptions{Limit: 5}
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > 20 {
			writeError(w, http.StatusBadRequest, wire.ErrBadRequest, "limit must be an integer from 1 to 20")
			return
		}
		opt.Limit = n
	}
	switch d := q.Get("direct"); d {
	case "", "0", "false":
	case "1", "true":
		opt.DirectOnly = true
	default:
		writeError(w, http.StatusBadRequest, wire.ErrBadRequest, "direct must be true or false")
		return
	}
	at, err := h.parseAt(q.Get("at"))
	if err != nil {
		writeError(w, http.StatusBadRequest, wire.ErrBadRequest, err.Error())
		return
	}
	origin, ok := h.station(w, "origin", q.Get("origin"))
	if !ok {
		return
	}
	dest, ok := h.station(w, "destination", q.Get("destination"))
	if !ok {
		return
	}
	js, fresh, err := h.b.Journeys(r.Context(), origin.ID, dest.ID, at, opt)
	if errors.Is(err, gtfsrail.ErrSameStation) {
		writeError(w, http.StatusBadRequest, wire.ErrBadRequest, err.Error())
		return
	}
	if err != nil {
		h.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cacheNoStore, wire.FromJourneys(origin, dest, js, fresh))
}

func (h *handler) leg(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var date gtfsrail.Date
	if s := q.Get("date"); s != "" {
		d, err := gtfsrail.ParseDate(s)
		if err != nil {
			writeError(w, http.StatusBadRequest, wire.ErrBadRequest, "invalid date: want YYYY-MM-DD")
			return
		}
		date = d
	}
	opt := gtfsrail.LegOptions{From: gtfsrail.StationID(q.Get("from")), To: gtfsrail.StationID(q.Get("to"))}
	leg, fresh, err := h.b.Leg(r.Context(), gtfsrail.TripID(r.PathValue("tripId")), date, opt)
	if errors.Is(err, gtfsrail.ErrNotFound) {
		writeError(w, http.StatusNotFound, wire.ErrNotFound, err.Error())
		return
	}
	if err != nil {
		h.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, cacheNoStore, wire.FromLegResponse(leg, fresh))
}

func (h *handler) shape(w http.ResponseWriter, r *http.Request) {
	id := gtfsrail.ShapeID(r.PathValue("shapeId"))
	pts, ok := h.b.Shape(id)
	if !ok {
		writeError(w, http.StatusNotFound, wire.ErrNotFound, "unknown shape")
		return
	}
	writeJSON(w, http.StatusOK, "public, max-age=86400, immutable", wire.FromShape(id, pts))
}

func (h *handler) health(w http.ResponseWriter, r *http.Request) {
	hl := h.b.Health()
	now := h.now()
	conv := func(f gtfsrail.FeedHealth) wire.FeedHealth {
		out := wire.FeedHealth{Fetches: f.Fetches, NotModified: f.NotModified}
		if !f.LastSuccess.IsZero() {
			s := wire.Time(f.LastSuccess)
			age := int64(now.Sub(f.LastSuccess) / time.Second)
			out.LastSuccess, out.AgeSeconds = &s, &age
		}
		if f.LastError != "" {
			e := f.LastError
			out.LastError = &e
		}
		return out
	}
	writeJSON(w, http.StatusOK, cacheNoStore, wire.HealthResponse{
		TripUpdates: conv(hl.TripUpdates), VehiclePositions: conv(hl.VehiclePositions),
		Alerts: conv(hl.Alerts), Degraded: hl.Degraded, Version: version.String(),
	})
}
