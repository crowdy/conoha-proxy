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
