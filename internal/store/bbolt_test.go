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
