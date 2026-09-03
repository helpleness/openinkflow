package storage

import (
	"fmt"
	"path"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
)

const knowledgeKeyPrefix = "organizations"

// NewKnowledgeObjectKey creates a tenant-isolated source-file key. The random
// UUID is part of the directory rather than the filename so an original name
// can remain useful when inspecting a private bucket.
func NewKnowledgeObjectKey(organizationID uint, filename string, now time.Time) (string, error) {
	return BuildKnowledgeObjectKey(organizationID, filename, now, uuid.NewString())
}

// BuildKnowledgeObjectKey is the deterministic form used by tests.
func BuildKnowledgeObjectKey(organizationID uint, filename string, now time.Time, objectID string) (string, error) {
	if organizationID == 0 {
		return "", fmt.Errorf("organization id is required")
	}
	if _, err := uuid.Parse(objectID); err != nil {
		return "", fmt.Errorf("invalid object uuid: %w", err)
	}
	safeName, err := SanitizeFilename(filename)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%d/knowledge/%04d/%02d/%s/%s", knowledgeKeyPrefix, organizationID, now.Year(), int(now.Month()), objectID, safeName), nil
}

// NewKnowledgeImageObjectKey places an extracted image below its source
// document's directory. It rejects a source key that is not a valid knowledge
// source key instead of trusting a database value as an object path.
func NewKnowledgeImageObjectKey(organizationID uint, sourceKey, filename string) (string, error) {
	if !IsKnowledgeObjectKeyForOrganization(organizationID, sourceKey) {
		return "", fmt.Errorf("source object key is outside the organization knowledge prefix")
	}
	safeName, err := SanitizeFilename(filename)
	if err != nil {
		return "", err
	}
	return path.Join(path.Dir(sourceKey), "images", uuid.NewString()+"_"+safeName), nil
}

// IsKnowledgeObjectKeyForOrganization prevents a document row from being used
// to sign or delete another organization's object.
func IsKnowledgeObjectKeyForOrganization(organizationID uint, key string) bool {
	prefix := fmt.Sprintf("%s/%d/knowledge/", knowledgeKeyPrefix, organizationID)
	key = strings.TrimSpace(key)
	return key != "" && !strings.HasPrefix(key, "/") && path.Clean(key) == key && strings.HasPrefix(key, prefix)
}

// SanitizeFilename removes path elements and unsafe characters while retaining
// Unicode letters, numbers, a single useful extension, dots, underscores and
// hyphens. It is used both for the object key and user-visible original name.
func SanitizeFilename(filename string) (string, error) {
	filename = strings.TrimSpace(strings.ReplaceAll(filename, "\\", "/"))
	filename = path.Base(filename)
	if filename == "" || filename == "." || filename == "/" {
		return "", fmt.Errorf("invalid filename")
	}

	var builder strings.Builder
	lastUnderscore := false
	for _, value := range filename {
		switch {
		case unicode.IsLetter(value), unicode.IsNumber(value), value == '.', value == '-', value == '_':
			builder.WriteRune(value)
			lastUnderscore = false
		case unicode.IsSpace(value):
			if !lastUnderscore {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		default:
			if !lastUnderscore {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	safeName := strings.Trim(builder.String(), "._-")
	if safeName == "" {
		return "", fmt.Errorf("filename contains no usable characters")
	}
	if len([]rune(safeName)) > 160 {
		extension := path.Ext(safeName)
		stem := strings.TrimSuffix(safeName, extension)
		limit := 160 - len([]rune(extension))
		if limit < 1 {
			return "", fmt.Errorf("filename extension is too long")
		}
		safeName = string([]rune(stem)[:limit]) + extension
	}
	return safeName, nil
}
