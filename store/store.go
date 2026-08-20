// Package store persists opaque SEAL packages with atomic Take delivery.
package store

import (
	"context"
	"errors"
	"time"
)

// Meta is operational metadata (never contains plaintext or keys).
type Meta struct {
	ID            string
	CreatedAt     time.Time
	ExpiresAt     time.Time
	BurnAfterRead bool
	Size          int64
	FormatVersion uint8
	// HasPassphrase is not stored on the secret row. Stores bump the
	// passphrase counter when it is true on Create.
	HasPassphrase bool
}

// Record is a secret as stored: meta + opaque blob.
type Record struct {
	Meta Meta
	Blob []byte
}

var (
	// ErrNotFound is returned when an id is missing, already burned, or expired.
	ErrNotFound = errors.New("store: not found")
	// ErrExists is returned when Create sees a duplicate id (server should retry mint).
	ErrExists = errors.New("store: id exists")
	// ErrLocked is returned when the data directory lock cannot be acquired.
	ErrLocked = errors.New("store: data directory locked by another process")
)

// Store is the v1 persistence API. HTTP GET must call Take only.
type Store interface {
	// Create inserts a new record. Meta.ID must already be set.
	Create(ctx context.Context, meta Meta, blob []byte) error

	// Take is the sole download/delivery primitive for HTTP GET.
	// Atomic: expired → delete + ErrNotFound; burn → delete then return;
	// multi-read → return without delete. Concurrent Takes on burn=1: one winner.
	Take(ctx context.Context, id string) (Record, error)

	// Get is a non-destructive snapshot for tests/ops. MUST NOT implement burn.
	Get(ctx context.Context, id string) (Record, error)

	Delete(ctx context.Context, id string) error
	DeleteExpired(ctx context.Context, now time.Time) (int, error)
	Count(ctx context.Context) (active int64, err error)
	Stats(ctx context.Context) (Counters, error)

	// Close releases resources and the process lock.
	Close() error
}
