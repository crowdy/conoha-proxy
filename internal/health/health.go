// Package health implements target health probing.
package health

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/crowdy/conoha-proxy/internal/service"
)

// Checker probes targets against a HealthPolicy.
type Checker interface {
	ProbeOnce(ctx context.Context, t service.Target, p service.HealthPolicy) error
}

// HTTPChecker uses a plain http.Client.
type HTTPChecker struct {
	client *http.Client
}

// NewHTTPChecker returns a Checker with a default transport.
func NewHTTPChecker() *HTTPChecker {
	return &HTTPChecker{client: &http.Client{}}
}

// ProbeOnce polls the target until HealthyThreshold consecutive 2xx responses
// are seen or UnhealthyThreshold consecutive failures, whichever comes first.
// The per-request timeout is TimeoutMs; between attempts IntervalMs is slept.
func (h *HTTPChecker) ProbeOnce(ctx context.Context, t service.Target, p service.HealthPolicy) error {
	p = p.WithDefaults()
	if err := t.Validate(); err != nil {
		return err
	}
	url := strings.TrimRight(t.URL, "/") + p.Path

	var consecutiveOK, consecutiveFail int
	for {
		reqCtx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutMs)*time.Millisecond)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
		if err != nil {
			cancel()
			return fmt.Errorf("build probe request: %w", err)
		}
		resp, err := h.client.Do(req)
		cancel()

		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				consecutiveOK++
				consecutiveFail = 0
				if consecutiveOK >= p.HealthyThreshold {
					return nil
				}
			} else {
				consecutiveOK = 0
				consecutiveFail++
			}
		} else {
			consecutiveOK = 0
			consecutiveFail++
		}

		if consecutiveFail >= p.UnhealthyThreshold {
			return fmt.Errorf("probe failed: %d consecutive failures", consecutiveFail)
		}

		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), errors.New("probe canceled"))
		case <-time.After(time.Duration(p.IntervalMs) * time.Millisecond):
		}
	}
}
