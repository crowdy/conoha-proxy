package adminapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/crowdy/conoha-proxy/internal/router"
	"github.com/crowdy/conoha-proxy/internal/service"
	"github.com/stretchr/testify/require"
)

// --- test doubles ---

type fakeStore struct {
	mu       sync.Mutex
	services map[string]service.Service
}

func newFakeStore() *fakeStore { return &fakeStore{services: map[string]service.Service{}} }

func (f *fakeStore) LoadAll(_ context.Context) ([]service.Service, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]service.Service, 0, len(f.services))
	for _, s := range f.services {
		out = append(out, s)
	}
	return out, nil
}
func (f *fakeStore) SaveService(_ context.Context, s service.Service) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.services[s.Name] = s
	return nil
}
func (f *fakeStore) DeleteService(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.services, name)
	return nil
}
func (f *fakeStore) Close() error { return nil }

type fakeChecker struct{ err error }

func (f *fakeChecker) ProbeOnce(_ context.Context, _ service.Target, _ service.HealthPolicy) error {
	return f.err
}

type fakeTLS struct{ managed []string }

func (f *fakeTLS) ManageDomains(ds []string) error { f.managed = ds; return nil }

// --- helpers ---

func newHarness(t *testing.T, probeErr error) (*Handlers, *router.Router, *fakeStore, *fakeTLS) {
	t.Helper()
	st := newFakeStore()
	r := router.New()
	tlsMgr := &fakeTLS{}
	h := NewHandlers(st, r, &fakeChecker{err: probeErr}, tlsMgr)
	return h, r, st, tlsMgr
}

func do(h *Handlers, method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// --- tests ---

func TestPostServices_Creates(t *testing.T) {
	h, _, st, tlsMgr := newHarness(t, nil)

	rr := do(h, http.MethodPost, "/v1/services", map[string]any{
		"name":  "myapp",
		"hosts": []string{"a.example.com"},
	})
	require.Equal(t, http.StatusCreated, rr.Code)

	got, _ := st.LoadAll(context.Background())
	require.Len(t, got, 1)
	require.Equal(t, "myapp", got[0].Name)
	require.Equal(t, []string{"a.example.com"}, tlsMgr.managed)
}

func TestPostServices_RejectsDuplicateHosts(t *testing.T) {
	h, _, _, _ := newHarness(t, nil)
	rr := do(h, http.MethodPost, "/v1/services", map[string]any{
		"name":  "a",
		"hosts": []string{"x.com", "x.com"},
	})
	require.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestGetServices_Lists(t *testing.T) {
	h, _, st, _ := newHarness(t, nil)
	_ = st.SaveService(context.Background(), service.Service{Name: "a", Hosts: []string{"a.com"}})

	rr := do(h, http.MethodGet, "/v1/services", nil)
	require.Equal(t, http.StatusOK, rr.Code)
	var resp struct {
		Services []service.Service `json:"services"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Services, 1)
}

func TestGetServiceByName_404(t *testing.T) {
	h, _, _, _ := newHarness(t, nil)
	rr := do(h, http.MethodGet, "/v1/services/missing", nil)
	require.Equal(t, http.StatusNotFound, rr.Code)
}

func TestDeploy_HappyPath(t *testing.T) {
	h, _, st, _ := newHarness(t, nil)
	_ = st.SaveService(context.Background(), service.Service{
		Name: "myapp", Hosts: []string{"a.com"},
	})

	rr := do(h, http.MethodPost, "/v1/services/myapp/deploy", map[string]any{
		"target_url": "http://127.0.0.1:9001",
		"drain_ms":   30000,
	})
	require.Equal(t, http.StatusOK, rr.Code)

	got, _ := st.LoadAll(context.Background())
	require.NotNil(t, got[0].ActiveTarget)
	require.Equal(t, "http://127.0.0.1:9001", got[0].ActiveTarget.URL)
}

func TestDeploy_ProbeFailureReturns424(t *testing.T) {
	h, _, st, _ := newHarness(t, errFake)
	_ = st.SaveService(context.Background(), service.Service{
		Name: "myapp", Hosts: []string{"a.com"},
		ActiveTarget: &service.Target{URL: "http://old"},
	})

	rr := do(h, http.MethodPost, "/v1/services/myapp/deploy", map[string]any{
		"target_url": "http://127.0.0.1:9002",
	})
	require.Equal(t, http.StatusFailedDependency, rr.Code)

	// State unchanged
	got, _ := st.LoadAll(context.Background())
	require.Equal(t, "http://old", got[0].ActiveTarget.URL)
	require.Nil(t, got[0].DrainingTarget)
}

func TestDeploy_SwapMovesActiveToDraining(t *testing.T) {
	h, _, st, _ := newHarness(t, nil)
	_ = st.SaveService(context.Background(), service.Service{
		Name: "myapp", Hosts: []string{"a.com"},
		ActiveTarget: &service.Target{URL: "http://old"},
	})

	rr := do(h, http.MethodPost, "/v1/services/myapp/deploy", map[string]any{
		"target_url": "http://new",
		"drain_ms":   60000,
	})
	require.Equal(t, http.StatusOK, rr.Code)

	got, _ := st.LoadAll(context.Background())
	require.Equal(t, "http://new", got[0].ActiveTarget.URL)
	require.NotNil(t, got[0].DrainingTarget)
	require.Equal(t, "http://old", got[0].DrainingTarget.URL)
	require.NotNil(t, got[0].DrainDeadline)
	require.True(t, got[0].DrainDeadline.After(time.Now()))
}

func TestRollback_ReversesActiveAndDraining(t *testing.T) {
	h, _, st, _ := newHarness(t, nil)
	fut := time.Now().Add(1 * time.Minute)
	_ = st.SaveService(context.Background(), service.Service{
		Name: "myapp", Hosts: []string{"a.com"},
		ActiveTarget:   &service.Target{URL: "http://new"},
		DrainingTarget: &service.Target{URL: "http://old"},
		DrainDeadline:  &fut,
	})

	rr := do(h, http.MethodPost, "/v1/services/myapp/rollback", nil)
	require.Equal(t, http.StatusOK, rr.Code)

	got, _ := st.LoadAll(context.Background())
	require.Equal(t, "http://old", got[0].ActiveTarget.URL)
	require.Equal(t, "http://new", got[0].DrainingTarget.URL)
}

func TestRollback_NoDrainingReturns409(t *testing.T) {
	h, _, st, _ := newHarness(t, nil)
	_ = st.SaveService(context.Background(), service.Service{
		Name: "myapp", Hosts: []string{"a.com"},
		ActiveTarget: &service.Target{URL: "http://only"},
	})

	rr := do(h, http.MethodPost, "/v1/services/myapp/rollback", nil)
	require.Equal(t, http.StatusConflict, rr.Code)
}

func TestDeleteService(t *testing.T) {
	h, _, st, _ := newHarness(t, nil)
	_ = st.SaveService(context.Background(), service.Service{Name: "a", Hosts: []string{"a.com"}})

	rr := do(h, http.MethodDelete, "/v1/services/a", nil)
	require.Equal(t, http.StatusNoContent, rr.Code)

	got, _ := st.LoadAll(context.Background())
	require.Empty(t, got)
}

func TestHealthz(t *testing.T) {
	h, _, _, _ := newHarness(t, nil)
	rr := do(h, http.MethodGet, "/healthz", nil)
	require.Equal(t, http.StatusOK, rr.Code)
}

// sentinel error for probe failure
var errFake = &fakeErr{}

type fakeErr struct{}

func (*fakeErr) Error() string { return "fake probe error" }
