package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"trains/gtfsrail"
	"trains/gtfsrail/wire"
)

func feedServer(t *testing.T) *httptest.Server {
	t.Helper()
	files := map[string]string{
		"/static.zip": "../../../testdata/static.zip",
		"/tu":         "../../../testdata/rt/20261023T000334/tu.pb",
		"/vp":         "../../../testdata/rt/20261023T000334/vp.pb",
		"/alerts":     "../../../testdata/rt/20261023T000334/alerts.pb",
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		data, err := os.ReadFile(p)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if r.URL.Path == "/static.zip" {
			w.Header().Set("ETag", `"fixture"`)
		}
		w.Write(data)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testNow() time.Time { return time.Date(2026, 10, 23, 0, 4, 0, 0, gtfsrail.Brisbane) }

func openService(t *testing.T, live bool) *gtfsrail.Service {
	t.Helper()
	fs := feedServer(t)
	svc, err := gtfsrail.Open(context.Background(), gtfsrail.Config{
		CacheDir:  t.TempDir(),
		StaticURL: fs.URL + "/static.zip",
		Feeds:     gtfsrail.FeedURLs{TripUpdates: fs.URL + "/tu", VehiclePositions: fs.URL + "/vp", Alerts: fs.URL + "/alerts"},
		Live:      live,
		Logger:    slog.New(slog.DiscardHandler),
		Now:       testNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.Close)
	return svc
}

func apiServer(t *testing.T, dev bool) *httptest.Server {
	t.Helper()
	svc := openService(t, false)
	h := newHandler(svc, fstest.MapFS{}, false, dev, slog.New(slog.DiscardHandler), nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

type resp struct {
	status int
	header http.Header
	body   []byte
}

func do(t *testing.T, method, url string, hdr map[string]string) resp {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return resp{r.StatusCode, r.Header, b}
}

func get(t *testing.T, url string) resp { return do(t, "GET", url, nil) }

func decode[T any](t *testing.T, r resp) T {
	t.Helper()
	if ct := r.header.Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content type %q, body %s", ct, r.body)
	}
	var v T
	if err := json.Unmarshal(r.body, &v); err != nil {
		t.Fatalf("decode: %v: %s", err, r.body)
	}
	return v
}

func TestAPI(t *testing.T) {
	srv := apiServer(t, false)
	u := srv.URL

	t.Run("stations", func(t *testing.T) {
		r := get(t, u+"/api/stations")
		if r.status != 200 || r.header.Get("Cache-Control") != "public, max-age=3600" {
			t.Fatalf("status %d cache %q", r.status, r.header.Get("Cache-Control"))
		}
		raw := decode[struct {
			Stations []map[string]json.RawMessage `json:"stations"`
		}](t, r)
		if len(raw.Stations) < 50 {
			t.Fatalf("only %d stations", len(raw.Stations))
		}
		for _, s := range raw.Stations {
			if a := string(s["aliases"]); !strings.HasPrefix(a, "[") {
				t.Fatalf("aliases not an array: %s", a)
			}
		}
	})

	var jr wire.JourneysResponse
	t.Run("journeys", func(t *testing.T) {
		r := get(t, u+"/api/journeys?origin=central&destination=roma")
		if r.status != 200 || r.header.Get("Cache-Control") != "no-store" {
			t.Fatalf("status %d: %s", r.status, r.body)
		}
		jr = decode[wire.JourneysResponse](t, r)
		if jr.Origin.Name != "Central station" || len(jr.Journeys) == 0 {
			t.Fatalf("origin %q, %d journeys", jr.Origin.Name, len(jr.Journeys))
		}
	})
	t.Run("journeys errors", func(t *testing.T) {
		r := get(t, u+"/api/journeys?origin=central&destination=south")
		e := decode[wire.ErrorResponse](t, r)
		if r.status != 400 || e.Error != wire.ErrAmbiguous || len(e.Candidates) != 2 {
			t.Fatalf("ambiguous: %d %s", r.status, r.body)
		}
		r = get(t, u+"/api/journeys?origin=central&destination=zzzzzz")
		e = decode[wire.ErrorResponse](t, r)
		if r.status != 404 || e.Error != wire.ErrUnknownStation {
			t.Fatalf("unknown: %d %s", r.status, r.body)
		}
		for _, q := range []string{"origin=central&destination=roma&limit=0", "origin=central", "origin=central&destination=roma&at=bogus", "origin=central&destination=central"} {
			r = get(t, u+"/api/journeys?"+q)
			if e := decode[wire.ErrorResponse](t, r); r.status != 400 || e.Error != wire.ErrBadRequest {
				t.Fatalf("%s: %d %s", q, r.status, r.body)
			}
		}
		r = get(t, u+"/api/journeys?origin=central&destination=roma&at=08:00")
		if r.status != 200 {
			t.Fatalf("at=08:00: %d %s", r.status, r.body)
		}
	})

	var lr wire.LegResponse
	t.Run("legs", func(t *testing.T) {
		if len(jr.Journeys) == 0 {
			t.Skip("no journeys")
		}
		l := jr.Journeys[0].Legs[0]
		r := get(t, u+"/api/legs/"+l.TripID+"?date="+l.ServiceDate)
		if r.status != 200 {
			t.Fatalf("leg: %d %s", r.status, r.body)
		}
		lr = decode[wire.LegResponse](t, r)
		if lr.Leg.TripID != l.TripID {
			t.Fatalf("trip %q", lr.Leg.TripID)
		}
		r = get(t, u+"/api/legs/no-such-trip")
		if e := decode[wire.ErrorResponse](t, r); r.status != 404 || e.Error != wire.ErrNotFound {
			t.Fatalf("unknown trip: %d %s", r.status, r.body)
		}
		r = get(t, u+"/api/legs/"+l.TripID+"?date=nope")
		if r.status != 400 {
			t.Fatalf("bad date: %d", r.status)
		}
	})
	t.Run("shapes", func(t *testing.T) {
		if lr.Leg.ShapeID == nil {
			t.Skip("no shape")
		}
		r := get(t, u+"/api/shapes/"+*lr.Leg.ShapeID)
		if r.status != 200 || r.header.Get("Cache-Control") != "public, max-age=86400, immutable" {
			t.Fatalf("shape: %d %q", r.status, r.header.Get("Cache-Control"))
		}
		if s := decode[wire.ShapeResponse](t, r); len(s.Points) < 2 {
			t.Fatalf("%d points", len(s.Points))
		}
		if r := get(t, u+"/api/shapes/nope"); r.status != 404 {
			t.Fatalf("unknown shape: %d", r.status)
		}
	})
	t.Run("healthz", func(t *testing.T) {
		r := get(t, u+"/api/healthz")
		h := decode[wire.HealthResponse](t, r)
		if r.status != 200 || h.TripUpdates.Fetches < 1 || h.TripUpdates.LastSuccess == nil {
			t.Fatalf("healthz: %d %s", r.status, r.body)
		}
	})
	t.Run("api not found", func(t *testing.T) {
		r := get(t, u+"/api/nope")
		if e := decode[wire.ErrorResponse](t, r); r.status != 404 || e.Error != "not_found" {
			t.Fatalf("%d %s", r.status, r.body)
		}
		r = do(t, "POST", u+"/api/stations", nil)
		if decode[wire.ErrorResponse](t, r); r.status != 405 {
			t.Fatalf("POST: %d %s", r.status, r.body)
		}
		if r := get(t, u+"/"); r.status != 503 {
			t.Fatalf("/ without UI: %d", r.status)
		}
	})
}

func TestUI(t *testing.T) {
	ui := fstest.MapFS{
		"index.html":    {Data: []byte("<html>app</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	}
	srv := httptest.NewServer(newHandler(nil, ui, true, false, slog.New(slog.DiscardHandler), nil))
	defer srv.Close()
	for _, p := range []string{"/", "/trip/abc", "/gallery"} {
		r := get(t, srv.URL+p)
		if r.status != 200 || string(r.body) != "<html>app</html>" || r.header.Get("Cache-Control") != "no-cache" {
			t.Fatalf("%s: %d %q", p, r.status, r.body)
		}
	}
	r := get(t, srv.URL+"/assets/app.js")
	if r.status != 200 || r.header.Get("Cache-Control") != cacheImmutable {
		t.Fatalf("asset: %d %q", r.status, r.header.Get("Cache-Control"))
	}
	for _, p := range []string{"/nonsense", "/assets/missing.js"} {
		if r := get(t, srv.URL+p); r.status != 404 || bytes.Contains(r.body, []byte("app")) {
			t.Fatalf("%s: %d", p, r.status)
		}
	}
	if r := get(t, srv.URL+"/api/whatever"); r.status != 404 || !strings.HasPrefix(r.header.Get("Content-Type"), "application/json") {
		t.Fatalf("api fallback: %d %s", r.status, r.body)
	}

	nb := httptest.NewServer(newHandler(nil, ui, false, false, slog.New(slog.DiscardHandler), nil))
	defer nb.Close()
	for _, p := range []string{"/", "/trip/abc"} {
		r := get(t, nb.URL+p)
		if r.status != 503 || !bytes.Contains(r.body, []byte("not built")) || !bytes.Contains(r.body, []byte("npm --prefix web ci")) {
			t.Fatalf("%s: %d %s", p, r.status, r.body)
		}
	}
}

func TestCORS(t *testing.T) {
	ui := fstest.MapFS{"index.html": {Data: []byte("x")}}
	dev := httptest.NewServer(newHandler(nil, ui, true, true, slog.New(slog.DiscardHandler), nil))
	defer dev.Close()
	prod := httptest.NewServer(newHandler(nil, ui, true, false, slog.New(slog.DiscardHandler), nil))
	defer prod.Close()
	origin := map[string]string{"Origin": "http://localhost:5173"}

	r := do(t, "GET", dev.URL+"/api/nope", origin)
	if r.header.Get("Access-Control-Allow-Origin") != "http://localhost:5173" {
		t.Fatalf("dev: %v", r.header)
	}
	r = do(t, "GET", dev.URL+"/api/nope", map[string]string{"Origin": "http://evil.example:5173"})
	if r.header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("foreign origin echoed")
	}
	r = do(t, "OPTIONS", dev.URL+"/api/journeys", map[string]string{"Origin": "http://127.0.0.1:5173", "Access-Control-Request-Method": "GET"})
	if r.status != 204 || r.header.Get("Access-Control-Allow-Origin") != "http://127.0.0.1:5173" {
		t.Fatalf("preflight: %d %v", r.status, r.header)
	}
	r = do(t, "GET", prod.URL+"/api/nope", origin)
	if r.header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("CORS header without --dev")
	}
}

func TestLoadDoesNotTriggerFetches(t *testing.T) {
	svc := openService(t, true)
	deadline := time.Now().Add(10 * time.Second)
	for svc.Health().TripUpdates.Fetches < 1 {
		if time.Now().After(deadline) {
			t.Fatal("no initial fetch")
		}
		time.Sleep(10 * time.Millisecond)
	}
	before := svc.Health()
	srv := httptest.NewServer(newHandler(svc, fstest.MapFS{}, false, false, slog.New(slog.DiscardHandler), nil))
	defer srv.Close()

	var wg sync.WaitGroup
	codes := make([]int, 50)
	for i := range 50 {
		wg.Go(func() {
			r, err := http.Get(srv.URL + "/api/journeys?origin=central&destination=roma")
			if err != nil {
				return
			}
			io.Copy(io.Discard, r.Body)
			r.Body.Close()
			codes[i] = r.StatusCode
		})
	}
	wg.Wait()
	for i, c := range codes {
		if c != 200 {
			t.Fatalf("request %d: status %d", i, c)
		}
	}
	after := svc.Health()
	if after.TripUpdates.Fetches != before.TripUpdates.Fetches ||
		after.VehiclePositions.Fetches != before.VehiclePositions.Fetches ||
		after.Alerts.Fetches != before.Alerts.Fetches {
		t.Fatalf("fetch counts changed: before %+v after %+v", before, after)
	}
}

func TestRun(t *testing.T) {
	ctx := context.Background()
	var out, errb bytes.Buffer
	if code := Run(ctx, []string{"--json"}, &out, &errb, []string{"json", "at"}); code != 2 ||
		!strings.Contains(errb.String(), "--json is a query flag: trains <origin> <destination> --json") {
		t.Fatalf("query flag: %d %q", code, errb.String())
	}

	out.Reset()
	errb.Reset()
	if code := Run(ctx, []string{"--help"}, &out, &errb, nil); code != 0 {
		t.Fatalf("help: %d", code)
	}
	for _, f := range FlagNames() {
		if !strings.Contains(out.String(), "-"+f) {
			t.Fatalf("usage lacks %s: %s", f, out.String())
		}
	}
	if strings.Contains(out.String(), "json") {
		t.Fatalf("usage lists query flags: %s", out.String())
	}

	if code := Run(ctx, []string{"--bogus"}, io.Discard, io.Discard, nil); code != 2 {
		t.Fatalf("unknown flag: %d", code)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	errb.Reset()
	if code := Run(ctx, []string{"--addr", ln.Addr().String(), "--cache", t.TempDir()}, io.Discard, &errb, nil); code != 1 {
		t.Fatalf("taken port: %d %s", code, errb.String())
	}
}
