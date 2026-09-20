// Package storage provides the narrow object-storage boundary used by domain
// services. Business code never receives an OSS SDK client or credentials.
package storage

import (
	"context"
	"errors"
	"io"
	"time"
)

var ErrNotConfigured = errors.New("object storage is not configured")

// ObjectStorage is deliberately provider-neutral so knowledge services can be
// tested with an in-memory fake and can later support another private store.
type ObjectStorage interface {
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error
	Delete(ctx context.Context, key string) error
	Exists(ctx context.Context, key string) (bool, error)
	SignedGetURL(ctx context.Context, key string, expiration time.Duration) (string, error)
}
