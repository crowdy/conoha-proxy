package adminapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
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

	// perService serializes writes to the same service name so concurrent
	// deploy/rollback/upsert/delete calls cannot clobber each other.
	perService sync.Map // name → *sync.Mutex
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
// small enough to stay here — adding chi/mux would be overkill for 9 routes.
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

	unlock := h.lockService(body.Name)
	defer unlock()

	now := time.Now().UTC()
	svc := service.Service{
		Name:         body.Name,
		Hosts:        body.Hosts,
		HealthPolicy: body.HealthPolicy.WithDefaults(),
		UpdatedAt:    now,
	}

	// Preserve active/draining/deadline/created_at on re-upsert (contract
	// documented in admin-api.md). Only hosts and health_policy change.
	existing, found, err := h.findService(r.Context(), body.Name)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "store_error", err.Error())
		return
	}
	if found {
		svc.ActiveTarget = existing.ActiveTarget
		svc.DrainingTarget = existing.DrainingTarget
		svc.DrainDeadline = existing.DrainDeadline
		svc.CreatedAt = existing.CreatedAt
	} else {
		svc.CreatedAt = now
	}

	if err := svc.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	if err := h.store.SaveService(r.Context(), svc); err != nil {
		writeError(w, http.StatusServiceUnavailable, "store_error", err.Error())
		return
	}
	if err := h.syncTLSDomains(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "tls_error", err.Error())
		return
	}
	if err := h.reloadRouter(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "reload_failed", err.Error())
		return
	}

	status := http.StatusCreated
	if found {
		status = http.StatusOK
	}
	writeJSON(w, status, viewOf(svc, time.Now()))
}

func (h *Handlers) handleList(w http.ResponseWriter, r *http.Request) {
	svcs, err := h.store.LoadAll(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "store_error", err.Error())
		return
	}
	now := time.Now()
	views := make([]serviceView, 0, len(svcs))
	for _, s := range svcs {
		views = append(views, viewOf(s, now))
	}
	writeJSON(w, 200, map[string]any{"services": views})
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
	writeJSON(w, 200, viewOf(svc, time.Now()))
}

func (h *Handlers) handleDelete(w http.ResponseWriter, r *http.Request, name string) {
	unlock := h.lockService(name)
	defer unlock()

	if err := h.store.DeleteService(r.Context(), name); err != nil {
		writeError(w, http.StatusServiceUnavailable, "store_error", err.Error())
		return
	}
	if err := h.syncTLSDomains(r.Context()); err != nil {
		writeError(w, http.StatusServiceUnavailable, "tls_error", err.Error())
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

	unlock := h.lockService(name)
	defer unlock()

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
	// A stale draining target that outlived its window must not be promoted
	// back to draining on the next swap — drop it before recomputing.
	svc.DropExpiredDraining(time.Now())

	if err := h.checker.ProbeOnce(r.Context(), newTarget, svc.HealthPolicy); err != nil {
		writeError(w, http.StatusFailedDependency, "probe_failed", err.Error())
		return
	}

	drain := resolveDrain(body.DrainMs, h.defaultDrain)
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
	writeJSON(w, 200, viewOf(svc, time.Now()))
}

type rollbackRequest struct {
	DrainMs int `json:"drain_ms"`
}

func (h *Handlers) handleRollback(w http.ResponseWriter, r *http.Request, name string) {
	var body rollbackRequest
	// Rollback body is optional — a missing/empty body means "use default drain".
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
	}

	unlock := h.lockService(name)
	defer unlock()

	svc, ok, err := h.findService(r.Context(), name)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "store_error", err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "service not found")
		return
	}
	// Drop a draining target whose window has already closed — rollback past
	// the drain deadline would point traffic at a container the operator
	// reaped long ago.
	svc.DropExpiredDraining(time.Now())
	if svc.DrainingTarget == nil {
		writeError(w, http.StatusConflict, "no_drain_target", "no draining target to roll back to")
		return
	}
	svc.ActiveTarget, svc.DrainingTarget = svc.DrainingTarget, svc.ActiveTarget
	drain := resolveDrain(body.DrainMs, h.defaultDrain)
	deadline := time.Now().Add(drain).UTC()
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
	writeJSON(w, 200, viewOf(svc, time.Now()))
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

// syncTLSDomains passes the UNION of hosts across ALL stored services to
// the TLS manager. Calling ManageDomains with only one service's hosts
// would drop every other service's domains from renewal.
func (h *Handlers) syncTLSDomains(ctx context.Context) error {
	svcs, err := h.store.LoadAll(ctx)
	if err != nil {
		return err
	}
	seen := make(map[string]struct{})
	all := make([]string, 0)
	for _, s := range svcs {
		for _, host := range s.Hosts {
			key := strings.TrimSpace(strings.ToLower(host))
			if key == "" {
				continue
			}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			all = append(all, key)
		}
	}
	return h.tls.ManageDomains(all)
}

// lockService acquires the per-name mutex and returns the unlock func.
// Callers MUST `defer unlock()`.
func (h *Handlers) lockService(name string) func() {
	val, _ := h.perService.LoadOrStore(name, &sync.Mutex{})
	m := val.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

func resolveDrain(drainMs int, def time.Duration) time.Duration {
	if drainMs <= 0 {
		return def
	}
	return time.Duration(drainMs) * time.Millisecond
}

// --- response shape ---

// serviceView is the observable shape of a Service as returned by the
// admin API. It inlines Service fields via embedding and adds the
// computed `phase` plus placeholder fields for future TLS / health
// wiring (spec §8.1).
type serviceView struct {
	service.Service
	Phase        service.Phase `json:"phase"`
	TLSStatus    string        `json:"tls_status,omitempty"`
	TLSError     string        `json:"tls_error,omitempty"`
	LastDeployAt *time.Time    `json:"last_deploy_at,omitempty"`
	Health       *healthView   `json:"health,omitempty"`
}

// healthView is the observable health snapshot for the active target.
// Background health tracking is deferred to post-MVP (spec §12); the
// fields are kept on the contract so callers can start reading them now.
type healthView struct {
	ActiveConsecutiveSuccess int        `json:"active_consecutive_success"`
	ActiveLastCheckedAt      *time.Time `json:"active_last_checked_at,omitempty"`
}

// viewOf hides an expired draining target (so GET never advertises a
// window the operator can no longer act on) and computes the externally
// observable phase.
func viewOf(svc service.Service, now time.Time) serviceView {
	masked := svc
	masked.DropExpiredDraining(now)
	view := serviceView{
		Service:   masked,
		Phase:     masked.Phase(now),
		TLSStatus: "unknown",
	}
	if masked.ActiveTarget != nil {
		t := masked.ActiveTarget.DeployedAt
		view.LastDeployAt = &t
	}
	return view
}

// VersionString is injected at build time via -ldflags.
// Unset during tests.
var version = "dev"

// VersionString returns the build version.
func VersionString() string { return version }

// compile-time interface check
var _ http.Handler = (*Handlers)(nil)
