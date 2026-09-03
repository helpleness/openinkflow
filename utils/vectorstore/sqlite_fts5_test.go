package vectorstore

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestSQLiteFTS5StoreUsesBusinessQueryAsCandidateSet(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "fts.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec(`CREATE VIRTUAL TABLE search_index USING fts5(collection UNINDEXED, record_id UNINDEXED, owner_id UNINDEXED, content, tokenize='trigram case_sensitive 0')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE notes(id INTEGER PRIMARY KEY, owner_id INTEGER NOT NULL)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO search_index(collection, record_id, owner_id, content) VALUES('notes', 7, 3, 'mountain archive')`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO notes(id, owner_id) VALUES(7, 3)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO search_index(collection, record_id, owner_id, content) VALUES('notes', 8, 4, 'mountain archive')").Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec("INSERT INTO notes(id, owner_id) VALUES(8, 4)").Error; err != nil {
		t.Fatal(err)
	}
	store := &SQLiteFTS5Store{Table: "search_index"}
	query, err := store.Search(context.Background(), StoreRequest{
		Collection: "notes", Query: "mountain", Limit: 5, Db: db.Table("notes").Where("owner_id = ?", 3),
	})
	if err != nil {
		t.Fatal(err)
	}
	var ids []uint
	if err := query.Pluck("id", &ids).Error; err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != 7 {
		t.Fatalf("unexpected IDs: %#v", ids)
	}
}
