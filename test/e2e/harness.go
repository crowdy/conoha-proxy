//go:build e2e

// Package e2e bootstraps the full conoha-proxy stack (Pebble ACME server,
// the proxy binary, and configurable fake upstreams) for scenario tests.
package e2e

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Harness holds all processes & endpoints for one e2e test.
type Harness struct {
	t           *testing.T
	ProxyBinary string
	DataDir     string

	pebbleCmd   *exec.Cmd
	proxyCmd    *exec.Cmd
	PebbleURL   string // ACME directory URL
	AdminSocket string
	HTTPSAddr   string
	HTTPAddr    string

	mu        sync.Mutex
	upstreams []*httptest.Server
}

// Setup builds the binary and launches Pebble + proxy. Caller must Cleanup.
// Requires Pebble installed: go install github.com/letsencrypt/pebble/v2/cmd/pebble@latest
func Setup(t *testing.T) *Harness {
	t.Helper()
	if os.Getenv("E2E") == "" {
		t.Skip("set E2E=1 to run e2e tests")
	}
	h := &Harness{t: t, DataDir: t.TempDir()}
	h.buildBinary()
	h.startPebble()
	h.startProxy()
	return h
}

// Cleanup terminates all child processes.
func (h *Harness) Cleanup() {
	h.mu.Lock()
	for _, u := range h.upstreams {
		u.Close()
	}
	h.mu.Unlock()
	if h.proxyCmd != nil && h.proxyCmd.Process != nil {
		_ = h.proxyCmd.Process.Kill()
		_, _ = h.proxyCmd.Process.Wait()
	}
	if h.pebbleCmd != nil && h.pebbleCmd.Process != nil {
		_ = h.pebbleCmd.Process.Kill()
		_, _ = h.pebbleCmd.Process.Wait()
	}
}

func (h *Harness) buildBinary() {
	out := filepath.Join(h.DataDir, "conoha-proxy")
	cmd := exec.Command("go", "build", "-o", out, "../../cmd/conoha-proxy")
	cmd.Stderr = os.Stderr
	require.NoError(h.t, cmd.Run())
	h.ProxyBinary = out
}

func (h *Harness) startPebble() {
	port := freePort(h.t)
	h.PebbleURL = fmt.Sprintf("https://localhost:%d/dir", port)
	cfg := filepath.Join(h.DataDir, "pebble-config.json")
	writePebbleConfig(h.t, cfg, port)

	cmd := exec.Command("pebble", "-config", cfg)
	cmd.Env = append(os.Environ(),
		"PEBBLE_VA_NOSLEEP=1",
		"PEBBLE_WFE_NONCEREJECT=0",
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	require.NoError(h.t, cmd.Start())
	h.pebbleCmd = cmd
	waitTCP(h.t, fmt.Sprintf("localhost:%d", port), 5*time.Second)
}

func writePebbleConfig(t *testing.T, path string, dirPort int) {
	t.Helper()
	cfg := map[string]any{
		"pebble": map[string]any{
			"listenAddress":                  fmt.Sprintf("0.0.0.0:%d", dirPort),
			"managementListenAddress":        "0.0.0.0:0",
			"httpPort":                       80,
			"tlsPort":                        443,
			"ocspResponderURL":               "",
			"externalAccountBindingRequired": false,
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

func (h *Harness) startProxy() {
	adminSock := filepath.Join(h.DataDir, "admin.sock")
	httpPort := freePort(h.t)
	httpsPort := freePort(h.t)

	cmd := exec.Command(h.ProxyBinary, "run",
		"--data-dir", h.DataDir,
		"--http-addr", fmt.Sprintf(":%d", httpPort),
		"--https-addr", fmt.Sprintf(":%d", httpsPort),
		"--admin-socket", adminSock,
		"--acme-email", "test@example.com",
		"--acme-ca", h.PebbleURL,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	require.NoError(h.t, cmd.Start())
	h.proxyCmd = cmd
	h.AdminSocket = adminSock
	h.HTTPAddr = fmt.Sprintf("127.0.0.1:%d", httpPort)
	h.HTTPSAddr = fmt.Sprintf("127.0.0.1:%d", httpsPort)

	// Wait for admin socket to exist.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(adminSock); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	h.t.Fatal("proxy admin socket never appeared")
}

// SpawnUpstream starts an httptest server and registers it for cleanup.
func (h *Harness) SpawnUpstream(handler http.HandlerFunc) *httptest.Server {
	srv := httptest.NewServer(handler)
	h.mu.Lock()
	h.upstreams = append(h.upstreams, srv)
	h.mu.Unlock()
	return srv
}

// AdminPOST JSON-encodes body and POSTs to path via the unix socket.
func (h *Harness) AdminPOST(path string, body any) *http.Response {
	return h.adminReq(http.MethodPost, path, body)
}

func (h *Harness) AdminDelete(path string) *http.Response {
	return h.adminReq(http.MethodDelete, path, nil)
}

func (h *Harness) AdminGet(path string) *http.Response {
	return h.adminReq(http.MethodGet, path, nil)
}

func (h *Harness) adminReq(method, path string, body any) *http.Response {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", h.AdminSocket)
			},
		},
	}
	var rdr io.Reader
	if body != nil {
		data, _ := json.Marshal(body)
		rdr = strings.NewReader(string(data))
	}
	req, err := http.NewRequest(method, "http://admin"+path, rdr)
	require.NoError(h.t, err)
	resp, err := client.Do(req)
	require.NoError(h.t, err)
	return resp
}

// HTTPGet performs a plain-HTTP request to the proxy's HTTP port using the
// given Host header. Redirects are NOT followed so we can observe 301.
func (h *Harness) HTTPGet(hostHeader string) *http.Response {
	req, err := http.NewRequest(http.MethodGet, "http://"+h.HTTPAddr+"/", nil)
	require.NoError(h.t, err)
	req.Host = hostHeader
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := c.Do(req)
	require.NoError(h.t, err)
	return resp
}

// HTTPSGet performs an HTTPS request to the proxy with SNI = hostHeader,
// trusting the Pebble-issued chain (InsecureSkipVerify for test purposes).
func (h *Harness) HTTPSGet(hostHeader string) *http.Response {
	c := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, ServerName: hostHeader},
		},
	}
	req, err := http.NewRequest(http.MethodGet, "https://"+h.HTTPSAddr+"/", nil)
	require.NoError(h.t, err)
	req.Host = hostHeader
	resp, err := c.Do(req)
	require.NoError(h.t, err)
	return resp
}

// --- utils ---

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitTCP(t *testing.T, addr string, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("tcp %s never became reachable", addr)
}
