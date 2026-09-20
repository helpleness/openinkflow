package vectorstore

import (
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

type testStore struct{}

func (testStore) Upsert(context.Context, []StoreRequest) error { return nil }
func (testStore) Search(context.Context, StoreRequest) (gorm.DB, error) {
	return gorm.DB{}, nil
}
func (testStore) Delete(context.Context, []StoreRequest) error { return nil }

func TestNewVectorStoreUsesSelectedFactory(t *testing.T) {
	want := testStore{}
	store, err := NewVectorStore(BackendPGVector, map[BackendType]Factory{
		BackendPGVector: func() (Store, error) { return want, nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.(testStore); !ok {
		t.Fatalf("expected selected store, got %T", store)
	}
	if _, err := NewVectorStore(BackendUSearch, nil); !errors.Is(err, ErrUnsupportedBackend) {
		t.Fatalf("expected unsupported backend error, got %v", err)
	}
}
