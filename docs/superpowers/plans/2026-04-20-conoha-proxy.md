# conoha-proxy MVP Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a Go reverse proxy daemon (`conoha-proxy`) with automatic Let's Encrypt HTTPS, multi-service domain routing, blue/green deploys, and an Admin HTTP API — shippable as a Docker image and Go binary.

**Architecture:** Single Go process. `net/http` listeners on :80 / :443 / admin socket. `certmagic` for ACME lifecycle. `bbolt` for state persistence. One-way component dependencies: `adminapi → service → store` plus `router`/`health`/`tls` siblings wired in `main.go`. TDD throughout; e2e harness uses [Pebble](https://github.com/letsencrypt/pebble).

**Tech Stack:** Go 1.22+, `caddyserver/certmagic`, `go.etcd.io/bbolt`, `spf13/cobra`, `log/slog` (stdlib), `stretchr/testify` (test). Docker multi-stage build. GitHub Actions CI with `golangci-lint`, `staticcheck`, race detector.

**Spec:** `docs/superpowers/specs/2026-04-20-conoha-proxy-design.md` (must be read before starting).

---

## File Structure

Files created by this plan, grouped by responsibility:

```
conoha-proxy/
├─ go.mod, go.sum, Makefile, .gitignore, .dockerignore, .golangci.yml
├─ LICENSE (Apache-2.0), NOTICES.md
├─ README.md, README-en.md, README-ko.md
├─ Dockerfile, .goreleaser.yaml
├─ cmd/conoha-proxy/main.go                   # Cobra root + `run` command, wires everything
├─ internal/
│  ├─ logging/
│  │  ├─ slog.go                              # JSON slog setup
│  │  └─ slog_test.go
│  ├─ service/
│  │  ├─ service.go                           # Service, Target, HealthPolicy types
│  │  ├─ service_test.go
│  │  ├─ phase.go                             # Phase enum + Snapshot()
│  │  └─ phase_test.go
│  ├─ store/
│  │  ├─ store.go                             # Store interface
│  │  ├─ bbolt.go                             # bbolt implementation
│  │  └─ bbolt_test.go
│  ├─ health/
│  │  ├─ health.go                            # Checker, ProbeOnce
│  │  ├─ health_test.go
│  │  ├─ watch.go                             # Watch (continuous)
│  │  └─ watch_test.go
│  ├─ router/
│  │  ├─ snapshot.go                          # Immutable routing snapshot
│  │  ├─ snapshot_test.go
│  │  ├─ router.go                            # http.Handler with atomic reload
│  │  └─ router_test.go
│  ├─ tls/
│  │  ├─ certmanager.go                       # certmagic wrapper
│  │  └─ certmanager_test.go
│  └─ adminapi/
│     ├─ server.go                            # HTTP server (Unix/TCP)
│     ├─ handlers.go                          # Handlers
│     ├─ handlers_test.go
│     └─ errors.go                            # Error response format
├─ test/e2e/
│  ├─ harness.go                              # Pebble + proxy + fake upstreams
│  └─ blue_green_test.go                      # 6 MVP scenarios
├─ docs/
│  ├─ architecture.md
│  ├─ ops-runbook.md
│  └─ admin-api.md
└─ .github/workflows/
   ├─ ci.yml
   └─ release.yml
```

---

## Task 1: Project Skeleton

**Files:**
- Create: `go.mod`, `Makefile`, `.gitignore`, `.dockerignore`, `.golangci.yml`, `LICENSE`, `NOTICES.md`, `README.md` (stub)

- [ ] **Step 1: Initialize Go module**

```bash
cd ~/dev/crowdy/conoha-proxy
go mod init github.com/crowdy/conoha-proxy
go mod tidy
```

Expected: creates `go.mod` with `go 1.22+` module line.

- [ ] **Step 2: Write `.gitignore`**

Create `.gitignore`:

```gitignore
# Binaries
/conoha-proxy
/dist/
/bin/

# Go
*.test
*.out
/coverage.txt

# OS
.DS_Store

# IDE
.idea/
.vscode/

# Local runtime
/var/
/tmp/
```

- [ ] **Step 3: Write `.dockerignore`**

Create `.dockerignore`:

```dockerignore
.git
.github
docs
test
*.md
Makefile
.golangci.yml
.goreleaser.yaml
dist/
coverage.txt
```

- [ ] **Step 4: Write Apache-2.0 LICENSE**

Create `LICENSE` — copy verbatim from https://www.apache.org/licenses/LICENSE-2.0.txt. Do not edit.

- [ ] **Step 5: Write `NOTICES.md` (stub — fill in Task 21)**

```markdown
# Third-Party Notices

conoha-proxy bundles or depends on the following third-party software.
Each retains its original license.

(Populated once dependencies are fixed — see Task 21.)
```

- [ ] **Step 6: Write `README.md` (stub)**

```markdown
# conoha-proxy

ConoHa VPS 向けの Go リバースプロキシデーモン。自動 HTTPS、マルチサービスルーティング、blue/green デプロイを提供。

(詳細は実装完了後に追記。設計は [docs/superpowers/specs/2026-04-20-conoha-proxy-design.md](docs/superpowers/specs/2026-04-20-conoha-proxy-design.md) 参照)

## License

Apache-2.0
```

- [ ] **Step 7: Write `.golangci.yml`**

```yaml
run:
  timeout: 5m
  go: "1.22"

linters:
  enable:
    - errcheck
    - gosimple
    - govet
    - ineffassign
    - staticcheck
    - unused
    - goconst
    - gofmt
    - goimports
    - misspell
    - revive
    - unconvert
    - unparam

issues:
  exclude-dirs:
    - test/e2e
```

- [ ] **Step 8: Write `Makefile`**

```makefile
.PHONY: build test lint docker clean

GO ?= go
BIN := conoha-proxy

build:
	$(GO) build -o bin/$(BIN) ./cmd/conoha-proxy

test:
	$(GO) test -race -coverprofile=coverage.txt ./...

lint:
	golangci-lint run
	staticcheck ./...

e2e:
	$(GO) test -tags=e2e -timeout=5m ./test/e2e/...

docker:
	docker build -t ghcr.io/crowdy/conoha-proxy:dev .

clean:
	rm -rf bin/ dist/ coverage.txt
```

- [ ] **Step 9: Verify**

Run:

```bash
go build ./...
```

Expected: no output (no code yet, module just initializes cleanly).

- [ ] **Step 10: Commit**

```bash
git add .
git commit -m "chore: project skeleton (go.mod, tooling, LICENSE)"
```

---

## Task 2: Logging Package

**Files:**
- Create: `internal/logging/slog.go`, `internal/logging/slog_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/logging/slog_test.go`:

```go
package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNew_EmitsJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, slog.LevelInfo)

	logger.Info("hello", "service", "myapp", "phase", "live")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))
	require.Equal(t, "hello", entry["msg"])
	require.Equal(t, "myapp", entry["service"])
	require.Equal(t, "live", entry["phase"])
	require.Contains(t, entry, "time")
	require.Contains(t, entry, "level")
}

func TestNew_RespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf, slog.LevelWarn)
	logger.Info("ignored")
	require.Empty(t, buf.String())
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/logging/...
```

Expected: FAIL — undefined `New`.

- [ ] **Step 3: Add testify dependency**

```bash
go get github.com/stretchr/testify/require
go mod tidy
```

- [ ] **Step 4: Implement `slog.go`**

Create `internal/logging/slog.go`:

```go
// Package logging provides a structured JSON logger shared across the proxy.
package logging

import (
	"io"
	"log/slog"
)

// New returns a slog.Logger that writes JSON lines to w at the given level.
func New(w io.Writer, level slog.Leveler) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(h)
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./internal/logging/... -v
```

Expected: PASS both tests.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/logging/
git commit -m "feat(logging): JSON slog helper"
```

---

## Task 3: Service Domain Types

**Files:**
- Create: `internal/service/service.go`, `internal/service/service_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/service/service_test.go`:

```go
package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestService_Validate_Ok(t *testing.T) {
	s := Service{
		Name:  "myapp",
		Hosts: []string{"a.example.com"},
		HealthPolicy: HealthPolicy{
			Path:               "/up",
			IntervalMs:         5000,
			TimeoutMs:          2000,
			HealthyThreshold:   1,
			UnhealthyThreshold: 3,
		},
	}
	require.NoError(t, s.Validate())
}

func TestService_Validate_RejectsEmptyName(t *testing.T) {
	s := Service{Hosts: []string{"a.example.com"}}
	require.ErrorContains(t, s.Validate(), "name")
}

func TestService_Validate_RejectsNoHosts(t *testing.T) {
	s := Service{Name: "myapp"}
	require.ErrorContains(t, s.Validate(), "hosts")
}

func TestService_Validate_RejectsInvalidHost(t *testing.T) {
	s := Service{Name: "myapp", Hosts: []string{""}}
	require.ErrorContains(t, s.Validate(), "host")
}

func TestService_Validate_RejectsDuplicateHosts(t *testing.T) {
	s := Service{
		Name:  "myapp",
		Hosts: []string{"a.com", "a.com"},
	}
	require.ErrorContains(t, s.Validate(), "duplicate")
}

func TestTarget_Validate_RequiresHTTPURL(t *testing.T) {
	tg := Target{URL: "ftp://bad"}
	require.ErrorContains(t, tg.Validate(), "http")
}

func TestHealthPolicy_WithDefaults(t *testing.T) {
	p := HealthPolicy{}.WithDefaults()
	require.Equal(t, "/up", p.Path)
	require.Equal(t, 5000, p.IntervalMs)
	require.Equal(t, 2000, p.TimeoutMs)
	require.Equal(t, 1, p.HealthyThreshold)
	require.Equal(t, 3, p.UnhealthyThreshold)
}

func TestHealthPolicy_WithDefaults_PreservesExplicit(t *testing.T) {
	p := HealthPolicy{Path: "/ping", TimeoutMs: 999}.WithDefaults()
	require.Equal(t, "/ping", p.Path)
	require.Equal(t, 999, p.TimeoutMs)
	require.Equal(t, 5000, p.IntervalMs) // still defaulted
}

func TestService_MatchesHost_CaseInsensitive(t *testing.T) {
	s := Service{Hosts: []string{"A.Example.Com"}}
	require.True(t, s.MatchesHost("a.example.com"))
	require.True(t, s.MatchesHost("A.EXAMPLE.COM"))
	require.False(t, s.MatchesHost("b.example.com"))
}

func TestService_MatchesHost_StripsPort(t *testing.T) {
	s := Service{Hosts: []string{"a.example.com"}}
	require.True(t, s.MatchesHost("a.example.com:8443"))
}

func TestService_Touch(t *testing.T) {
	s := Service{}
	before := time.Now()
	s.Touch()
	require.True(t, !s.UpdatedAt.Before(before))
}
```

- [ ] **Step 2: Run test — verify it fails**

```bash
go test ./internal/service/...
```

Expected: FAIL (undefined symbols).

- [ ] **Step 3: Implement `service.go`**

Create `internal/service/service.go`:

```go
// Package service defines the domain types for conoha-proxy services,
// targets, and health policies, plus validation and small query helpers.
package service

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Service is a named routing unit that matches one or more hosts and proxies
// to a blue/green pair of upstream Targets.
type Service struct {
	Name           string        `json:"name"`
	Hosts          []string      `json:"hosts"`
	ActiveTarget   *Target       `json:"active_target,omitempty"`
	DrainingTarget *Target       `json:"draining_target,omitempty"`
	DrainDeadline  *time.Time    `json:"drain_deadline,omitempty"`
	HealthPolicy   HealthPolicy  `json:"health_policy"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

// Target is a single upstream URL.
type Target struct {
	URL        string    `json:"url"`
	DeployedAt time.Time `json:"deployed_at"`
}

// HealthPolicy controls how we probe a Target.
type HealthPolicy struct {
	Path               string `json:"path"`
	IntervalMs         int    `json:"interval_ms"`
	TimeoutMs          int    `json:"timeout_ms"`
	HealthyThreshold   int    `json:"healthy_threshold"`
	UnhealthyThreshold int    `json:"unhealthy_threshold"`
}

// WithDefaults returns a copy of p with zero fields replaced by defaults.
func (p HealthPolicy) WithDefaults() HealthPolicy {
	if p.Path == "" {
		p.Path = "/up"
	}
	if p.IntervalMs == 0 {
		p.IntervalMs = 5000
	}
	if p.TimeoutMs == 0 {
		p.TimeoutMs = 2000
	}
	if p.HealthyThreshold == 0 {
		p.HealthyThreshold = 1
	}
	if p.UnhealthyThreshold == 0 {
		p.UnhealthyThreshold = 3
	}
	return p
}

// Validate returns nil when s is a usable Service.
func (s *Service) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("service name must be non-empty")
	}
	if len(s.Hosts) == 0 {
		return errors.New("service must have at least one host")
	}
	seen := make(map[string]struct{}, len(s.Hosts))
	for _, h := range s.Hosts {
		h = strings.TrimSpace(strings.ToLower(h))
		if h == "" {
			return errors.New("host entries must be non-empty")
		}
		if _, ok := seen[h]; ok {
			return fmt.Errorf("duplicate host: %q", h)
		}
		seen[h] = struct{}{}
	}
	return nil
}

// Validate checks that the target URL is an http:// or https:// URL.
func (t *Target) Validate() error {
	u, err := url.Parse(t.URL)
	if err != nil {
		return fmt.Errorf("invalid target url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("target url scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("target url must include host")
	}
	return nil
}

// MatchesHost reports whether h (possibly with :port) matches any of s.Hosts
// after case folding and port stripping.
func (s *Service) MatchesHost(h string) bool {
	h = strings.ToLower(h)
	if i := strings.Index(h, ":"); i >= 0 {
		h = h[:i]
	}
	for _, x := range s.Hosts {
		if strings.EqualFold(strings.TrimSpace(x), h) {
			return true
		}
	}
	return false
}

// Touch stamps UpdatedAt to now.
func (s *Service) Touch() {
	s.UpdatedAt = time.Now().UTC()
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
go test ./internal/service/... -v
```

Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/
git commit -m "feat(service): domain types for Service/Target/HealthPolicy"
```

---

## Task 4: Service Phase & Snapshot

**Files:**
- Create: `internal/service/phase.go`, `internal/service/phase_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/service/phase_test.go`:

```go
package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPhase_Configured_WhenNoActive(t *testing.T) {
	s := &Service{}
	require.Equal(t, PhaseConfigured, s.Phase(time.Now()))
}

func TestPhase_Live_WhenActiveOnly(t *testing.T) {
	s := &Service{ActiveTarget: &Target{URL: "http://x"}}
	require.Equal(t, PhaseLive, s.Phase(time.Now()))
}

func TestPhase_Swapping_WhenDraining(t *testing.T) {
	deadline := time.Now().Add(10 * time.Second)
	s := &Service{
		ActiveTarget:   &Target{URL: "http://new"},
		DrainingTarget: &Target{URL: "http://old"},
		DrainDeadline:  &deadline,
	}
	require.Equal(t, PhaseSwapping, s.Phase(time.Now()))
}

func TestPhase_Live_AfterDrainDeadline(t *testing.T) {
	deadline := time.Now().Add(-1 * time.Second)
	s := &Service{
		ActiveTarget:   &Target{URL: "http://new"},
		DrainingTarget: &Target{URL: "http://old"},
		DrainDeadline:  &deadline,
	}
	require.Equal(t, PhaseLive, s.Phase(time.Now()))
}

func TestDropExpiredDraining_DropsPastDeadline(t *testing.T) {
	past := time.Now().Add(-1 * time.Second)
	s := &Service{
		ActiveTarget:   &Target{URL: "http://new"},
		DrainingTarget: &Target{URL: "http://old"},
		DrainDeadline:  &past,
	}
	s.DropExpiredDraining(time.Now())
	require.Nil(t, s.DrainingTarget)
	require.Nil(t, s.DrainDeadline)
}

func TestDropExpiredDraining_KeepsFutureDeadline(t *testing.T) {
	fut := time.Now().Add(1 * time.Minute)
	s := &Service{
		DrainingTarget: &Target{URL: "http://old"},
		DrainDeadline:  &fut,
	}
	s.DropExpiredDraining(time.Now())
	require.NotNil(t, s.DrainingTarget)
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/service/...
```

Expected: undefined `PhaseConfigured`/`PhaseLive`/`PhaseSwapping`/`Phase`/`DropExpiredDraining`.

- [ ] **Step 3: Implement `phase.go`**

Create `internal/service/phase.go`:

```go
package service

import "time"

// Phase is the externally observable lifecycle phase of a Service.
// Internal probing variants collapse to PhaseLive (or PhaseConfigured
// when no active target exists yet) — the transient probe happens
// inside a single API call and is never observed by the routing layer.
type Phase string

const (
	PhaseConfigured Phase = "configured"
	PhaseLive       Phase = "live"
	PhaseSwapping   Phase = "swapping"
)

// Phase returns the current phase given a clock reading.
func (s *Service) Phase(now time.Time) Phase {
	if s.DrainingTarget != nil && s.DrainDeadline != nil && now.Before(*s.DrainDeadline) {
		return PhaseSwapping
	}
	if s.ActiveTarget != nil {
		return PhaseLive
	}
	return PhaseConfigured
}

// DropExpiredDraining clears the draining pointer and deadline if the
// deadline has passed. Called periodically by the router reload loop.
func (s *Service) DropExpiredDraining(now time.Time) {
	if s.DrainDeadline != nil && !now.Before(*s.DrainDeadline) {
		s.DrainingTarget = nil
		s.DrainDeadline = nil
	}
}
```

- [ ] **Step 4: Verify tests pass**

```bash
go test ./internal/service/... -v
```

Expected: all pass (old + new).

- [ ] **Step 5: Commit**

```bash
git add internal/service/
git commit -m "feat(service): phase calculation and drain-expiry helper"
```

---

## Task 5: Store Interface + bbolt Implementation

**Files:**
- Create: `internal/store/store.go`, `internal/store/bbolt.go`, `internal/store/bbolt_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/store/bbolt_test.go`:

```go
package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/crowdy/conoha-proxy/internal/service"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) *BoltStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.db")
	s, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestBoltStore_SaveAndLoad(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	svc := service.Service{
		Name:      "myapp",
		Hosts:     []string{"a.com"},
		CreatedAt: time.Now().UTC().Round(time.Millisecond),
		UpdatedAt: time.Now().UTC().Round(time.Millisecond),
		HealthPolicy: service.HealthPolicy{Path: "/up"},
	}
	require.NoError(t, st.SaveService(ctx, svc))

	got, err := st.LoadAll(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "myapp", got[0].Name)
	require.Equal(t, []string{"a.com"}, got[0].Hosts)
}

func TestBoltStore_DeleteService(t *testing.T) {
	ctx := context.Background()
	st := newTestStore(t)

	require.NoError(t, st.SaveService(ctx, service.Service{Name: "a", Hosts: []string{"a.com"}}))
	require.NoError(t, st.SaveService(ctx, service.Service{Name: "b", Hosts: []string{"b.com"}}))
	require.NoError(t, st.DeleteService(ctx, "a"))

	got, err := st.LoadAll(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "b", got[0].Name)
}

func TestBoltStore_SchemaVersion(t *testing.T) {
	st := newTestStore(t)
	ver, err := st.SchemaVersion()
	require.NoError(t, err)
	require.Equal(t, "1", ver)
}

func TestBoltStore_Persistence(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")

	st1, err := Open(path)
	require.NoError(t, err)
	require.NoError(t, st1.SaveService(ctx, service.Service{Name: "x", Hosts: []string{"x.com"}}))
	require.NoError(t, st1.Close())

	st2, err := Open(path)
	require.NoError(t, err)
	defer st2.Close()

	got, err := st2.LoadAll(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "x", got[0].Name)
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/store/...
```

Expected: undefined `Open`, `BoltStore`.

- [ ] **Step 3: Add bbolt dependency**

```bash
go get go.etcd.io/bbolt
go mod tidy
```

- [ ] **Step 4: Implement `store.go`**

Create `internal/store/store.go`:

```go
// Package store provides persistent storage for Service configurations.
package store

import (
	"context"

	"github.com/crowdy/conoha-proxy/internal/service"
)

// Store is the persistence interface used by adminapi.
type Store interface {
	LoadAll(ctx context.Context) ([]service.Service, error)
	SaveService(ctx context.Context, svc service.Service) error
	DeleteService(ctx context.Context, name string) error
	Close() error
}
```

- [ ] **Step 5: Implement `bbolt.go`**

Create `internal/store/bbolt.go`:

```go
package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/crowdy/conoha-proxy/internal/service"
	bolt "go.etcd.io/bbolt"
)

const (
	bucketServices = "services"
	bucketMeta     = "meta"
	keySchemaVer   = "schema_version"
	currentSchema  = "1"
)

// BoltStore is a bbolt-backed Store.
type BoltStore struct {
	db *bolt.DB
}

// Open opens (or creates) a bbolt database at path.
func Open(path string) (*BoltStore, error) {
	db, err := bolt.Open(path, 0o600, nil)
	if err != nil {
		return nil, fmt.Errorf("open bbolt: %w", err)
	}
	s := &BoltStore{db: db}
	if err := s.init(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *BoltStore) init() error {
	return s.db.Update(func(tx *bolt.Tx) error {
		for _, name := range []string{bucketServices, bucketMeta} {
			if _, err := tx.CreateBucketIfNotExists([]byte(name)); err != nil {
				return fmt.Errorf("create bucket %s: %w", name, err)
			}
		}
		meta := tx.Bucket([]byte(bucketMeta))
		if v := meta.Get([]byte(keySchemaVer)); v == nil {
			if err := meta.Put([]byte(keySchemaVer), []byte(currentSchema)); err != nil {
				return fmt.Errorf("set schema version: %w", err)
			}
		}
		return nil
	})
}

// SchemaVersion returns the stored schema version.
func (s *BoltStore) SchemaVersion() (string, error) {
	var v string
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketMeta)).Get([]byte(keySchemaVer))
		if b == nil {
			return errors.New("schema version missing")
		}
		v = string(b)
		return nil
	})
	return v, err
}

// LoadAll returns every stored Service.
func (s *BoltStore) LoadAll(ctx context.Context) ([]service.Service, error) {
	var out []service.Service
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucketServices))
		return b.ForEach(func(_ []byte, v []byte) error {
			var svc service.Service
			if err := json.Unmarshal(v, &svc); err != nil {
				return fmt.Errorf("decode service: %w", err)
			}
			out = append(out, svc)
			return nil
		})
	})
	return out, err
}

// SaveService upserts svc into the store.
func (s *BoltStore) SaveService(ctx context.Context, svc service.Service) error {
	data, err := json.Marshal(svc)
	if err != nil {
		return fmt.Errorf("encode service: %w", err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketServices)).Put([]byte(svc.Name), data)
	})
}

// DeleteService removes the named service if present. Missing names are not an error.
func (s *BoltStore) DeleteService(ctx context.Context, name string) error {
	return s.db.Update(func(tx *bolt.Tx) error {
		return tx.Bucket([]byte(bucketServices)).Delete([]byte(name))
	})
}

// Close closes the underlying database.
func (s *BoltStore) Close() error {
	return s.db.Close()
}

// Compile-time assertion that BoltStore satisfies Store.
var _ Store = (*BoltStore)(nil)
```

- [ ] **Step 6: Verify tests pass**

```bash
go test ./internal/store/... -v
```

Expected: 4 tests PASS.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/store/
git commit -m "feat(store): bbolt-backed Service persistence"
```

---

## Task 6: Health — ProbeOnce

**Files:**
- Create: `internal/health/health.go`, `internal/health/health_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/health/health_test.go`:

```go
package health

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/crowdy/conoha-proxy/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProbeOnce_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/up" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c := NewHTTPChecker()
	ctx := context.Background()
	err := c.ProbeOnce(ctx, service.Target{URL: srv.URL},
		service.HealthPolicy{Path: "/up", TimeoutMs: 1000, HealthyThreshold: 1, UnhealthyThreshold: 3})
	require.NoError(t, err)
}

func TestProbeOnce_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewHTTPChecker()
	err := c.ProbeOnce(context.Background(), service.Target{URL: srv.URL},
		service.HealthPolicy{Path: "/up", TimeoutMs: 1000, UnhealthyThreshold: 2, HealthyThreshold: 1})
	require.Error(t, err)
}

func TestProbeOnce_RetriesUntilHealthy(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewHTTPChecker()
	err := c.ProbeOnce(context.Background(), service.Target{URL: srv.URL},
		service.HealthPolicy{Path: "/up", TimeoutMs: 500, HealthyThreshold: 1, UnhealthyThreshold: 5, IntervalMs: 50})
	require.NoError(t, err)
	require.Equal(t, 3, attempts)
}

func TestProbeOnce_ContextCanceled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewHTTPChecker()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := c.ProbeOnce(ctx, service.Target{URL: srv.URL},
		service.HealthPolicy{Path: "/up", TimeoutMs: 1000, HealthyThreshold: 1, UnhealthyThreshold: 3, IntervalMs: 10})
	require.Error(t, err)
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/health/...
```

Expected: undefined `NewHTTPChecker`/`ProbeOnce`.

- [ ] **Step 3: Implement `health.go`**

Create `internal/health/health.go`:

```go
// Package health implements target health probing.
package health

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/crowdy/conoha-proxy/internal/service"
)

// Checker probes targets against a HealthPolicy.
type Checker interface {
	ProbeOnce(ctx context.Context, t service.Target, p service.HealthPolicy) error
}

// HTTPChecker uses a plain http.Client.
type HTTPChecker struct {
	client *http.Client
}

// NewHTTPChecker returns a Checker with a default transport.
func NewHTTPChecker() *HTTPChecker {
	return &HTTPChecker{client: &http.Client{}}
}

// ProbeOnce polls the target until HealthyThreshold consecutive 2xx responses
// are seen or UnhealthyThreshold consecutive failures, whichever comes first.
// The per-request timeout is TimeoutMs; between attempts IntervalMs is slept.
func (h *HTTPChecker) ProbeOnce(ctx context.Context, t service.Target, p service.HealthPolicy) error {
	p = p.WithDefaults()
	if err := t.Validate(); err != nil {
		return err
	}
	url := strings.TrimRight(t.URL, "/") + p.Path

	var consecutiveOK, consecutiveFail int
	for {
		reqCtx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutMs)*time.Millisecond)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
		if err != nil {
			cancel()
			return fmt.Errorf("build probe request: %w", err)
		}
		resp, err := h.client.Do(req)
		cancel()

		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				consecutiveOK++
				consecutiveFail = 0
				if consecutiveOK >= p.HealthyThreshold {
					return nil
				}
			} else {
				consecutiveOK = 0
				consecutiveFail++
			}
		} else {
			consecutiveOK = 0
			consecutiveFail++
		}

		if consecutiveFail >= p.UnhealthyThreshold {
			return fmt.Errorf("probe failed: %d consecutive failures", consecutiveFail)
		}

		select {
		case <-ctx.Done():
			return errors.Join(ctx.Err(), errors.New("probe canceled"))
		case <-time.After(time.Duration(p.IntervalMs) * time.Millisecond):
		}
	}
}
```

- [ ] **Step 4: Verify tests pass**

```bash
go test ./internal/health/... -v
```

Expected: all 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/health/
git commit -m "feat(health): HTTP ProbeOnce with threshold-based success/failure"
```

---

## Task 7: Health — Continuous Watch

**Files:**
- Create: `internal/health/watch.go`, `internal/health/watch_test.go`

- [ ] **Step 1: Write failing test**

Create `internal/health/watch_test.go`:

```go
package health

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crowdy/conoha-proxy/internal/service"
	"github.com/stretchr/testify/require"
)

func TestWatch_EmitsEventsOnTransition(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if healthy.Load() {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewHTTPChecker()
	events, stop := c.Watch(service.Target{URL: srv.URL},
		service.HealthPolicy{Path: "/up", IntervalMs: 30, TimeoutMs: 200, HealthyThreshold: 2, UnhealthyThreshold: 2})
	t.Cleanup(stop)

	// First: expect a Healthy event after 2 consecutive 200s.
	select {
	case ev := <-events:
		require.True(t, ev.Healthy)
	case <-time.After(2 * time.Second):
		t.Fatal("expected healthy event")
	}

	// Flip to unhealthy.
	healthy.Store(false)

	// Expect an Unhealthy event.
	deadline := time.After(3 * time.Second)
	for {
		select {
		case ev := <-events:
			if !ev.Healthy {
				return // success
			}
		case <-deadline:
			t.Fatal("expected unhealthy event")
		}
	}
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/health/... -run TestWatch
```

Expected: undefined `Watch`, `HealthEvent`.

- [ ] **Step 3: Implement `watch.go`**

Create `internal/health/watch.go`:

```go
package health

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/crowdy/conoha-proxy/internal/service"
)

// HealthEvent is emitted when a target transitions between Healthy states.
type HealthEvent struct {
	Healthy bool
	At      time.Time
	Err     error
}

// Watch probes t continuously according to p and emits an event on each
// transition. The returned stop function cancels the watcher and closes
// the channel.
func (h *HTTPChecker) Watch(t service.Target, p service.HealthPolicy) (<-chan HealthEvent, func()) {
	p = p.WithDefaults()
	ctx, cancel := context.WithCancel(context.Background())
	ch := make(chan HealthEvent, 8)

	go h.watchLoop(ctx, ch, t, p)

	return ch, func() {
		cancel()
	}
}

func (h *HTTPChecker) watchLoop(ctx context.Context, ch chan<- HealthEvent, t service.Target, p service.HealthPolicy) {
	defer close(ch)

	url := strings.TrimRight(t.URL, "/") + p.Path
	var ok, fail int
	// Start in an indeterminate state — emit the first transition only.
	state := -1 // -1 unknown, 0 unhealthy, 1 healthy

	ticker := time.NewTicker(time.Duration(p.IntervalMs) * time.Millisecond)
	defer ticker.Stop()

	probe := func() {
		reqCtx, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutMs)*time.Millisecond)
		defer cancel()
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
		if err != nil {
			ok, fail = 0, fail+1
			return
		}
		resp, err := h.client.Do(req)
		if err != nil {
			ok, fail = 0, fail+1
			return
		}
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			ok, fail = ok+1, 0
		} else {
			ok, fail = 0, fail+1
		}
	}

	emit := func(healthy bool, err error) {
		if (state == 1) == healthy {
			return // no transition
		}
		state = 0
		if healthy {
			state = 1
		}
		select {
		case ch <- HealthEvent{Healthy: healthy, At: time.Now(), Err: err}:
		default:
			// drop on full channel
		}
	}

	for {
		probe()
		if ok >= p.HealthyThreshold {
			emit(true, nil)
		} else if fail >= p.UnhealthyThreshold {
			emit(false, nil)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
```

- [ ] **Step 4: Verify test passes**

```bash
go test ./internal/health/... -v
```

Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add internal/health/
git commit -m "feat(health): continuous Watch with transition events"
```

---

## Task 8: Router — Snapshot

**Files:**
- Create: `internal/router/snapshot.go`, `internal/router/snapshot_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/router/snapshot_test.go`:

```go
package router

import (
	"net/url"
	"testing"
	"time"

	"github.com/crowdy/conoha-proxy/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSnapshot_BuildFromServices(t *testing.T) {
	fut := time.Now().Add(time.Minute)
	svcs := []service.Service{
		{
			Name:         "app1",
			Hosts:        []string{"a.com", "www.a.com"},
			ActiveTarget: &service.Target{URL: "http://127.0.0.1:9001"},
		},
		{
			Name:           "app2",
			Hosts:          []string{"b.com"},
			ActiveTarget:   &service.Target{URL: "http://127.0.0.1:9002"},
			DrainingTarget: &service.Target{URL: "http://127.0.0.1:9003"},
			DrainDeadline:  &fut,
		},
	}
	snap, err := BuildSnapshot(svcs)
	require.NoError(t, err)

	entry, ok := snap.Lookup("a.com")
	require.True(t, ok)
	require.NotNil(t, entry.Active)
	require.Equal(t, "127.0.0.1:9001", entry.Active.Host)

	entry, ok = snap.Lookup("WWW.A.COM")
	require.True(t, ok)

	entry, ok = snap.Lookup("b.com")
	require.True(t, ok)
	require.NotNil(t, entry.Active)
	require.NotNil(t, entry.Draining)
}

func TestSnapshot_Lookup_UnknownHost(t *testing.T) {
	snap, err := BuildSnapshot(nil)
	require.NoError(t, err)
	_, ok := snap.Lookup("unknown.com")
	require.False(t, ok)
}

func TestSnapshot_StripsPort(t *testing.T) {
	snap, err := BuildSnapshot([]service.Service{{
		Name: "a", Hosts: []string{"a.com"},
		ActiveTarget: &service.Target{URL: "http://127.0.0.1:9001"},
	}})
	require.NoError(t, err)
	_, ok := snap.Lookup("a.com:443")
	require.True(t, ok)
}

func TestSnapshot_InvalidTargetURL(t *testing.T) {
	_, err := BuildSnapshot([]service.Service{{
		Name: "a", Hosts: []string{"a.com"},
		ActiveTarget: &service.Target{URL: "://bad"},
	}})
	require.Error(t, err)
}

// Ensure we expose *url.URL for use by ReverseProxy.
func TestSnapshot_ReturnsParsedURLs(t *testing.T) {
	snap, err := BuildSnapshot([]service.Service{{
		Name: "a", Hosts: []string{"a.com"},
		ActiveTarget: &service.Target{URL: "http://127.0.0.1:9001/"},
	}})
	require.NoError(t, err)
	e, _ := snap.Lookup("a.com")
	require.IsType(t, &url.URL{}, e.Active)
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/router/...
```

Expected: undefined `BuildSnapshot`/`Snapshot`.

- [ ] **Step 3: Implement `snapshot.go`**

Create `internal/router/snapshot.go`:

```go
// Package router maps incoming requests to upstream URLs and performs
// the reverse proxy hop. Routing state is immutable after creation —
// reloads happen via atomic pointer swap (see router.go).
package router

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/crowdy/conoha-proxy/internal/service"
)

// SnapshotEntry is one resolved routing record for a host.
type SnapshotEntry struct {
	ServiceName string
	Active      *url.URL
	Draining    *url.URL
}

// Snapshot is an immutable host→entry lookup table.
type Snapshot struct {
	byHost map[string]SnapshotEntry
}

// BuildSnapshot converts a list of Services into an immutable snapshot.
// It returns an error on malformed target URLs so we never silently
// publish a broken routing table.
func BuildSnapshot(svcs []service.Service) (*Snapshot, error) {
	byHost := make(map[string]SnapshotEntry, len(svcs))
	for _, s := range svcs {
		var active, draining *url.URL
		if s.ActiveTarget != nil {
			u, err := parseTargetURL(s.ActiveTarget.URL)
			if err != nil {
				return nil, fmt.Errorf("service %q: %w", s.Name, err)
			}
			active = u
		}
		if s.DrainingTarget != nil {
			u, err := parseTargetURL(s.DrainingTarget.URL)
			if err != nil {
				return nil, fmt.Errorf("service %q draining: %w", s.Name, err)
			}
			draining = u
		}
		entry := SnapshotEntry{ServiceName: s.Name, Active: active, Draining: draining}
		for _, h := range s.Hosts {
			byHost[normalizeHost(h)] = entry
		}
	}
	return &Snapshot{byHost: byHost}, nil
}

// Lookup returns the routing entry for host h (case and port insensitive).
func (s *Snapshot) Lookup(h string) (SnapshotEntry, bool) {
	e, ok := s.byHost[normalizeHost(h)]
	return e, ok
}

func normalizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	if i := strings.Index(h, ":"); i >= 0 {
		h = h[:i]
	}
	return h
}

func parseTargetURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid target url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("target scheme must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("target url must include host")
	}
	return u, nil
}
```

- [ ] **Step 4: Verify tests pass**

```bash
go test ./internal/router/... -v
```

Expected: all 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/router/
git commit -m "feat(router): immutable routing snapshot"
```

---

## Task 9: Router — HTTP Handler with Atomic Reload

**Files:**
- Create: `internal/router/router.go`, `internal/router/router_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/router/router_test.go`:

```go
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
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/router/... -run Router
```

Expected: undefined `New`, `Reload`, `ServeHTTP`.

- [ ] **Step 3: Implement `router.go`**

Create `internal/router/router.go`:

```go
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
```

- [ ] **Step 4: Verify tests pass**

```bash
go test ./internal/router/... -v -race
```

Expected: all PASS, race detector clean.

- [ ] **Step 5: Commit**

```bash
git add internal/router/
git commit -m "feat(router): atomic snapshot reload + reverse proxy"
```

---

## Task 10: TLS — certmagic Wrapper

**Files:**
- Create: `internal/tls/certmanager.go`, `internal/tls/certmanager_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/tls/certmanager_test.go`:

```go
package tls

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCertManager_NewManagesDomains(t *testing.T) {
	cm, err := New(Config{
		StorageDir: t.TempDir(),
		Email:      "admin@example.com",
		CADirURL:   "https://localhost:14000/dir", // Pebble-compatible; we won't actually call it
		Staging:    true,
		AllowedDomains: nil,
	})
	require.NoError(t, err)
	require.NotNil(t, cm)

	// ManageDomains must be idempotent and accept repeated calls.
	require.NoError(t, cm.ManageDomains([]string{"a.example.com"}))
	require.NoError(t, cm.ManageDomains([]string{"a.example.com", "b.example.com"}))
	require.NoError(t, cm.ManageDomains(nil))
}

func TestCertManager_TLSConfig(t *testing.T) {
	cm, err := New(Config{StorageDir: t.TempDir(), Email: "admin@example.com"})
	require.NoError(t, err)
	cfg := cm.TLSConfig()
	require.NotNil(t, cfg)
	require.NotNil(t, cfg.GetCertificate)
}
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/tls/...
```

Expected: undefined `New`, `Config`.

- [ ] **Step 3: Add certmagic dependency**

```bash
go get github.com/caddyserver/certmagic
go mod tidy
```

- [ ] **Step 4: Implement `certmanager.go`**

Create `internal/tls/certmanager.go`:

```go
// Package tls wraps caddyserver/certmagic to provide automatic HTTPS
// with HTTP-01 challenge for a mutable set of domains.
package tls

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"sync"

	"github.com/caddyserver/certmagic"
)

// Config configures the certificate manager.
type Config struct {
	// StorageDir is the directory certmagic uses for cert/key/account files.
	StorageDir string
	// Email is the ACME account email.
	Email string
	// CADirURL overrides the ACME directory URL. Leave empty for Let's Encrypt prod.
	CADirURL string
	// Staging uses the Let's Encrypt staging endpoint when CADirURL is empty.
	Staging bool
	// AllowedDomains, when non-empty, restricts which hosts may obtain
	// certificates. Empty means allow any.
	AllowedDomains []string
}

// CertManager wraps certmagic.Config.
type CertManager struct {
	mu      sync.Mutex
	magic   *certmagic.Config
	issuer  *certmagic.ACMEIssuer
	domains map[string]struct{}
}

// New builds a CertManager with HTTP-01 challenge handling.
func New(c Config) (*CertManager, error) {
	if c.StorageDir == "" {
		return nil, fmt.Errorf("StorageDir is required")
	}
	storage := &certmagic.FileStorage{Path: c.StorageDir}

	cache := certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) {
			return certmagic.NewDefault(), nil
		},
	})
	mCfg := certmagic.New(cache, certmagic.Config{Storage: storage})

	ca := c.CADirURL
	if ca == "" {
		ca = certmagic.LetsEncryptProductionCA
		if c.Staging {
			ca = certmagic.LetsEncryptStagingCA
		}
	}
	issuer := certmagic.NewACMEIssuer(mCfg, certmagic.ACMEIssuer{
		CA:                      ca,
		Email:                   c.Email,
		Agreed:                  true,
		DisableTLSALPNChallenge: true,
	})
	mCfg.Issuers = []certmagic.Issuer{issuer}

	return &CertManager{
		magic:   mCfg,
		issuer:  issuer,
		domains: make(map[string]struct{}),
	}, nil
}

// ManageDomains ensures certmagic is managing (issuing/renewing) exactly
// the given set of domains. Removed domains stop being renewed but files
// are not deleted (they expire naturally — safer than aggressive deletion).
func (c *CertManager) ManageDomains(domains []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	next := make(map[string]struct{}, len(domains))
	for _, d := range domains {
		next[d] = struct{}{}
	}

	var toAdd []string
	for d := range next {
		if _, ok := c.domains[d]; !ok {
			toAdd = append(toAdd, d)
		}
	}
	if len(toAdd) > 0 {
		if err := c.magic.ManageAsync(context.Background(), toAdd); err != nil {
			return fmt.Errorf("certmagic manage: %w", err)
		}
	}
	c.domains = next
	return nil
}

// TLSConfig returns a *tls.Config suitable for http.Server.
func (c *CertManager) TLSConfig() *tls.Config {
	return c.magic.TLSConfig()
}

// HTTPChallengeHandler returns an http.Handler that answers ACME HTTP-01
// challenges and falls through to fallback for all other requests.
// Install this on the :80 listener.
func (c *CertManager) HTTPChallengeHandler(fallback http.Handler) http.Handler {
	return c.issuer.HTTPChallengeHandler(fallback)
}
```

- [ ] **Step 5: Verify tests pass**

```bash
go test ./internal/tls/... -v
```

Expected: both tests PASS (no network calls made).

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/tls/
git commit -m "feat(tls): certmagic wrapper with dynamic domain management"
```

---

## Task 11: Admin API — Error Responses

**Files:**
- Create: `internal/adminapi/errors.go`

- [ ] **Step 1: Implement `errors.go`**

Create `internal/adminapi/errors.go`:

```go
// Package adminapi implements the Admin HTTP API.
package adminapi

import (
	"encoding/json"
	"net/http"
)

// APIError is the wire format for 4xx/5xx responses.
type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error APIError `json:"error"`
}

// writeError emits `{"error":{"code":...,"message":...}}` with status.
func writeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorResponse{Error: APIError{Code: code, Message: msg}})
}

// writeJSON emits v as JSON with status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 2: Commit**

```bash
git add internal/adminapi/
git commit -m "feat(adminapi): shared error/json response helpers"
```

---

## Task 12: Admin API — Handlers (Services CRUD + Deploy + Rollback)

**Files:**
- Create: `internal/adminapi/handlers.go`, `internal/adminapi/handlers_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/adminapi/handlers_test.go`:

```go
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
```

- [ ] **Step 2: Verify failure**

```bash
go test ./internal/adminapi/...
```

Expected: undefined `NewHandlers`, `Handlers`.

- [ ] **Step 3: Implement `handlers.go`**

Create `internal/adminapi/handlers.go`:

```go
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
	Name         string                `json:"name"`
	Hosts        []string              `json:"hosts"`
	HealthPolicy service.HealthPolicy  `json:"health_policy"`
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
```

- [ ] **Step 4: Verify tests pass**

```bash
go test ./internal/adminapi/... -v
```

Expected: all 11 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adminapi/
git commit -m "feat(adminapi): services CRUD + deploy/rollback handlers"
```

---

## Task 13: Admin API — Listener (Unix socket + TCP localhost)

**Files:**
- Create: `internal/adminapi/server.go`

- [ ] **Step 1: Implement `server.go`**

Create `internal/adminapi/server.go`:

```go
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

	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
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
```

- [ ] **Step 2: Verify build**

```bash
go build ./internal/adminapi/...
```

Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/adminapi/
git commit -m "feat(adminapi): listener on Unix socket or loopback TCP"
```

---

## Task 14: Main — cobra root + `run` command

**Files:**
- Create: `cmd/conoha-proxy/main.go`

- [ ] **Step 1: Add cobra dependency**

```bash
go get github.com/spf13/cobra
go mod tidy
```

- [ ] **Step 2: Implement `main.go`**

Create `cmd/conoha-proxy/main.go`:

```go
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
	dataDir    string
	httpAddr   string
	httpsAddr  string
	adminSock  string
	adminTCP   string
	acmeEmail  string
	acmeCA     string
	acmeStage  bool
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
		Addr:    f.httpAddr,
		Handler: cm.HTTPChallengeHandler(redirectToHTTPS()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	// HTTPS (reverse proxy).
	httpsSrv := &http.Server{
		Addr:      f.httpsAddr,
		Handler:   rt,
		TLSConfig: cm.TLSConfig(),
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
```

- [ ] **Step 3: Verify build**

```bash
go build ./cmd/conoha-proxy
```

Expected: produces `./conoha-proxy` binary.

- [ ] **Step 4: Manual smoke test**

```bash
./conoha-proxy version
./conoha-proxy run --help
```

Expected: version output, flag help text.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum cmd/
git commit -m "feat(cmd): main binary with run/version subcommands"
```

---

## Task 15: Dockerfile

**Files:**
- Create: `Dockerfile`

- [ ] **Step 1: Write Dockerfile**

Create `Dockerfile`:

```dockerfile
# syntax=docker/dockerfile:1.7
FROM golang:1.22-alpine AS build
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=""
ARG BUILD_DATE=""
RUN CGO_ENABLED=0 go build \
    -ldflags="-s -w \
      -X main.version=${VERSION} \
      -X main.commit=${COMMIT} \
      -X main.buildDate=${BUILD_DATE} \
      -X github.com/crowdy/conoha-proxy/internal/adminapi.version=${VERSION}" \
    -o /out/conoha-proxy \
    ./cmd/conoha-proxy

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/conoha-proxy /usr/local/bin/conoha-proxy
USER nonroot:nonroot
EXPOSE 80 443
VOLUME ["/var/lib/conoha-proxy"]
ENTRYPOINT ["/usr/local/bin/conoha-proxy"]
CMD ["run"]
STOPSIGNAL SIGTERM
```

- [ ] **Step 2: Verify build**

```bash
docker build -t conoha-proxy:dev .
docker run --rm conoha-proxy:dev version
```

Expected: image builds, version line prints.

- [ ] **Step 3: Commit**

```bash
git add Dockerfile
git commit -m "build: multi-stage Dockerfile on distroless/static"
```

---

## Task 16: GitHub Actions — CI

**Files:**
- Create: `.github/workflows/ci.yml`

- [ ] **Step 1: Write `ci.yml`**

Create `.github/workflows/ci.yml`:

```yaml
name: ci
on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - name: Verify go mod
        run: |
          go mod tidy
          git diff --exit-code go.mod go.sum
      - name: Vet
        run: go vet ./...
      - name: Test with race
        run: go test -race -coverprofile=coverage.txt ./...
      - name: Coverage summary
        run: go tool cover -func=coverage.txt | tail -1

  lint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - uses: golangci/golangci-lint-action@v6
        with:
          version: v1.59

  docker:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: docker/setup-buildx-action@v3
      - name: Build (no push)
        run: docker build -t conoha-proxy:ci .
```

- [ ] **Step 2: Commit**

```bash
git add .github/
git commit -m "ci: GitHub Actions with test/lint/docker build"
```

---

## Task 17: Release — goreleaser + workflow

**Files:**
- Create: `.goreleaser.yaml`, `.github/workflows/release.yml`

- [ ] **Step 1: Write `.goreleaser.yaml`**

Create `.goreleaser.yaml`:

```yaml
version: 2
project_name: conoha-proxy

builds:
  - id: conoha-proxy
    main: ./cmd/conoha-proxy
    binary: conoha-proxy
    env: [CGO_ENABLED=0]
    goos: [linux, darwin]
    goarch: [amd64, arm64]
    ldflags:
      - -s -w
      - -X main.version={{.Version}}
      - -X main.commit={{.Commit}}
      - -X main.buildDate={{.Date}}
      - -X github.com/crowdy/conoha-proxy/internal/adminapi.version={{.Version}}

archives:
  - formats: [tar.gz]
    name_template: "{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}"
    files:
      - LICENSE
      - NOTICES.md
      - README.md

dockers:
  - image_templates:
      - "ghcr.io/crowdy/conoha-proxy:{{ .Version }}"
      - "ghcr.io/crowdy/conoha-proxy:latest"
    dockerfile: Dockerfile
    use: buildx
    build_flag_templates:
      - "--platform=linux/amd64"
      - "--build-arg=VERSION={{.Version}}"
      - "--build-arg=COMMIT={{.Commit}}"
      - "--build-arg=BUILD_DATE={{.Date}}"

checksum:
  name_template: "checksums.txt"
```

- [ ] **Step 2: Write `release.yml`**

Create `.github/workflows/release.yml`:

```yaml
name: release
on:
  push:
    tags: ["v*"]

permissions:
  contents: write
  packages: write

jobs:
  goreleaser:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with: { fetch-depth: 0 }
      - uses: actions/setup-go@v5
        with: { go-version: "1.22" }
      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}
      - uses: goreleaser/goreleaser-action@v6
        with:
          distribution: goreleaser
          version: latest
          args: release --clean
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

- [ ] **Step 3: Commit**

```bash
git add .goreleaser.yaml .github/
git commit -m "release: goreleaser config + tag-triggered workflow"
```

---

## Task 18: E2E Harness — Pebble + Proxy + Fake Upstreams

**Files:**
- Create: `test/e2e/harness.go`

- [ ] **Step 1: Implement `harness.go`**

Create `test/e2e/harness.go`:

```go
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
	PebbleURL   string  // ACME directory URL
	AdminSocket string
	HTTPSAddr   string
	HTTPAddr    string

	mu        sync.Mutex
	upstreams []*httptest.Server
}

// Setup builds the binary and launches Pebble + proxy. Caller must Cleanup.
// Requires Pebble installed: `go install github.com/letsencrypt/pebble/v2/cmd/pebble@latest`
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
			"listenAddress":           fmt.Sprintf("0.0.0.0:%d", dirPort),
			"managementListenAddress": "0.0.0.0:0",
			"httpPort":                80,
			"tlsPort":                 443,
			"ocspResponderURL":        "",
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

// SpawnUpstream starts an httptest server bound to a fixed localhost port
// and registers it for cleanup.
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
// given Host header.
func (h *Harness) HTTPGet(hostHeader string) *http.Response {
	req, err := http.NewRequest(http.MethodGet, "http://"+h.HTTPAddr+"/", nil)
	require.NoError(h.t, err)
	req.Host = hostHeader
	// We don't follow redirects — we want to observe the 301 to HTTPS.
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
```

- [ ] **Step 2: Commit**

```bash
git add test/e2e/harness.go
git commit -m "test(e2e): Pebble + proxy harness with unix-socket admin client"
```

---

## Task 19: E2E — Blue/Green Scenarios (MVP Done Definition)

**Files:**
- Create: `test/e2e/blue_green_test.go`

- [ ] **Step 1: Implement the six MVP scenarios**

Create `test/e2e/blue_green_test.go`:

```go
//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

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
		"name":  "myapp",
		"hosts": []string{"app.test"},
		"health_policy": map[string]any{"path": "/up"},
	})
	require.Equal(t, 201, resp.StatusCode)

	// No deploy yet → HTTPS should return 503.
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
		"name":  "myapp",
		"hosts": []string{"app.test"},
		"health_policy": map[string]any{"path": "/up", "unhealthy_threshold": 2, "interval_ms": 50, "timeout_ms": 500},
	})
	h.AdminPOST("/v1/services/myapp/deploy", map[string]any{"target_url": good.URL})

	resp := h.AdminPOST("/v1/services/myapp/deploy", map[string]any{"target_url": bad.URL})
	require.Equal(t, http.StatusFailedDependency, resp.StatusCode)

	// State unchanged: we still get "good".
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

	// Router snapshot no longer has the host.
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
```

The required imports are `io`, `net/http`, `strings`, `testing`, and `github.com/stretchr/testify/require`. Remove the unused `fmt` / `time` imports from the scaffold above — `goimports` will clean them automatically.

- [ ] **Step 2: Verify e2e builds**

```bash
go build -tags=e2e ./test/e2e/...
```

Expected: no errors.

- [ ] **Step 3: Run e2e (requires Pebble installed)**

```bash
go install github.com/letsencrypt/pebble/v2/cmd/pebble@latest
E2E=1 go test -tags=e2e -timeout=5m ./test/e2e/...
```

Expected: all 7 scenarios PASS.

- [ ] **Step 4: Commit**

```bash
git add test/e2e/
git commit -m "test(e2e): MVP done-definition scenarios (1-6 + HTTP redirect)"
```

---

## Task 20: Docs — README, architecture, ops-runbook, admin-api

**Files:**
- Modify: `README.md`
- Create: `docs/architecture.md`, `docs/ops-runbook.md`, `docs/admin-api.md`

- [ ] **Step 1: Expand `README.md`**

Overwrite `README.md` with the following structure (the outer ```markdown``` fence is just for this plan; in the real file use raw markdown):

1. **H1 title**: `# conoha-proxy`
2. **Badges**: `ci` workflow badge + Apache-2.0 license badge
3. **Intro paragraph** (ja): "ConoHa VPS 向けの Go 製リバースプロキシデーモン。自動 HTTPS (Let's Encrypt)、マルチサービスのドメイン別ルーティング、blue/green デプロイを提供する。"
4. **Language links**: `[English](README-en.md) | [한국어](README-ko.md)`
5. **`## 特徴`**: bullet list —
   - 単一 Go バイナリ、Docker イメージとして配布
   - Let's Encrypt 自動発行・更新 (HTTP-01 challenge)
   - サービス単位の blue/green ターゲットスワップ + drain
   - ヘルスチェック (HTTP) に基づくデプロイ合否判定
   - Admin HTTP API (Unix socket または localhost TCP)
   - 構造化 JSON ログ
   - Apache-2.0
6. **`## 配置`**: the ASCII block from spec §3.2 verbatim, wrapped in a triple-backtick fence
7. **`## クイックスタート`**: a bash fence with `docker run ...` + two `curl --unix-socket ...` examples from the earlier `README.md` stub
8. **`## ドキュメント`**: links to `docs/architecture.md`, `docs/ops-runbook.md`, `docs/admin-api.md`
9. **`## ライセンス`**: "Apache-2.0 — [LICENSE](LICENSE)。サードパーティライブラリは [NOTICES.md](NOTICES.md) を参照。"

Copy the ASCII diagram exactly from the design spec file `docs/superpowers/specs/2026-04-20-conoha-proxy-design.md` §3.2.

- [ ] **Step 2: Write `README-en.md` and `README-ko.md` stubs**

Both files should be short mirrors of README.md with translated headings and a note "this is a translation — authoritative source is README.md (ja)". (Approximately 20-30 lines each.)

- [ ] **Step 3: Write `docs/architecture.md`**

Create `docs/architecture.md` with the contents of spec §3 (Architecture) and §4 (Components), adapted for implementation reality (point to actual Go packages).

- [ ] **Step 4: Write `docs/ops-runbook.md`**

Create `docs/ops-runbook.md` with:

- Contents of spec §7 (Data flow) as operator-facing procedures
- Contents of spec §8 (Error handling) as an incident-response table
- Additional sections:
  - Backup / restore (`cp state.db` and `certs/` directory)
  - Log analysis recipes (common JSON queries)
  - Upgrade procedure (stop container → pull new image → start with same volume)

- [ ] **Step 5: Write `docs/admin-api.md`**

Create `docs/admin-api.md` documenting every endpoint from spec §6:

- Full request/response bodies with examples
- All error codes (`not_found`, `validation_failed`, `probe_failed`, `store_error`, `tls_error`, `reload_failed`, `no_drain_target`, `method_not_allowed`, `invalid_body`)
- `curl` examples for each endpoint over Unix socket

- [ ] **Step 6: Commit**

```bash
git add README*.md docs/
git commit -m "docs: README + architecture / ops-runbook / admin-api"
```

---

## Task 21: NOTICES.md (third-party licenses)

**Files:**
- Modify: `NOTICES.md`

- [ ] **Step 1: Generate dependency list**

```bash
go list -m -json all | jq -r 'select(.Main == null) | .Path + " " + .Version' > /tmp/deps.txt
cat /tmp/deps.txt
```

- [ ] **Step 2: Fill `NOTICES.md`**

Overwrite `NOTICES.md`:

```markdown
# Third-Party Notices

conoha-proxy is licensed under Apache-2.0. It uses the following third-party
Go modules, each retaining its original license.

## Dependencies

| Module | License | URL |
|---|---|---|
| github.com/caddyserver/certmagic | Apache-2.0 | https://github.com/caddyserver/certmagic |
| go.etcd.io/bbolt | MIT | https://github.com/etcd-io/bbolt |
| github.com/spf13/cobra | Apache-2.0 | https://github.com/spf13/cobra |
| github.com/stretchr/testify | MIT | https://github.com/stretchr/testify |

(Transitive dependencies, including `libdns/libdns`, `mholt/acmez`, `go-acme/lego`, etc. are pulled in by certmagic; all are permissive licenses — see their repositories.)

## Test-only

| Module | License | URL |
|---|---|---|
| github.com/letsencrypt/pebble/v2 | MPL-2.0 | https://github.com/letsencrypt/pebble |

Pebble is launched as a separate process in e2e tests only; it is NOT bundled in the shipped binary or Docker image.
```

- [ ] **Step 3: Commit**

```bash
git add NOTICES.md
git commit -m "docs: populate NOTICES.md with dependency licenses"
```

---

## Task 22: Final validation pass

- [ ] **Step 1: Full test run**

```bash
go test -race ./...
```

Expected: all unit + integration tests PASS.

- [ ] **Step 2: Static analysis**

```bash
go vet ./...
golangci-lint run
```

Expected: no warnings.

- [ ] **Step 3: Build matrix**

```bash
GOOS=linux GOARCH=amd64 go build -o /dev/null ./cmd/conoha-proxy
GOOS=linux GOARCH=arm64 go build -o /dev/null ./cmd/conoha-proxy
GOOS=darwin GOARCH=amd64 go build -o /dev/null ./cmd/conoha-proxy
GOOS=darwin GOARCH=arm64 go build -o /dev/null ./cmd/conoha-proxy
```

Expected: all succeed.

- [ ] **Step 4: Docker build**

```bash
docker build --build-arg VERSION=v0.1.0-rc.1 -t conoha-proxy:rc .
docker run --rm conoha-proxy:rc version
```

Expected: `conoha-proxy v0.1.0-rc.1 ...`.

- [ ] **Step 5: E2E (with Pebble)**

```bash
E2E=1 go test -tags=e2e -timeout=10m ./test/e2e/...
```

Expected: all 7 scenarios PASS.

- [ ] **Step 6: Manual Let's Encrypt staging verification**

Deploy the image to a real VPS with a real domain pointed at it. Confirm:

1. On first HTTPS request to the configured host, the proxy fetches a Let's Encrypt **staging** certificate (run with `--acme-staging`)
2. `curl -v https://<host>/` succeeds (staging cert will be untrusted — use `-k`)
3. Proxy log contains `ACME certificate obtained` structured event

Document the verification date and staging cert serial in the PR description for the v0.1.0 release.

- [ ] **Step 7: Create annotated release tag**

```bash
git tag -a v0.1.0 -m "MVP release"
git push origin v0.1.0
```

The `release` workflow builds and publishes the Docker image + binaries.
