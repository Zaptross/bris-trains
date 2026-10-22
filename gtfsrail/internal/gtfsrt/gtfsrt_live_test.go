//go:build live

package gtfsrt

import (
	"context"
	"testing"
	"time"
)

// TestLiveConditionalGET checks the conditional-GET path against Translink.
// Upstream republishes every ~10 s with a new ETag (even when the body is
// unchanged), so 15 s polling almost never sees a 304; two fetches inside one
// publish window must.
//
//	go test -tags live -run Live ./gtfsrail/internal/gtfsrt/
func TestLiveConditionalGET(t *testing.T) {
	s := NewStore(DefaultFeeds, nil, nil, nil)
	ctx := context.Background()
	if err := s.FetchOnce(ctx, 10*time.Second); err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if err := s.FetchOnce(ctx, 10*time.Second); err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	h := s.Health()
	notModified := h.TripUpdates.NotModified + h.VehiclePositions.NotModified + h.Alerts.NotModified
	t.Logf("304s: tripUpdates=%d vehiclePositions=%d alerts=%d; trips=%d vehicles=%d",
		h.TripUpdates.NotModified, h.VehiclePositions.NotModified, h.Alerts.NotModified,
		len(s.Snapshot().Trips), len(s.Snapshot().Vehicles))
	if notModified == 0 {
		t.Error("no 304 across two back-to-back fetches: If-None-Match is not being sent or honoured")
	}
	if len(s.Snapshot().Trips) == 0 {
		t.Error("a 304 must keep the previous snapshot, but it is empty")
	}
}
