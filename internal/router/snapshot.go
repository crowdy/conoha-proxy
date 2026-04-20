// Package router maps incoming requests to upstream URLs and performs
// the reverse proxy hop. Routing state is immutable after creation —
// reloads happen via atomic pointer swap (see router.go).
package router

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/crowdy/conoha-proxy/internal/service"
)

// SnapshotEntry is one resolved routing record for a host.
type SnapshotEntry struct {
	ServiceName string
	Active      *url.URL
	Draining    *url.URL
}

// Snapshot is an immutable host→entry lookup table.
type Snapshot struct {
	byHost map[string]SnapshotEntry
}

// BuildSnapshot converts a list of Services into an immutable snapshot.
// It returns an error on malformed target URLs so we never silently
// publish a broken routing table.
func BuildSnapshot(svcs []service.Service) (*Snapshot, error) {
	byHost := make(map[string]SnapshotEntry, len(svcs))
	for _, s := range svcs {
		var active, draining *url.URL
		if s.ActiveTarget != nil {
			u, err := parseTargetURL(s.ActiveTarget.URL)
			if err != nil {
				return nil, fmt.Errorf("service %q: %w", s.Name, err)
			}
			active = u
		}
		if s.DrainingTarget != nil {
			u, err := parseTargetURL(s.DrainingTarget.URL)
			if err != nil {
				return nil, fmt.Errorf("service %q draining: %w", s.Name, err)
			}
			draining = u
		}
		entry := SnapshotEntry{ServiceName: s.Name, Active: active, Draining: draining}
		for _, h := range s.Hosts {
			byHost[normalizeHost(h)] = entry
		}
	}
	return &Snapshot{byHost: byHost}, nil
}

// Lookup returns the routing entry for host h (case and port insensitive).
func (s *Snapshot) Lookup(h string) (SnapshotEntry, bool) {
	e, ok := s.byHost[normalizeHost(h)]
	return e, ok
}

func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if i := strings.Index(h, ":"); i >= 0 {
		h = h[:i]
	}
	return h
}

func parseTargetURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid target url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("target scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("target url must include host")
	}
	return u, nil
}
