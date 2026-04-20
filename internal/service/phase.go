package service

import "time"

// Phase is the externally observable lifecycle phase of a Service.
// Internal probing variants collapse to PhaseLive (or PhaseConfigured
// when no active target exists yet) — the transient probe happens
// inside a single API call and is never observed by the routing layer.
type Phase string

const (
	PhaseConfigured Phase = "configured"
	PhaseLive       Phase = "live"
	PhaseSwapping   Phase = "swapping"
)

// Phase returns the current phase given a clock reading.
func (s *Service) Phase(now time.Time) Phase {
	if s.DrainingTarget != nil && s.DrainDeadline != nil && now.Before(*s.DrainDeadline) {
		return PhaseSwapping
	}
	if s.ActiveTarget != nil {
		return PhaseLive
	}
	return PhaseConfigured
}

// DropExpiredDraining clears the draining pointer and deadline if the
// deadline has passed. Called periodically by the router reload loop.
func (s *Service) DropExpiredDraining(now time.Time) {
	if s.DrainDeadline != nil && !now.Before(*s.DrainDeadline) {
		s.DrainingTarget = nil
		s.DrainDeadline = nil
	}
}
