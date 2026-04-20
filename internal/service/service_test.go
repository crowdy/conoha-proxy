package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestService_Validate_Ok(t *testing.T) {
	s := Service{
		Name:  "myapp",
		Hosts: []string{"a.example.com"},
		HealthPolicy: HealthPolicy{
			Path:               "/up",
			IntervalMs:         5000,
			TimeoutMs:          2000,
			HealthyThreshold:   1,
			UnhealthyThreshold: 3,
		},
	}
	require.NoError(t, s.Validate())
}

func TestService_Validate_RejectsEmptyName(t *testing.T) {
	s := Service{Hosts: []string{"a.example.com"}}
	require.ErrorContains(t, s.Validate(), "name")
}

func TestService_Validate_RejectsNoHosts(t *testing.T) {
	s := Service{Name: "myapp"}
	require.ErrorContains(t, s.Validate(), "hosts")
}

func TestService_Validate_RejectsInvalidHost(t *testing.T) {
	s := Service{Name: "myapp", Hosts: []string{""}}
	require.ErrorContains(t, s.Validate(), "host")
}

func TestService_Validate_RejectsDuplicateHosts(t *testing.T) {
	s := Service{
		Name:  "myapp",
		Hosts: []string{"a.com", "a.com"},
	}
	require.ErrorContains(t, s.Validate(), "duplicate")
}

func TestTarget_Validate_RequiresHTTPURL(t *testing.T) {
	tg := Target{URL: "ftp://bad"}
	require.ErrorContains(t, tg.Validate(), "http")
}

func TestHealthPolicy_WithDefaults(t *testing.T) {
	p := HealthPolicy{}.WithDefaults()
	require.Equal(t, "/up", p.Path)
	require.Equal(t, 5000, p.IntervalMs)
	require.Equal(t, 2000, p.TimeoutMs)
	require.Equal(t, 1, p.HealthyThreshold)
	require.Equal(t, 3, p.UnhealthyThreshold)
}

func TestHealthPolicy_WithDefaults_PreservesExplicit(t *testing.T) {
	p := HealthPolicy{Path: "/ping", TimeoutMs: 999}.WithDefaults()
	require.Equal(t, "/ping", p.Path)
	require.Equal(t, 999, p.TimeoutMs)
	require.Equal(t, 5000, p.IntervalMs) // still defaulted
}

func TestService_MatchesHost_CaseInsensitive(t *testing.T) {
	s := Service{Hosts: []string{"A.Example.Com"}}
	require.True(t, s.MatchesHost("a.example.com"))
	require.True(t, s.MatchesHost("A.EXAMPLE.COM"))
	require.False(t, s.MatchesHost("b.example.com"))
}

func TestService_MatchesHost_StripsPort(t *testing.T) {
	s := Service{Hosts: []string{"a.example.com"}}
	require.True(t, s.MatchesHost("a.example.com:8443"))
}

func TestService_Touch(t *testing.T) {
	s := Service{}
	before := time.Now()
	s.Touch()
	require.True(t, !s.UpdatedAt.Before(before))
}
