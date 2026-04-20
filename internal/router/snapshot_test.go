package router

import (
	"net/url"
	"testing"
	"time"

	"github.com/crowdy/conoha-proxy/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSnapshot_BuildFromServices(t *testing.T) {
	fut := time.Now().Add(time.Minute)
	svcs := []service.Service{
		{
			Name:         "app1",
			Hosts:        []string{"a.com", "www.a.com"},
			ActiveTarget: &service.Target{URL: "http://127.0.0.1:9001"},
		},
		{
			Name:           "app2",
			Hosts:          []string{"b.com"},
			ActiveTarget:   &service.Target{URL: "http://127.0.0.1:9002"},
			DrainingTarget: &service.Target{URL: "http://127.0.0.1:9003"},
			DrainDeadline:  &fut,
		},
	}
	snap, err := BuildSnapshot(svcs)
	require.NoError(t, err)

	entry, ok := snap.Lookup("a.com")
	require.True(t, ok)
	require.NotNil(t, entry.Active)
	require.Equal(t, "127.0.0.1:9001", entry.Active.Host)

	entry, ok = snap.Lookup("WWW.A.COM")
	require.True(t, ok)

	entry, ok = snap.Lookup("b.com")
	require.True(t, ok)
	require.NotNil(t, entry.Active)
	require.NotNil(t, entry.Draining)
}

func TestSnapshot_Lookup_UnknownHost(t *testing.T) {
	snap, err := BuildSnapshot(nil)
	require.NoError(t, err)
	_, ok := snap.Lookup("unknown.com")
	require.False(t, ok)
}

func TestSnapshot_StripsPort(t *testing.T) {
	snap, err := BuildSnapshot([]service.Service{{
		Name: "a", Hosts: []string{"a.com"},
		ActiveTarget: &service.Target{URL: "http://127.0.0.1:9001"},
	}})
	require.NoError(t, err)
	_, ok := snap.Lookup("a.com:443")
	require.True(t, ok)
}

func TestSnapshot_InvalidTargetURL(t *testing.T) {
	_, err := BuildSnapshot([]service.Service{{
		Name: "a", Hosts: []string{"a.com"},
		ActiveTarget: &service.Target{URL: "://bad"},
	}})
	require.Error(t, err)
}

// Ensure we expose *url.URL for use by ReverseProxy.
func TestSnapshot_ReturnsParsedURLs(t *testing.T) {
	snap, err := BuildSnapshot([]service.Service{{
		Name: "a", Hosts: []string{"a.com"},
		ActiveTarget: &service.Target{URL: "http://127.0.0.1:9001/"},
	}})
	require.NoError(t, err)
	e, _ := snap.Lookup("a.com")
	require.IsType(t, &url.URL{}, e.Active)
}
