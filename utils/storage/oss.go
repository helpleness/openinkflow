package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	aliyunoss "github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// OSSConfig contains only the settings required to create one reusable OSS
// client. Credentials are supplied by application configuration, never callers
// or browser requests.
type OSSConfig struct {
	Endpoint        string
	Bucket          string
	Region          string
	AccessKeyID     string
	AccessKeySecret string
}

// OSSStorage is the Alibaba Cloud OSS implementation of ObjectStorage.
type OSSStorage struct {
	bucket *aliyunoss.Bucket
}

func NewOSS(config OSSConfig) (*OSSStorage, error) {
	config.Endpoint = strings.TrimSpace(config.Endpoint)
	config.Bucket = strings.TrimSpace(config.Bucket)
	config.Region = strings.TrimSpace(config.Region)
	config.AccessKeyID = strings.TrimSpace(config.AccessKeyID)
	config.AccessKeySecret = strings.TrimSpace(config.AccessKeySecret)
	missing := make([]string, 0, 5)
	if config.Endpoint == "" {
		missing = append(missing, "oss.endpoint")
	}
	if config.Bucket == "" {
		missing = append(missing, "oss.bucket")
	}
	if config.Region == "" {
		missing = append(missing, "oss.region")
	}
	if config.AccessKeyID == "" {
		missing = append(missing, "oss.access-key-id")
	}
	if config.AccessKeySecret == "" {
		missing = append(missing, "oss.access-key-secret")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: missing %s", ErrNotConfigured, strings.Join(missing, ", "))
	}
	if !strings.HasPrefix(config.Endpoint, "https://") && !strings.HasPrefix(config.Endpoint, "http://") {
		config.Endpoint = "https://" + config.Endpoint
	}
	client, err := aliyunoss.New(config.Endpoint, config.AccessKeyID, config.AccessKeySecret)
	if err != nil {
		return nil, fmt.Errorf("create OSS client: %w", err)
	}
	bucket, err := client.Bucket(config.Bucket)
	if err != nil {
		return nil, fmt.Errorf("open OSS bucket: %w", err)
	}
	return &OSSStorage{bucket: bucket}, nil
}

func (storage *OSSStorage) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) error {
	if err := validateOperation(ctx, key); err != nil {
		return err
	}
	if reader == nil || size < 0 {
		return errors.New("invalid object upload stream")
	}
	options := make([]aliyunoss.Option, 0, 1)
	if contentType = strings.TrimSpace(contentType); contentType != "" {
		options = append(options, aliyunoss.ContentType(contentType))
	}
	if err := storage.bucket.PutObject(key, reader, options...); err != nil {
		return fmt.Errorf("upload OSS object %q: %w", key, err)
	}
	return ctx.Err()
}

func (storage *OSSStorage) Delete(ctx context.Context, key string) error {
	if err := validateOperation(ctx, key); err != nil {
		return err
	}
	if err := storage.bucket.DeleteObject(key); err != nil {
		return fmt.Errorf("delete OSS object %q: %w", key, err)
	}
	return ctx.Err()
}

func (storage *OSSStorage) Exists(ctx context.Context, key string) (bool, error) {
	if err := validateOperation(ctx, key); err != nil {
		return false, err
	}
	exists, err := storage.bucket.IsObjectExist(key)
	if err != nil {
		return false, fmt.Errorf("check OSS object %q: %w", key, err)
	}
	return exists, ctx.Err()
}

func (storage *OSSStorage) SignedGetURL(ctx context.Context, key string, expiration time.Duration) (string, error) {
	if err := validateOperation(ctx, key); err != nil {
		return "", err
	}
	if expiration <= 0 {
		return "", errors.New("signed URL expiration must be positive")
	}
	seconds := int64(math.Ceil(expiration.Seconds()))
	url, err := storage.bucket.SignURL(key, aliyunoss.HTTPGet, seconds)
	if err != nil {
		return "", fmt.Errorf("sign OSS GET URL for %q: %w", key, err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return url, nil
}

func validateOperation(ctx context.Context, key string) error {
	if ctx == nil {
		return errors.New("object storage context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, "/") || strings.HasPrefix(key, "\\") || len(key) > 1023 {
		return errors.New("invalid object key")
	}
	return nil
}
