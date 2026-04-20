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
