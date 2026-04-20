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
