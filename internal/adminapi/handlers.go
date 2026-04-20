package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/crowdy/conoha-proxy/internal/health"
	"github.com/crowdy/conoha-proxy/internal/router"
	"github.com/crowdy/conoha-proxy/internal/service"
	"github.com/crowdy/conoha-proxy/internal/store"
)

// TLSManager is the subset of the TLS component that the admin API uses.
type TLSManager interface {
	ManageDomains(domains []string) error
}

// Handlers is the admin HTTP API handler stack.
type Handlers struct {
	store   store.Store
	router  *router.Router
	checker health.Checker
	tls     TLSManager

	defaultDrain time.Duration
}

// NewHandlers constructs a Handlers with a 30s default drain window.
func NewHandlers(st store.Store, r *router.Router, c health.Checker, t TLSManager) *Handlers {
	return &Handlers{
		store:        st,
		router:       r,
		checker:      c,
		tls:          t,
		defaultDrain: 30 * time.Second,
	}
}

// ServeHTTP routes requests to the appropriate handler. The routing table is
// small enough to stay here — adding chi/mux would be overkill for 8 routes.
func (h *Handlers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Path

	switch {
	case p == "/healthz" && r.Method == http.MethodGet:
		writeJSON(w, 200, map[string]string{"status": "ok"})
	case p == "/readyz" && r.Method == http.MethodGet:
		writeJSON(w, 200, map[string]string{"status": "ok"})
	case p == "/version" && r.Method == http.MethodGet:
		writeJSON(w, 200, map[string]string{"version": VersionString()})

	case p == "/v1/services" && r.Method == http.MethodPost:
		h.handleUpsert(w, r)
	case p == "/v1/services" && r.Method == http.MethodGet:
		h.handleList(w, r)

	case strings.HasPrefix(p, "/v1/services/"):
		h.serveServiceSub(w, r)

	default:
		writeError(w, http.StatusNotFound, "not_found", "no such route")
	}
}

func (h *Handlers) serveServiceSub(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/v1/services/")
	parts := strings.Split(rest, "/")
	name := parts[0]
	if name == "" {
		writeError(w, http.StatusNotFound, "not_found", "service name missing")
		return
	}

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			h.handleGet(w, r, name)
		case http.MethodDelete:
			h.handleDelete(w, r, name)
		default:
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
		}
		return
	}

	switch {
	case len(parts) == 2 && parts[1] == "deploy" && r.Method == http.MethodPost:
		h.handleDeploy(w, r, name)
	case len(parts) == 2 && parts[1] == "rollback" && r.Method == http.MethodPost:
		h.handleRollback(w, r, name)
	default:
		writeError(w, http.StatusNotFound, "not_found", "")
	}
}

// --- upsert / list / get / delete ---

type upsertRequest struct {
	Name         string               `json:"name"`
	Hosts        []string             `json:"hosts"`
	HealthPolicy service.HealthPolicy `json:"health_policy"`
}

func (h *Handlers) handleUpsert(w http.ResponseWriter, r *http.Request) {
	var body upsertRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	svc := service.Service{
		Name:         body.Name,
		Hosts:        body.Hosts,
		HealthPolicy: body.HealthPolicy.WithDefaults(),
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := svc.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	if err := h.store.SaveService(r.Context(), svc); err != nil {
		writeError(w, http.StatusServiceUnavailable, "store_error", err.Error())
		return
	}
	if err := h.tls.ManageDomains(svc.Hosts); err != nil {
		writeError(w, http.StatusServiceUnavailable, "tls_error", err.Error())
		return
	}
	if err := h.reloadRouter(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "reload_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, svc)
}

func (h *Handlers) handleList(w http.ResponseWriter, r *http.Request) {
	svcs, err := h.store.LoadAll(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "store_error", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"services": svcs})
}

func (h *Handlers) handleGet(w http.ResponseWriter, r *http.Request, name string) {
	svc, ok, err := h.findService(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "store_error", err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "service not found")
		return
	}
	writeJSON(w, 200, svc)
}

func (h *Handlers) handleDelete(w http.ResponseWriter, r *http.Request, name string) {
	if err := h.store.DeleteService(r.Context(), name); err != nil {
		writeError(w, http.StatusServiceUnavailable, "store_error", err.Error())
		return
	}
	if err := h.reloadRouter(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "reload_failed", err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- deploy / rollback ---

type deployRequest struct {
	TargetURL string `json:"target_url"`
	DrainMs   int    `json:"drain_ms"`
}

func (h *Handlers) handleDeploy(w http.ResponseWriter, r *http.Request, name string) {
	var body deployRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	newTarget := service.Target{URL: body.TargetURL, DeployedAt: time.Now().UTC()}
	if err := newTarget.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	svc, ok, err := h.findService(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "store_error", err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "service not found")
		return
	}

	if err := h.checker.ProbeOnce(r.Context(), newTarget, svc.HealthPolicy); err != nil {
		writeError(w, http.StatusFailedDependency, "probe_failed", err.Error())
		return
	}

	drain := time.Duration(body.DrainMs) * time.Millisecond
	if drain <= 0 {
		drain = h.defaultDrain
	}
	deadline := time.Now().Add(drain).UTC()

	if svc.ActiveTarget != nil {
		svc.DrainingTarget = svc.ActiveTarget
		svc.DrainDeadline = &deadline
	}
	svc.ActiveTarget = &newTarget
	svc.Touch()

	if err := h.store.SaveService(r.Context(), svc); err != nil {
		writeError(w, http.StatusServiceUnavailable, "store_error", err.Error())
		return
	}
	if err := h.reloadRouter(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "reload_failed", err.Error())
		return
	}
	writeJSON(w, 200, svc)
}

func (h *Handlers) handleRollback(w http.ResponseWriter, r *http.Request, name string) {
	svc, ok, err := h.findService(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "store_error", err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "service not found")
		return
	}
	if svc.DrainingTarget == nil {
		writeError(w, http.StatusConflict, "no_drain_target", "no draining target to roll back to")
		return
	}
	svc.ActiveTarget, svc.DrainingTarget = svc.DrainingTarget, svc.ActiveTarget
	deadline := time.Now().Add(h.defaultDrain).UTC()
	svc.DrainDeadline = &deadline
	svc.Touch()

	if err := h.store.SaveService(r.Context(), svc); err != nil {
		writeError(w, http.StatusServiceUnavailable, "store_error", err.Error())
		return
	}
	if err := h.reloadRouter(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "reload_failed", err.Error())
		return
	}
	writeJSON(w, 200, svc)
}

// --- helpers ---

func (h *Handlers) findService(ctx context.Context, name string) (service.Service, bool, error) {
	svcs, err := h.store.LoadAll(ctx)
	if err != nil {
		return service.Service{}, false, err
	}
	for _, s := range svcs {
		if s.Name == name {
			return s, true, nil
		}
	}
	return service.Service{}, false, nil
}

func (h *Handlers) reloadRouter(ctx context.Context) error {
	svcs, err := h.store.LoadAll(ctx)
	if err != nil {
		return err
	}
	snap, err := router.BuildSnapshot(svcs)
	if err != nil {
		return err
	}
	return h.router.Reload(snap)
}

// VersionString is injected at build time via -ldflags.
// Unset during tests.
var version = "dev"

// VersionString returns the build version.
func VersionString() string { return version }

// compile-time interface check
var _ http.Handler = (*Handlers)(nil)

// errors.Is compatibility
var _ = errors.New
