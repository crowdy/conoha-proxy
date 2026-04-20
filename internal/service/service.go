// Package service defines the domain types for conoha-proxy services,
// targets, and health policies, plus validation and small query helpers.
package service

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Service is a named routing unit that matches one or more hosts and proxies
// to a blue/green pair of upstream Targets.
type Service struct {
	Name           string       `json:"name"`
	Hosts          []string     `json:"hosts"`
	ActiveTarget   *Target      `json:"active_target,omitempty"`
	DrainingTarget *Target      `json:"draining_target,omitempty"`
	DrainDeadline  *time.Time   `json:"drain_deadline,omitempty"`
	HealthPolicy   HealthPolicy `json:"health_policy"`
	CreatedAt      time.Time    `json:"created_at"`
	UpdatedAt      time.Time    `json:"updated_at"`
}

// Target is a single upstream URL.
type Target struct {
	URL        string    `json:"url"`
	DeployedAt time.Time `json:"deployed_at"`
}

// HealthPolicy controls how we probe a Target.
type HealthPolicy struct {
	Path               string `json:"path"`
	IntervalMs         int    `json:"interval_ms"`
	TimeoutMs          int    `json:"timeout_ms"`
	HealthyThreshold   int    `json:"healthy_threshold"`
	UnhealthyThreshold int    `json:"unhealthy_threshold"`
}

// WithDefaults returns a copy of p with zero fields replaced by defaults.
func (p HealthPolicy) WithDefaults() HealthPolicy {
	if p.Path == "" {
		p.Path = "/up"
	}
	if p.IntervalMs == 0 {
		p.IntervalMs = 5000
	}
	if p.TimeoutMs == 0 {
		p.TimeoutMs = 2000
	}
	if p.HealthyThreshold == 0 {
		p.HealthyThreshold = 1
	}
	if p.UnhealthyThreshold == 0 {
		p.UnhealthyThreshold = 3
	}
	return p
}

// Validate returns nil when s is a usable Service.
func (s *Service) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("service name must be non-empty")
	}
	if len(s.Hosts) == 0 {
		return errors.New("service must have at least one hosts entry")
	}
	seen := make(map[string]struct{}, len(s.Hosts))
	for _, h := range s.Hosts {
		h = strings.TrimSpace(strings.ToLower(h))
		if h == "" {
			return errors.New("host entries must be non-empty")
		}
		if _, ok := seen[h]; ok {
			return fmt.Errorf("duplicate host: %q", h)
		}
		seen[h] = struct{}{}
	}
	return nil
}

// Validate checks that the target URL is an http:// or https:// URL.
func (t *Target) Validate() error {
	u, err := url.Parse(t.URL)
	if err != nil {
		return fmt.Errorf("invalid target url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("target url scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("target url must include host")
	}
	return nil
}

// MatchesHost reports whether h (possibly with :port) matches any of s.Hosts
// after case folding and port stripping.
func (s *Service) MatchesHost(h string) bool {
	h = strings.ToLower(h)
	if i := strings.Index(h, ":"); i >= 0 {
		h = h[:i]
	}
	for _, x := range s.Hosts {
		if strings.EqualFold(strings.TrimSpace(x), h) {
			return true
		}
	}
	return false
}

// Touch stamps UpdatedAt to now.
func (s *Service) Touch() {
	s.UpdatedAt = time.Now().UTC()
}
