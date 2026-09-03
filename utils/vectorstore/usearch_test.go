package vectorstore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	usearch "github.com/unum-cloud/usearch/golang"
	"gorm.io/gorm"
)

type usSearchTestRecord struct {
	ID   uint `gorm:"primaryKey"`
	Name string
}

func TestUSearchStoreCRUD(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:usearch-store-test?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&usSearchTestRecord{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&[]usSearchTestRecord{{ID: 1, Name: "first"}, {ID: 2, Name: "second"}}).Error; err != nil {
		t.Fatal(err)
	}

	index := newUSearchTestIndex(t, 3)
	store := &USearchStore{
		Indexes:   map[Collection]*usearch.Index{"records": index},
		Dimension: 3,
		IndexPath: func(Collection) string { return filepath.Join(t.TempDir(), "records.usearch") },
	}
	if err := store.Upsert(context.Background(), []StoreRequest{
		{Collection: "records", ID: 1, Vector: []float32{1, 0, 0}},
		{Collection: "records", ID: 2, Vector: []float32{0, 1, 0}},
	}); err != nil {
		t.Fatal(err)
	}

	query, err := store.Search(context.Background(), StoreRequest{
		Collection: "records",
		Vector:     []float32{1, 0, 0},
		Limit:      1,
		Db:         db.Model(&usSearchTestRecord{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	var records []usSearchTestRecord
	if err := query.Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ID != 1 {
		t.Fatalf("expected only record 1, got %#v", records)
	}

	if err := store.Delete(context.Background(), []StoreRequest{{Collection: "records", ID: 1}}); err != nil {
		t.Fatal(err)
	}
	query, err = store.Search(context.Background(), StoreRequest{
		Collection: "records",
		Vector:     []float32{1, 0, 0},
		Limit:      2,
		Db:         db.Model(&usSearchTestRecord{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	records = nil
	if err := query.Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].ID != 2 {
		t.Fatalf("expected only record 2 after delete, got %#v", records)
	}

	if err := store.Delete(context.Background(), []StoreRequest{{Collection: "records", ID: 2}}); err != nil {
		t.Fatal(err)
	}
	query, err = store.Search(context.Background(), StoreRequest{
		Collection: "records",
		Vector:     []float32{1, 0, 0},
		Limit:      2,
		Db:         db.Model(&usSearchTestRecord{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	records = nil
	if err := query.Find(&records).Error; err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("expected no records after index is empty, got %#v", records)
	}
}

func TestUSearchStoreUpsertReplacesExistingAndDeduplicatesKeys(t *testing.T) {
	index := newUSearchTestIndex(t, 3)
	store := &USearchStore{
		Indexes:   map[Collection]*usearch.Index{"records": index},
		Dimension: 3,
		IndexPath: func(Collection) string { return filepath.Join(t.TempDir(), "records.usearch") },
	}

	if err := store.Upsert(context.Background(), []StoreRequest{
		{Collection: "records", ID: 1, Vector: []float32{1, 0, 0}},
	}); err != nil {
		t.Fatal(err)
	}

	// 同一批内重复 ID 时应按最后一条覆盖；再次 Upsert 已有 ID 也必须成功。
	if err := store.Upsert(context.Background(), []StoreRequest{
		{Collection: "records", ID: 1, Vector: []float32{0, 1, 0}},
		{Collection: "records", ID: 1, Vector: []float32{0, 0, 1}},
	}); err != nil {
		t.Fatalf("upsert duplicate key: %v", err)
	}

	count, err := index.Count(usearch.Key(1))
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected one vector for key 1, got %d", count)
	}

	keys, _, err := index.Search([]float32{0, 0, 1}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != usearch.Key(1) {
		t.Fatalf("expected updated vector for key 1, got %#v", keys)
	}
}

func TestUSearchStoreRebuildCollectionReplacesAllVectors(t *testing.T) {
	index, err := usearch.NewIndex(usearch.DefaultConfig(3))
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	store := &USearchStore{
		Indexes:   map[Collection]*usearch.Index{"records": index},
		Dimension: 3,
		IndexPath: func(Collection) string { return filepath.Join(directory, "records.usearch") },
	}
	t.Cleanup(func() {
		for _, item := range store.Indexes {
			_ = item.Destroy()
		}
	})

	if err := store.Upsert(context.Background(), []StoreRequest{
		{Collection: "records", ID: 41, Vector: []float32{1, 0, 0}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.RebuildCollection(context.Background(), "records", []StoreRequest{
		{Collection: "records", ID: 1, Vector: []float32{0, 1, 0}},
		{Collection: "records", ID: 2, Vector: []float32{0, 0, 1}},
	}); err != nil {
		t.Fatal(err)
	}

	if contains, err := store.Indexes["records"].Contains(usearch.Key(41)); err != nil || contains {
		t.Fatalf("old vector should be removed, contains=%v err=%v", contains, err)
	}
	for _, id := range []uint{1, 2} {
		contains, err := store.Indexes["records"].Contains(usearch.Key(id))
		if err != nil || !contains {
			t.Fatalf("rebuilt vector %d missing, contains=%v err=%v", id, contains, err)
		}
	}
}

func newUSearchTestIndex(t *testing.T, dimension int) *usearch.Index {
	t.Helper()
	index, err := usearch.NewIndex(usearch.DefaultConfig(uint(dimension)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := index.Destroy(); err != nil {
			t.Error(err)
		}
	})
	return index
}
