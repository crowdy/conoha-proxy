package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPhase_Configured_WhenNoActive(t *testing.T) {
	s := &Service{}
	require.Equal(t, PhaseConfigured, s.Phase(time.Now()))
}

func TestPhase_Live_WhenActiveOnly(t *testing.T) {
	s := &Service{ActiveTarget: &Target{URL: "http://x"}}
	require.Equal(t, PhaseLive, s.Phase(time.Now()))
}

func TestPhase_Swapping_WhenDraining(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second)
	s := &Service{
		ActiveTarget:   &Target{URL: "http://new"},
		DrainingTarget: &Target{URL: "http://old"},
		DrainDeadline:  &deadline,
	}
	require.Equal(t, PhaseSwapping, s.Phase(time.Now()))
}

func TestPhase_Live_AfterDrainDeadline(t *testing.T) {
	deadline := time.Now().Add(-1 * time.Second)
	s := &Service{
		ActiveTarget:   &Target{URL: "http://new"},
		DrainingTarget: &Target{URL: "http://old"},
		DrainDeadline:  &deadline,
	}
	require.Equal(t, PhaseLive, s.Phase(time.Now()))
}

func TestDropExpiredDraining_DropsPastDeadline(t *testing.T) {
	past := time.Now().Add(-1 * time.Second)
	s := &Service{
		ActiveTarget:   &Target{URL: "http://new"},
		DrainingTarget: &Target{URL: "http://old"},
		DrainDeadline:  &past,
	}
	s.DropExpiredDraining(time.Now())
	require.Nil(t, s.DrainingTarget)
	require.Nil(t, s.DrainDeadline)
}

func TestDropExpiredDraining_KeepsFutureDeadline(t *testing.T) {
	fut := time.Now().Add(1 * time.Minute)
	s := &Service{
		DrainingTarget: &Target{URL: "http://old"},
		DrainDeadline:  &fut,
	}
	s.DropExpiredDraining(time.Now())
	require.NotNil(t, s.DrainingTarget)
}
