// Package main is the conoha-proxy binary entrypoint.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/crowdy/conoha-proxy/internal/adminapi"
	"github.com/crowdy/conoha-proxy/internal/health"
	"github.com/crowdy/conoha-proxy/internal/logging"
	"github.com/crowdy/conoha-proxy/internal/router"
	"github.com/crowdy/conoha-proxy/internal/store"
	mytls "github.com/crowdy/conoha-proxy/internal/tls"
	"github.com/spf13/cobra"
)

// Injected at build time via -ldflags.
var (
	version   = "dev"
	commit    = ""
	buildDate = ""
)

func main() {
	root := &cobra.Command{
		Use:   "conoha-proxy",
		Short: "Automatic-HTTPS reverse proxy with blue/green deploys",
	}
	root.AddCommand(runCmd())
	root.AddCommand(versionCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print build version",
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Printf("conoha-proxy %s (commit %s, built %s)\n", version, commit, buildDate)
		},
	}
}

type runFlags struct {
	dataDir   string
	httpAddr  string
	httpsAddr string
	adminSock string
	adminTCP  string
	acmeEmail string
	acmeCA    string
	acmeStage bool
}

func runCmd() *cobra.Command {
	f := &runFlags{}
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start the proxy",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProxy(cmd.Context(), f)
		},
	}
	cmd.Flags().StringVar(&f.dataDir, "data-dir", "/var/lib/conoha-proxy", "persistent state directory")
	cmd.Flags().StringVar(&f.httpAddr, "http-addr", ":80", "HTTP listen addr (ACME + redirect)")
	cmd.Flags().StringVar(&f.httpsAddr, "https-addr", ":443", "HTTPS listen addr (reverse proxy)")
	cmd.Flags().StringVar(&f.adminSock, "admin-socket", "/var/run/conoha-proxy.sock", "Unix socket for admin API (empty to disable)")
	cmd.Flags().StringVar(&f.adminTCP, "admin-tcp", "", "loopback TCP address for admin API (e.g. 127.0.0.1:9999)")
	cmd.Flags().StringVar(&f.acmeEmail, "acme-email", "", "ACME account email (required)")
	cmd.Flags().StringVar(&f.acmeCA, "acme-ca", "", "override ACME directory URL (for Pebble in tests)")
	cmd.Flags().BoolVar(&f.acmeStage, "acme-staging", false, "use Let's Encrypt staging")
	return cmd
}

func runProxy(ctx context.Context, f *runFlags) error {
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	logger := logging.New(os.Stdout, slog.LevelInfo)
	logger.Info("starting", "version", version, "commit", commit)

	if f.acmeEmail == "" && f.acmeCA == "" {
		return errors.New("--acme-email is required (or set --acme-ca for custom CA)")
	}

	if err := os.MkdirAll(f.dataDir, 0o750); err != nil {
		return fmt.Errorf("mkdir data dir: %w", err)
	}

	// Wire components.
	st, err := store.Open(filepath.Join(f.dataDir, "state.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	rt := router.New()
	checker := health.NewHTTPChecker()

	cm, err := mytls.New(mytls.Config{
		StorageDir: filepath.Join(f.dataDir, "certs"),
		Email:      f.acmeEmail,
		CADirURL:   f.acmeCA,
		Staging:    f.acmeStage,
	})
	if err != nil {
		return err
	}

	handlers := adminapi.NewHandlers(st, rt, checker, cm)

	// Initial snapshot from persisted state.
	svcs, err := st.LoadAll(ctx)
	if err != nil {
		return err
	}
	snap, err := router.BuildSnapshot(svcs)
	if err != nil {
		return err
	}
	if err := rt.Reload(snap); err != nil {
		return err
	}
	// Re-manage any persisted domains.
	var allDomains []string
	for _, s := range svcs {
		allDomains = append(allDomains, s.Hosts...)
	}
	if err := cm.ManageDomains(allDomains); err != nil {
		return err
	}

	// HTTP (ACME + redirect).
	httpSrv := &http.Server{
		Addr:              f.httpAddr,
		Handler:           cm.HTTPChallengeHandler(redirectToHTTPS()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	// HTTPS (reverse proxy).
	httpsSrv := &http.Server{
		Addr:              f.httpsAddr,
		Handler:           rt,
		TLSConfig:         cm.TLSConfig(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	// Admin listener(s).
	var adminServers []*adminapi.Server
	if f.adminSock != "" {
		s, err := adminapi.NewServer(adminapi.ListenerConfig{UnixSocket: f.adminSock}, handlers)
		if err != nil {
			return err
		}
		adminServers = append(adminServers, s)
	}
	if f.adminTCP != "" {
		s, err := adminapi.NewServer(adminapi.ListenerConfig{TCPAddr: f.adminTCP}, handlers)
		if err != nil {
			return err
		}
		adminServers = append(adminServers, s)
	}
	if len(adminServers) == 0 {
		return errors.New("at least one of --admin-socket or --admin-tcp must be set")
	}

	// Serve.
	errCh := make(chan error, 3+len(adminServers))

	go func() { errCh <- httpSrv.ListenAndServe() }()
	go func() { errCh <- httpsSrv.ListenAndServeTLS("", "") }()
	for _, s := range adminServers {
		s := s
		go func() { errCh <- s.Serve(ctx) }()
	}

	// Wait for shutdown or first error.
	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listener error", "err", err)
			return err
		}
	}

	// Graceful shutdown.
	shutCtx, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()
	_ = httpSrv.Shutdown(shutCtx)
	_ = httpsSrv.Shutdown(shutCtx)
	return nil
}

func redirectToHTTPS() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := "https://" + r.Host + r.URL.RequestURI()
		http.Redirect(w, r, target, http.StatusMovedPermanently)
	})
}
