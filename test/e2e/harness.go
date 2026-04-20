//go:build e2e

// Package e2e bootstraps the full conoha-proxy stack (Pebble ACME server,
// the proxy binary, and configurable fake upstreams) for scenario tests.
package e2e

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
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

	pebbleCmd      *exec.Cmd
	proxyCmd       *exec.Cmd
	PebbleURL      string // ACME directory URL
	PebbleCAPath   string // PEM path for the Pebble TLS listener cert (also used as trust root by the proxy)
	AdminSocket    string
	HTTPSAddr      string
	HTTPAddr       string
	proxyHTTPPort  int
	proxyHTTPSPort int

	dnsConn        net.PacketConn // stub DNS resolver used by Pebble's VA (UDP)
	dnsTCPListener net.Listener   // stub DNS resolver used by Pebble's VA (TCP)

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
	// Reserve proxy ports first so Pebble can target the proxy's HTTP port
	// during HTTP-01 challenge validation.
	h.proxyHTTPPort = freePort(t)
	h.proxyHTTPSPort = freePort(t)
	h.HTTPAddr = fmt.Sprintf("127.0.0.1:%d", h.proxyHTTPPort)
	h.HTTPSAddr = fmt.Sprintf("127.0.0.1:%d", h.proxyHTTPSPort)
	h.startDNS()
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
	if h.dnsConn != nil {
		_ = h.dnsConn.Close()
	}
	if h.dnsTCPListener != nil {
		_ = h.dnsTCPListener.Close()
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
	certPath := filepath.Join(h.DataDir, "pebble-cert.pem")
	keyPath := filepath.Join(h.DataDir, "pebble-key.pem")
	writeSelfSignedCert(h.t, certPath, keyPath)
	h.PebbleCAPath = certPath
	// Point Pebble's HTTP-01 validator at the proxy's HTTP listener.
	writePebbleConfig(h.t, cfg, port, h.proxyHTTPPort, certPath, keyPath)

	args := []string{"-config", cfg}
	if h.dnsConn != nil {
		args = append(args, "-dnsserver", h.dnsConn.LocalAddr().String())
	}
	cmd := exec.Command("pebble", args...)
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

func writePebbleConfig(t *testing.T, path string, dirPort, challengeHTTPPort int, certPath, keyPath string) {
	t.Helper()
	cfg := map[string]any{
		"pebble": map[string]any{
			"listenAddress":                  fmt.Sprintf("0.0.0.0:%d", dirPort),
			"managementListenAddress":        "0.0.0.0:0",
			"certificate":                    certPath,
			"privateKey":                     keyPath,
			"httpPort":                       challengeHTTPPort,
			"tlsPort":                        443,
			"ocspResponderURL":               "",
			"externalAccountBindingRequired": false,
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0o644))
}

// startDNS spins up a tiny stub DNS server on a random UDP+TCP port that
// answers every A query with 127.0.0.1 and every AAAA query with ::1.
// Pebble's HTTP-01 validator uses this resolver (via -dnsserver) so that
// domains like "app.test" point back at the proxy on localhost.
func (h *Harness) startDNS() {
	h.t.Helper()
	// Pick a port via UDP listen so both transports share it.
	udpConn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(h.t, err)
	h.dnsConn = udpConn
	addr := udpConn.LocalAddr().String()

	go func() {
		buf := make([]byte, 1500)
		for {
			n, peer, err := udpConn.ReadFrom(buf)
			if err != nil {
				return // conn closed on Cleanup
			}
			resp := answerDNS(buf[:n])
			if resp != nil {
				_, _ = udpConn.WriteTo(resp, peer)
			}
		}
	}()

	// Pebble's VA may fall back to TCP; bind the same port for TCP.
	tcpLn, err := net.Listen("tcp", addr)
	require.NoError(h.t, err)
	h.dnsTCPListener = tcpLn
	go func() {
		for {
			c, err := tcpLn.Accept()
			if err != nil {
				return
			}
			go handleDNSTCP(c)
		}
	}()
}

// handleDNSTCP answers a single DNS query over TCP. DNS-over-TCP frames
// each message with a 2-byte big-endian length prefix.
func handleDNSTCP(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	var lbuf [2]byte
	if _, err := io.ReadFull(c, lbuf[:]); err != nil {
		return
	}
	qlen := binary.BigEndian.Uint16(lbuf[:])
	if qlen == 0 || qlen > 4096 {
		return
	}
	q := make([]byte, qlen)
	if _, err := io.ReadFull(c, q); err != nil {
		return
	}
	resp := answerDNS(q)
	if resp == nil {
		return
	}
	out := make([]byte, 2+len(resp))
	binary.BigEndian.PutUint16(out[:2], uint16(len(resp)))
	copy(out[2:], resp)
	_, _ = c.Write(out)
}

// answerDNS returns a DNS response for the given query, mapping every
// A to 127.0.0.1 and every AAAA to ::1. Unsupported queries are NXDOMAIN.
// The encoding is intentionally minimal — only what Pebble's VA needs.
func answerDNS(req []byte) []byte {
	if len(req) < 12 {
		return nil
	}
	// Parse question name and type.
	id := binary.BigEndian.Uint16(req[0:2])
	qdCount := binary.BigEndian.Uint16(req[4:6])
	if qdCount != 1 {
		return nil
	}
	// Skip header (12 bytes), walk the QNAME.
	off := 12
	nameStart := off
	for off < len(req) {
		l := int(req[off])
		if l == 0 {
			off++
			break
		}
		if l&0xC0 != 0 { // compression pointer — unexpected in query
			return nil
		}
		off += 1 + l
		if off > len(req) {
			return nil
		}
	}
	if off+4 > len(req) {
		return nil
	}
	qtype := binary.BigEndian.Uint16(req[off : off+2])
	qclass := binary.BigEndian.Uint16(req[off+2 : off+4])
	qend := off + 4
	qname := req[nameStart:off] // includes trailing zero byte

	// Build response.
	var rdata []byte
	switch qtype {
	case 1: // A
		rdata = []byte{127, 0, 0, 1}
	case 28: // AAAA
		rdata = net.ParseIP("::1").To16()
	default:
		// Return empty answer (NOERROR, zero RRs) for other qtypes.
		resp := make([]byte, qend)
		copy(resp, req[:qend])
		binary.BigEndian.PutUint16(resp[0:2], id)
		// flags: QR=1, AA=1, RD copied, RA=0
		resp[2] = 0x84
		resp[3] = 0x00
		binary.BigEndian.PutUint16(resp[4:6], 1)  // QDCOUNT
		binary.BigEndian.PutUint16(resp[6:8], 0)  // ANCOUNT
		binary.BigEndian.PutUint16(resp[8:10], 0) // NSCOUNT
		binary.BigEndian.PutUint16(resp[10:12], 0)
		return resp
	}

	resp := make([]byte, 0, qend+16+len(rdata))
	// Header
	hdr := make([]byte, 12)
	binary.BigEndian.PutUint16(hdr[0:2], id)
	hdr[2] = 0x84 // QR=1, AA=1
	hdr[3] = 0x00
	binary.BigEndian.PutUint16(hdr[4:6], 1)  // QDCOUNT
	binary.BigEndian.PutUint16(hdr[6:8], 1)  // ANCOUNT
	binary.BigEndian.PutUint16(hdr[8:10], 0) // NSCOUNT
	binary.BigEndian.PutUint16(hdr[10:12], 0)
	resp = append(resp, hdr...)
	// Question copy
	resp = append(resp, req[12:qend]...)
	// Answer: NAME(pointer to offset 12) TYPE CLASS TTL RDLENGTH RDATA
	ans := make([]byte, 12+len(rdata))
	ans[0] = 0xC0
	ans[1] = 0x0C // pointer to QNAME at offset 12
	binary.BigEndian.PutUint16(ans[2:4], qtype)
	binary.BigEndian.PutUint16(ans[4:6], qclass)
	binary.BigEndian.PutUint32(ans[6:10], 60) // TTL
	binary.BigEndian.PutUint16(ans[10:12], uint16(len(rdata)))
	copy(ans[12:], rdata)
	resp = append(resp, ans...)
	_ = qname
	return resp
}

// writeSelfSignedCert generates a short-lived ECDSA P-256 self-signed
// certificate valid for localhost / 127.0.0.1 / ::1 and writes PEM files.
// Pebble uses this cert for its ACME directory TLS listener in tests.
func writeSelfSignedCert(t *testing.T, certPath, keyPath string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)

	tmpl := x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "pebble test"},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		BasicConstraintsValid: true,
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)

	certOut, err := os.Create(certPath)
	require.NoError(t, err)
	require.NoError(t, pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}))
	require.NoError(t, certOut.Close())

	keyBytes, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	require.NoError(t, err)
	require.NoError(t, pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}))
	require.NoError(t, keyOut.Close())
}

func (h *Harness) startProxy() {
	adminSock := filepath.Join(h.DataDir, "admin.sock")

	cmd := exec.Command(h.ProxyBinary, "run",
		"--data-dir", h.DataDir,
		"--http-addr", fmt.Sprintf(":%d", h.proxyHTTPPort),
		"--https-addr", fmt.Sprintf(":%d", h.proxyHTTPSPort),
		"--admin-socket", adminSock,
		"--acme-email", "test@example.com",
		"--acme-ca", h.PebbleURL,
	)
	// Make the proxy trust Pebble's self-signed ACME directory cert.
	// Go's crypto/x509 honors SSL_CERT_FILE on Linux; certmagic reuses the
	// default system root pool for ACME HTTPS calls, so this is sufficient.
	cmd.Env = append(os.Environ(),
		"SSL_CERT_FILE="+h.PebbleCAPath,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	require.NoError(h.t, cmd.Start())
	h.proxyCmd = cmd
	h.AdminSocket = adminSock

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
// Retries briefly while certmagic is still obtaining the cert via ACME.
func (h *Harness) HTTPSGet(hostHeader string) *http.Response {
	c := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true, ServerName: hostHeader},
		},
	}
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		req, err := http.NewRequest(http.MethodGet, "https://"+h.HTTPSAddr+"/", nil)
		require.NoError(h.t, err)
		req.Host = hostHeader
		resp, err := c.Do(req)
		if err == nil {
			return resp
		}
		lastErr = err
		// Cert may not be issued yet. Retry on TLS errors until deadline.
		if !strings.Contains(err.Error(), "tls:") {
			h.t.Fatalf("HTTPSGet %q: %v", hostHeader, err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	h.t.Fatalf("HTTPSGet %q never succeeded: %v", hostHeader, lastErr)
	return nil
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
