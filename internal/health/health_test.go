package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/crowdy/conoha-proxy/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProbeOnce_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/up" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewHTTPChecker()
	ctx := context.Background()
	err := c.ProbeOnce(ctx, service.Target{URL: srv.URL},
		service.HealthPolicy{Path: "/up", TimeoutMs: 1000, HealthyThreshold: 1, UnhealthyThreshold: 3})
	require.NoError(t, err)
}

func TestProbeOnce_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewHTTPChecker()
	err := c.ProbeOnce(context.Background(), service.Target{URL: srv.URL},
		service.HealthPolicy{Path: "/up", TimeoutMs: 1000, UnhealthyThreshold: 2, HealthyThreshold: 1})
	require.Error(t, err)
}

func TestProbeOnce_RetriesUntilHealthy(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewHTTPChecker()
	err := c.ProbeOnce(context.Background(), service.Target{URL: srv.URL},
		service.HealthPolicy{Path: "/up", TimeoutMs: 500, HealthyThreshold: 1, UnhealthyThreshold: 5, IntervalMs: 50})
	require.NoError(t, err)
	require.Equal(t, 3, attempts)
}

func TestProbeOnce_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewHTTPChecker()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := c.ProbeOnce(ctx, service.Target{URL: srv.URL},
		service.HealthPolicy{Path: "/up", TimeoutMs: 1000, HealthyThreshold: 1, UnhealthyThreshold: 3, IntervalMs: 10})
	require.Error(t, err)
}
