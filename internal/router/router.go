package router

import (
	"net/http"
	"net/http/httputil"
	"sync/atomic"
)

// Router is an http.Handler backed by an atomically replaceable Snapshot.
type Router struct {
	snap atomic.Pointer[Snapshot]
}

// New returns a Router with an empty snapshot.
func New() *Router {
	r := &Router{}
	empty, _ := BuildSnapshot(nil)
	r.snap.Store(empty)
	return r
}

// Reload atomically replaces the current snapshot. After Reload returns,
// every subsequent request uses the new snapshot.
func (r *Router) Reload(snap *Snapshot) error {
	if snap == nil {
		return errNilSnapshot
	}
	r.snap.Store(snap)
	return nil
}

// ServeHTTP resolves the incoming request's Host header against the
// current snapshot and forwards to the active target. Unknown hosts are
// answered 421 and services without an active target are answered 503.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	entry, ok := r.snap.Load().Lookup(req.Host)
	if !ok {
		http.Error(w, "no service registered for host", http.StatusMisdirectedRequest)
		return
	}
	if entry.Active == nil {
		http.Error(w, "service not ready", http.StatusServiceUnavailable)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(entry.Active)
	// Preserve the host header for upstream — many apps depend on it.
	orig := proxy.Director
	proxy.Director = func(r *http.Request) {
		orig(r)
		r.Host = entry.Active.Host
	}
	proxy.ServeHTTP(w, req)
}

// errNilSnapshot is returned when Reload is called with nil.
var errNilSnapshot = errNilSnap{}

type errNilSnap struct{}

func (errNilSnap) Error() string { return "cannot reload with nil snapshot" }
