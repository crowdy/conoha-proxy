package health

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/crowdy/conoha-proxy/internal/service"
)

// HealthEvent is emitted when a target transitions between Healthy states.
type HealthEvent struct {
	Healthy bool
	At      time.Time
	Err     error
}

// Watch probes t continuously according to p and emits an event on each
// transition. The returned stop function cancels the watcher and closes
// the channel.
func (h *HTTPChecker) Watch(t service.Target, p service.HealthPolicy) (<-chan HealthEvent, func()) {
	p = p.WithDefaults()
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan HealthEvent, 8)

	go h.watchLoop(ctx, ch, t, p)

	return ch, func() {
		cancel()
	}
}

func (h *HTTPChecker) watchLoop(ctx context.Context, ch chan<- HealthEvent, t service.Target, p service.HealthPolicy) {
	defer close(ch)

	url := strings.TrimRight(t.URL, "/") + p.Path
	var ok, fail int
	// Start in an indeterminate state — emit the first transition only.
	state := -1 // -1 unknown, 0 unhealthy, 1 healthy

	ticker := time.NewTicker(time.Duration(p.IntervalMs) * time.Millisecond)
	defer ticker.Stop()

	probe := func() {
		reqCtx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutMs)*time.Millisecond)
		defer cancel()
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
		if err != nil {
			ok, fail = 0, fail+1
			return
		}
		resp, err := h.client.Do(req)
		if err != nil {
			ok, fail = 0, fail+1
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			ok, fail = ok+1, 0
		} else {
			ok, fail = 0, fail+1
		}
	}

	emit := func(healthy bool, err error) {
		if (state == 1) == healthy {
			return // no transition
		}
		state = 0
		if healthy {
			state = 1
		}
		select {
		case ch <- HealthEvent{Healthy: healthy, At: time.Now(), Err: err}:
		default:
			// drop on full channel
		}
	}

	for {
		probe()
		if ok >= p.HealthyThreshold {
			emit(true, nil)
		} else if fail >= p.UnhealthyThreshold {
			emit(false, nil)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
