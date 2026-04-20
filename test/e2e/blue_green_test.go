//go:build e2e

package e2e

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Scenarios map 1:1 to spec §1.3 MVP Done Definition.
//   1. Service registered (no active) → 503
//   2. Initial deploy → 200 (green)
//   3. Blue/green swap → 200 (new upstream)
//   4. Rollback within drain window → 200 (old upstream returns)
//   5. Probe failure → 424 + state unchanged
//   6. Service delete → 421

func TestE2E_Scenario1_RegisteredButNotDeployed_Returns503(t *testing.T) {
	h := Setup(t)
	t.Cleanup(h.Cleanup)

	resp := h.AdminPOST("/v1/services", map[string]any{
		"name":          "myapp",
		"hosts":         []string{"app.test"},
		"health_policy": map[string]any{"path": "/up"},
	})
	require.Equal(t, 201, resp.StatusCode)

	r := h.HTTPSGet("app.test")
	require.Equal(t, 503, r.StatusCode)
}

func TestE2E_Scenario2_InitialDeploy_Returns200(t *testing.T) {
	h := Setup(t)
	t.Cleanup(h.Cleanup)

	green := h.SpawnUpstream(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "green")
	})

	require.Equal(t, 201, h.AdminPOST("/v1/services", map[string]any{
		"name":  "myapp",
		"hosts": []string{"app.test"},
	}).StatusCode)
	require.Equal(t, 200, h.AdminPOST("/v1/services/myapp/deploy", map[string]any{
		"target_url": green.URL,
	}).StatusCode)

	r := h.HTTPSGet("app.test")
	require.Equal(t, 200, r.StatusCode)
	body, _ := io.ReadAll(r.Body)
	require.Equal(t, "green", string(body))
}

func TestE2E_Scenario3_BlueGreenSwap(t *testing.T) {
	h := Setup(t)
	t.Cleanup(h.Cleanup)

	blue := h.SpawnUpstream(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "blue")
	})
	green := h.SpawnUpstream(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "green")
	})

	h.AdminPOST("/v1/services", map[string]any{"name": "myapp", "hosts": []string{"app.test"}})
	h.AdminPOST("/v1/services/myapp/deploy", map[string]any{"target_url": blue.URL})

	r := h.HTTPSGet("app.test")
	body, _ := io.ReadAll(r.Body)
	require.Equal(t, "blue", string(body))

	require.Equal(t, 200, h.AdminPOST("/v1/services/myapp/deploy", map[string]any{
		"target_url": green.URL,
		"drain_ms":   60000,
	}).StatusCode)

	r = h.HTTPSGet("app.test")
	body, _ = io.ReadAll(r.Body)
	require.Equal(t, "green", string(body))
}

func TestE2E_Scenario4_RollbackWithinDrainWindow(t *testing.T) {
	h := Setup(t)
	t.Cleanup(h.Cleanup)

	blue := h.SpawnUpstream(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "blue")
	})
	green := h.SpawnUpstream(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "green")
	})

	h.AdminPOST("/v1/services", map[string]any{"name": "myapp", "hosts": []string{"app.test"}})
	h.AdminPOST("/v1/services/myapp/deploy", map[string]any{"target_url": blue.URL})
	h.AdminPOST("/v1/services/myapp/deploy", map[string]any{"target_url": green.URL, "drain_ms": 60000})
	require.Equal(t, 200, h.AdminPOST("/v1/services/myapp/rollback", nil).StatusCode)

	r := h.HTTPSGet("app.test")
	body, _ := io.ReadAll(r.Body)
	require.Equal(t, "blue", string(body))
}

func TestE2E_Scenario5_ProbeFailure_Returns424_StateUnchanged(t *testing.T) {
	h := Setup(t)
	t.Cleanup(h.Cleanup)

	good := h.SpawnUpstream(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "good")
	})
	bad := h.SpawnUpstream(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
	})

	h.AdminPOST("/v1/services", map[string]any{
		"name":          "myapp",
		"hosts":         []string{"app.test"},
		"health_policy": map[string]any{"path": "/up", "unhealthy_threshold": 2, "interval_ms": 50, "timeout_ms": 500},
	})
	h.AdminPOST("/v1/services/myapp/deploy", map[string]any{"target_url": good.URL})

	resp := h.AdminPOST("/v1/services/myapp/deploy", map[string]any{"target_url": bad.URL})
	require.Equal(t, http.StatusFailedDependency, resp.StatusCode)

	r := h.HTTPSGet("app.test")
	body, _ := io.ReadAll(r.Body)
	require.Equal(t, "good", string(body))
}

func TestE2E_Scenario6_DeleteService_Returns421(t *testing.T) {
	h := Setup(t)
	t.Cleanup(h.Cleanup)

	green := h.SpawnUpstream(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "green")
	})

	h.AdminPOST("/v1/services", map[string]any{"name": "myapp", "hosts": []string{"app.test"}})
	h.AdminPOST("/v1/services/myapp/deploy", map[string]any{"target_url": green.URL})

	require.Equal(t, 204, h.AdminDelete("/v1/services/myapp").StatusCode)

	r := h.HTTPSGet("app.test")
	require.Equal(t, http.StatusMisdirectedRequest, r.StatusCode)
}

// Sanity: HTTP → HTTPS redirect.
func TestE2E_HTTPRedirectsToHTTPS(t *testing.T) {
	h := Setup(t)
	t.Cleanup(h.Cleanup)

	h.AdminPOST("/v1/services", map[string]any{"name": "myapp", "hosts": []string{"app.test"}})
	r := h.HTTPGet("app.test")
	require.Equal(t, http.StatusMovedPermanently, r.StatusCode)
	require.True(t, strings.HasPrefix(r.Header.Get("Location"), "https://"),
		"want https redirect, got %q", r.Header.Get("Location"))
}
