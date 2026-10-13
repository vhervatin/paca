package apikeydom

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Repository defines persistence operations for API keys.
type Repository interface {
	// FindByID returns the API key with the given ID.
	FindByID(ctx context.Context, id uuid.UUID) (*APIKey, error)
	// FindByHash looks up an API key by its SHA-256 hash.
	// Returns ErrNotFound when no matching key exists or its account is
	// deleted. The result carries AccountKind.
	FindByHash(ctx context.Context, keyHash string) (*APIKey, error)
	// ListByAccountID returns all non-revoked API keys of the given account,
	// ordered by creation date descending.
	ListByAccountID(ctx context.Context, accountID uuid.UUID) ([]*APIKey, error)
	// Create persists a new API key.
	Create(ctx context.Context, key *APIKey, keyHash string) error
	// Revoke soft-deletes an API key by setting revoked_at.
	Revoke(ctx context.Context, id uuid.UUID) error
	// UpdateLastUsed sets last_used_at to the provided time.
	UpdateLastUsed(ctx context.Context, id uuid.UUID, at time.Time) error
}
