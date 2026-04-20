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
