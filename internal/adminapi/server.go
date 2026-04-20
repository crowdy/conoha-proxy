package adminapi

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// ListenerConfig configures one Admin API listener.
type ListenerConfig struct {
	// UnixSocket is a filesystem path; if set, takes precedence.
	UnixSocket string
	// TCPAddr is host:port. Must resolve to a loopback address.
	TCPAddr string
	// SocketMode is the unix-socket file permission (e.g. 0660).
	SocketMode os.FileMode
}

// Server wraps *http.Server and the underlying listener.
type Server struct {
	http *http.Server
	ln   net.Listener
}

// NewServer constructs an Admin API Server. Exactly one of UnixSocket or
// TCPAddr must be set. TCP binds that do not resolve to loopback are
// rejected with an error.
func NewServer(cfg ListenerConfig, handler http.Handler) (*Server, error) {
	switch {
	case cfg.UnixSocket != "" && cfg.TCPAddr != "":
		return nil, errors.New("set exactly one of UnixSocket or TCPAddr")
	case cfg.UnixSocket == "" && cfg.TCPAddr == "":
		return nil, errors.New("set exactly one of UnixSocket or TCPAddr")
	}

	var ln net.Listener
	var err error
	if cfg.UnixSocket != "" {
		_ = os.Remove(cfg.UnixSocket) // best effort
		ln, err = net.Listen("unix", cfg.UnixSocket)
		if err != nil {
			return nil, fmt.Errorf("listen unix: %w", err)
		}
		mode := cfg.SocketMode
		if mode == 0 {
			mode = 0o660
		}
		if err := os.Chmod(cfg.UnixSocket, mode); err != nil {
			_ = ln.Close()
			return nil, fmt.Errorf("chmod socket: %w", err)
		}
	} else {
		if err := requireLoopback(cfg.TCPAddr); err != nil {
			return nil, err
		}
		ln, err = net.Listen("tcp", cfg.TCPAddr)
		if err != nil {
			return nil, fmt.Errorf("listen tcp: %w", err)
		}
	}

	// Timeouts are deliberately generous: deploy calls include a health
	// probe that may run for several seconds before responding.
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	return &Server{http: srv, ln: ln}, nil
}

// Serve blocks until the context is canceled or the listener errors.
func (s *Server) Serve(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() { errCh <- s.http.Serve(s.ln) }()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return s.http.Shutdown(shutCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// Addr returns the listening address (socket path or host:port).
func (s *Server) Addr() string { return s.ln.Addr().String() }

func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid tcp addr %q: %w", addr, err)
	}
	// Treat empty host (":port") as 0.0.0.0 — reject.
	if host == "" || host == "0.0.0.0" || host == "::" {
		return fmt.Errorf("admin TCP bind must be loopback; got %q", addr)
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil {
		// hostname like "localhost" — check resolution
		ips, err := net.LookupIP(host)
		if err != nil {
			return fmt.Errorf("resolve %q: %w", host, err)
		}
		for _, i := range ips {
			if !i.IsLoopback() {
				return fmt.Errorf("admin TCP bind must be loopback; %q resolves to non-loopback %s", host, i)
			}
		}
		return nil
	}
	if !ip.IsLoopback() {
		return fmt.Errorf("admin TCP bind must be loopback; got %s", ip)
	}
	return nil
}
