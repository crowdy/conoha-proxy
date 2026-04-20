package health

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crowdy/conoha-proxy/internal/service"
	"github.com/stretchr/testify/require"
)

func TestWatch_EmitsEventsOnTransition(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if healthy.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewHTTPChecker()
	events, stop := c.Watch(service.Target{URL: srv.URL},
		service.HealthPolicy{Path: "/up", IntervalMs: 30, TimeoutMs: 200, HealthyThreshold: 2, UnhealthyThreshold: 2})
	t.Cleanup(stop)

	// First: expect a Healthy event after 2 consecutive 200s.
	select {
	case ev := <-events:
		require.True(t, ev.Healthy)
	case <-time.After(2 * time.Second):
		t.Fatal("expected healthy event")
	}

	// Flip to unhealthy.
	healthy.Store(false)

	// Expect an Unhealthy event.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-events:
			if !ev.Healthy {
				return // success
			}
		case <-deadline:
			t.Fatal("expected unhealthy event")
		}
	}
}
