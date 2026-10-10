package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// LocalStorage keeps desktop knowledge files beside the desktop database.
// Only the authenticated knowledge-document API may serve these files.
type LocalStorage struct{ root string }

func NewLocal(root string) (*LocalStorage, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("local object storage root is empty")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	return &LocalStorage{root: abs}, nil
}

func (s *LocalStorage) filename(ctx context.Context, key string) (string, error) {
	if ctx == nil {
		return "", errors.New("object storage context is required")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	// Object keys always use forward slashes. Reject platform separators before
	// converting the path so a database value cannot escape the private root.
	if key == "" || len(key) > 1023 || strings.ContainsAny(key, "\\:") ||
		strings.HasPrefix(key, "/") || path.Clean(key) != key {
		return "", errors.New("invalid local object key")
	}
	parts := strings.Split(key, "/")
	if len(parts) < 5 || parts[0] != "organizations" || parts[2] != "knowledge" {
		return "", errors.New("invalid local object key")
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return "", errors.New("invalid local object key")
		}
	}
	return filepath.Join(s.root, filepath.FromSlash(key)), nil
}

func (s *LocalStorage) Upload(ctx context.Context, key string, reader io.Reader, size int64, _ string) error {
	name, err := s.filename(ctx, key)
	if err != nil {
		return err
	}
	if reader == nil || size < 0 {
		return errors.New("invalid object upload stream")
	}
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(name), ".upload-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	defer temp.Close()
	if err := temp.Chmod(0o600); err != nil {
		return err
	}
	if _, err := io.CopyN(temp, reader, size); err != nil {
		return err
	}
	var extra [1]byte
	if n, readErr := reader.Read(extra[:]); n != 0 || (readErr != nil && readErr != io.EOF) {
		return errors.New("object upload size does not match stream")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), name)
}

func (s *LocalStorage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	name, err := s.filename(ctx, key)
	if err != nil {
		return nil, err
	}
	return os.Open(name)
}

func (s *LocalStorage) Delete(ctx context.Context, key string) error {
	name, err := s.filename(ctx, key)
	if err != nil {
		return err
	}
	if err := os.Remove(name); errors.Is(err, os.ErrNotExist) {
		return nil
	} else {
		return err
	}
}

func (s *LocalStorage) Exists(ctx context.Context, key string) (bool, error) {
	name, err := s.filename(ctx, key)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(name)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return err == nil, err
}

func (s *LocalStorage) SignedGetURL(context.Context, string, time.Duration) (string, error) {
	return "", errors.New("local objects must be downloaded through the authenticated document API")
}
