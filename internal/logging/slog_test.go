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
