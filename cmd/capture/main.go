// Command capture fetches the three Translink rail realtime feeds at one
// instant and writes the raw protobuf payloads, for use as test fixtures.
//
//	go run ./cmd/capture -out testdata/rt
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var feeds = map[string]string{
	"tu.pb":     "https://gtfsrt.api.translink.com.au/api/realtime/SEQ/TripUpdates/Rail",
	"vp.pb":     "https://gtfsrt.api.translink.com.au/api/realtime/SEQ/VehiclePositions/Rail",
	"alerts.pb": "https://gtfsrt.api.translink.com.au/api/realtime/SEQ/Alerts",
}

func main() {
	out := flag.String("out", "testdata/rt", "parent directory for the capture")
	flag.Parse()
	if err := run(*out); err != nil {
		fmt.Fprintln(os.Stderr, "capture:", err)
		os.Exit(1)
	}
}

func run(parent string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	at := time.Now()
	dir := filepath.Join(parent, at.Format("20060102T150405"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	var wg sync.WaitGroup
	errs := make(chan error, len(feeds))
	for name, url := range feeds {
		wg.Go(func() {
			if err := fetch(ctx, url, filepath.Join(dir, name)); err != nil {
				errs <- fmt.Errorf("%s: %w", name, err)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		return err
	}
	fmt.Println(dir)
	return nil
}

func fetch(ctx context.Context, url, path string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}
