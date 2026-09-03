package storage

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewOSSRejectsMissingPrivateCredentials(t *testing.T) {
	_, err := NewOSS(OSSConfig{})
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("NewOSS() error = %v, want ErrNotConfigured", err)
	}
}

func TestBuildKnowledgeObjectKeyIsOrganizationScoped(t *testing.T) {
	key, err := BuildKnowledgeObjectKey(12, "../2026 年度总结.pdf", time.Date(2026, time.September, 2, 0, 0, 0, 0, time.UTC), "550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := "organizations/12/knowledge/2026/09/550e8400-e29b-41d4-a716-446655440000/"
	if !strings.HasPrefix(key, wantPrefix) {
		t.Fatalf("key = %q, want prefix %q", key, wantPrefix)
	}
	if strings.Contains(key, "..") || strings.Contains(key, "\\") {
		t.Fatalf("key must not contain traversal components: %q", key)
	}
	if !IsKnowledgeObjectKeyForOrganization(12, key) {
		t.Fatalf("key should belong to organization 12: %q", key)
	}
	if IsKnowledgeObjectKeyForOrganization(13, key) {
		t.Fatalf("key must not belong to another organization: %q", key)
	}
}

func TestSanitizeFilenameRejectsEmptyAndCleansUnsafeCharacters(t *testing.T) {
	if _, err := SanitizeFilename("../.."); err == nil {
		t.Fatal("path-only filename should be rejected")
	}
	safe, err := SanitizeFilename(`..\\财务?报告<>.PDF`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(safe, `/\\<>?`) || !strings.HasSuffix(safe, ".PDF") {
		t.Fatalf("unsafe filename was not cleaned: %q", safe)
	}
}
