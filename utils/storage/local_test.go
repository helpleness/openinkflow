package storage

import (
	"bytes"
	"context"
	"io"
	"path/filepath"
	"testing"
)

func TestLocalStoragePersistsAndDeletesKnowledgeObject(t *testing.T) {
	root := filepath.Join(t.TempDir(), "knowledge-objects")
	store, err := NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	key := "organizations/12/knowledge/2026/10/550e8400-e29b-41d4-a716-446655440000/report.md"
	content := []byte("# 测试文档")
	if err := store.Upload(context.Background(), key, bytes.NewReader(content), int64(len(content)), "text/markdown"); err != nil {
		t.Fatal(err)
	}
	// Reopening the store simulates the next desktop process after an upgrade.
	store, err = NewLocal(root)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := store.Download(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	reader.Close()
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("reopened object = %q, %v", got, err)
	}
	if err := store.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	exists, err := store.Exists(context.Background(), key)
	if err != nil || exists {
		t.Fatalf("deleted object exists = %v, %v", exists, err)
	}
}

func TestLocalStorageRejectsTraversalAndWrongSize(t *testing.T) {
	store, err := NewLocal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"../outside", "organizations/1/knowledge/../../outside", "organizations/1/knowledge/a\\..\\outside", "C:/outside", "organizations/1/knowledge/a:stream",
	} {
		if err := store.Upload(context.Background(), key, bytes.NewReader([]byte("x")), 1, ""); err == nil {
			t.Errorf("accepted unsafe key %q", key)
		}
	}
	key := "organizations/1/knowledge/2026/10/id/file.txt"
	if err := store.Upload(context.Background(), key, bytes.NewReader([]byte("too long")), 2, ""); err == nil {
		t.Fatal("accepted incorrect upload length")
	}
	exists, err := store.Exists(context.Background(), key)
	if err != nil || exists {
		t.Fatalf("partial object exists = %v, %v", exists, err)
	}
}
