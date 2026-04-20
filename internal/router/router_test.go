package router

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/crowdy/conoha-proxy/internal/service"
	"github.com/stretchr/testify/require"
)

func bodyOf(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	b, _ := io.ReadAll(rr.Body)
	return string(b)
}

func TestRouter_421OnUnknownHost(t *testing.T) {
	r := New()
	snap, _ := BuildSnapshot(nil)
	require.NoError(t, r.Reload(snap))

	req := httptest.NewRequest(http.MethodGet, "http://x.example.com/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusMisdirectedRequest, rr.Code)
}

func TestRouter_503WhenActiveNil(t *testing.T) {
	r := New()
	snap, _ := BuildSnapshot([]service.Service{{
		Name: "a", Hosts: []string{"a.com"}, ActiveTarget: nil,
	}})
	require.NoError(t, r.Reload(snap))

	req := httptest.NewRequest(http.MethodGet, "http://a.com/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusServiceUnavailable, rr.Code)
}

func TestRouter_ProxiesToActive(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("from-active"))
	}))
	defer upstream.Close()

	r := New()
	snap, err := BuildSnapshot([]service.Service{{
		Name:         "a",
		Hosts:        []string{"a.com"},
		ActiveTarget: &service.Target{URL: upstream.URL},
	}})
	require.NoError(t, err)
	require.NoError(t, r.Reload(snap))

	req := httptest.NewRequest(http.MethodGet, "http://a.com/", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	require.Equal(t, 200, rr.Code)
	require.Equal(t, "from-active", bodyOf(t, rr))
}

func TestRouter_AtomicReload(t *testing.T) {
	r := New()

	u1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("one"))
	}))
	defer u1.Close()
	u2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("two"))
	}))
	defer u2.Close()

	snap1, _ := BuildSnapshot([]service.Service{{
		Name: "a", Hosts: []string{"a.com"},
		ActiveTarget: &service.Target{URL: u1.URL},
	}})
	require.NoError(t, r.Reload(snap1))

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "http://a.com/", nil))
	require.Equal(t, "one", bodyOf(t, rr))

	snap2, _ := BuildSnapshot([]service.Service{{
		Name: "a", Hosts: []string{"a.com"},
		ActiveTarget: &service.Target{URL: u2.URL},
	}})
	require.NoError(t, r.Reload(snap2))

	rr2 := httptest.NewRecorder()
	r.ServeHTTP(rr2, httptest.NewRequest(http.MethodGet, "http://a.com/", nil))
	require.Equal(t, "two", bodyOf(t, rr2))
}
