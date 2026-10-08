package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"InkFlow/internal/ai/eval"
	model "InkFlow/model/officialdoc"
	"InkFlow/utils/vectorstore"
	"github.com/glebarez/sqlite"
	"github.com/pgvector/pgvector-go"
	usearch "github.com/unum-cloud/usearch/golang"
	"gorm.io/gorm"
)

const fixtureCollection = vectorstore.Collection("officialdoc_knowledge_chunks")

type validationReport struct {
	Fixture           string            `json:"fixture"`
	QueryMode         string            `json:"query_mode"`
	DBRows            int               `json:"db_rows"`
	IndexRows         uint              `json:"index_rows"`
	IndexReloaded     bool              `json:"index_reloaded"`
	SelfTop1Hits      int               `json:"self_top1_hits"`
	SelfQueries       int               `json:"self_queries"`
	ExactTop10Overlap int               `json:"exact_top10_overlap"`
	ExactTop10Total   int               `json:"exact_top10_total"`
	ExactRecallAtK    map[int]eval.Rate `json:"exact_recall_at_k"`
	HNSW              struct {
		Metric          string `json:"metric"`
		Quantization    string `json:"quantization"`
		Connectivity    uint   `json:"connectivity"`
		ExpansionAdd    uint   `json:"expansion_add"`
		ExpansionSearch uint   `json:"expansion_search"`
	} `json:"hnsw"`
}

type retrievalFixture struct {
	DB         *gorm.DB
	Store      *vectorstore.USearchStore
	Index      *usearch.Index
	Rows       []model.KnowledgeChunk
	Vectors    [][]float32
	Directory  string
	Validation validationReport
}

// buildFixture persists the same knowledge chunk rows and HNSW configuration
// used by the desktop client, then closes and reloads both files for searching.
func buildFixture(root string, corpus []eval.CorpusChunk, vectors [][]float32) (*retrievalFixture, error) {
	if len(corpus) == 0 || len(corpus) != len(vectors) {
		return nil, fmt.Errorf("invalid fixture corpus or vectors")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(root, "retrieval-")
	if err != nil {
		return nil, err
	}
	dbPath := filepath.Join(dir, "inkflow-eval.db")
	indexPath := filepath.Join(dir, string(fixtureCollection)+".usearch")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open SQLite fixture: %w", err)
	}
	if err := db.AutoMigrate(&model.KnowledgeChunk{}); err != nil {
		return nil, fmt.Errorf("migrate SQLite fixture: %w", err)
	}
	rows := make([]model.KnowledgeChunk, len(corpus))
	records := make([]vectorstore.StoreRequest, len(corpus))
	for i, chunk := range corpus {
		if len(vectors[i]) != len(vectors[0]) {
			return nil, fmt.Errorf("embedding dimension mismatch at %s", chunk.ID)
		}
		value := pgvector.NewVector(vectors[i])
		rows[i] = model.KnowledgeChunk{DocumentID: 1, TenantID: 1, OrganizationID: 1, ChunkIndex: i, Title: chunk.Title, Content: chunk.Content, Metadata: chunk.ID, Embedding: &value}
		if err := db.Create(&rows[i]).Error; err != nil {
			return nil, fmt.Errorf("insert %s into SQLite fixture: %w", chunk.ID, err)
		}
		records[i] = vectorstore.StoreRequest{Collection: fixtureCollection, ID: rows[i].ID, Vector: vectors[i]}
	}
	config := usearch.DefaultConfig(uint(len(vectors[0])))
	config.Metric = usearch.Cosine
	config.Quantization = usearch.F32
	config.Connectivity = 32
	config.ExpansionAdd = 256
	config.ExpansionSearch = 64
	index, err := usearch.NewIndex(config)
	if err != nil {
		return nil, fmt.Errorf("create fixture HNSW: %w", err)
	}
	store := &vectorstore.USearchStore{Indexes: map[vectorstore.Collection]*usearch.Index{fixtureCollection: index}, Dimension: len(vectors[0]), IndexPath: func(vectorstore.Collection) string { return indexPath }}
	if err := store.Upsert(context.Background(), records); err != nil {
		_ = index.Destroy()
		return nil, fmt.Errorf("persist fixture HNSW: %w", err)
	}
	if err := index.Destroy(); err != nil {
		return nil, fmt.Errorf("close fixture HNSW: %w", err)
	}
	if sqlDB, err := db.DB(); err != nil {
		return nil, err
	} else if err := sqlDB.Close(); err != nil {
		return nil, fmt.Errorf("close fixture SQLite: %w", err)
	}
	db, err = gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("reopen fixture SQLite: %w", err)
	}
	index, err = usearch.NewIndex(config)
	if err != nil {
		return nil, fmt.Errorf("create reload HNSW: %w", err)
	}
	if err := index.Load(indexPath); err != nil {
		_ = index.Destroy()
		return nil, fmt.Errorf("reload fixture HNSW: %w", err)
	}
	store.Indexes[fixtureCollection] = index
	f := &retrievalFixture{DB: db, Store: store, Index: index, Rows: rows, Vectors: vectors, Directory: dir}
	var count int64
	if err := db.Model(&model.KnowledgeChunk{}).Count(&count).Error; err != nil {
		return nil, fmt.Errorf("count fixture rows: %w", err)
	}
	indexCount, err := index.Len()
	if err != nil {
		return nil, fmt.Errorf("count fixture vectors: %w", err)
	}
	if count != int64(len(corpus)) || indexCount != uint(len(corpus)) {
		return nil, fmt.Errorf("fixture counts differ: SQLite=%d, HNSW=%d, corpus=%d", count, indexCount, len(corpus))
	}
	f.Validation.Fixture = "SQLite knowledge_chunks + saved/reloaded USearch"
	f.Validation.DBRows = int(count)
	f.Validation.IndexRows = indexCount
	f.Validation.IndexReloaded = true
	f.Validation.SelfQueries = len(corpus)
	f.Validation.HNSW.Metric = "cosine"
	f.Validation.HNSW.Quantization = "f32"
	f.Validation.HNSW.Connectivity = config.Connectivity
	f.Validation.HNSW.ExpansionAdd = config.ExpansionAdd
	f.Validation.HNSW.ExpansionSearch = config.ExpansionSearch
	for i, row := range rows {
		keys, _, err := index.Search(vectors[i], 1)
		if err != nil {
			return nil, fmt.Errorf("self-query %s: %w", row.Metadata, err)
		}
		if len(keys) > 0 && keys[0] == usearch.Key(row.ID) {
			f.Validation.SelfTop1Hits++
		}
	}
	return f, nil
}

func (f *retrievalFixture) Close() {
	if f.Index != nil {
		_ = f.Index.Destroy()
	}
	if f.DB != nil {
		if db, err := f.DB.DB(); err == nil {
			_ = db.Close()
		}
	}
}

func (f *retrievalFixture) Search(query []float32, limit int) ([]model.KnowledgeChunk, error) {
	base := f.DB.Model(&model.KnowledgeChunk{}).Where("tenant_id = ? AND organization_id = ? AND embedding IS NOT NULL", 1, 1)
	queryDB, err := f.Store.Search(context.Background(), vectorstore.StoreRequest{Collection: fixtureCollection, Vector: query, Limit: limit, Db: base})
	if err != nil {
		return nil, err
	}
	var found []model.KnowledgeChunk
	if err := queryDB.Find(&found).Error; err != nil {
		return nil, err
	}
	return found, nil
}

func (f *retrievalFixture) ExactTopK(query []float32, k int) ([]string, error) {
	type item struct {
		id       string
		distance float32
	}
	items := make([]item, len(f.Rows))
	for i, row := range f.Rows {
		distance, err := usearch.Distance(query, f.Vectors[i], uint(len(query)), usearch.Cosine)
		if err != nil {
			return nil, err
		}
		items[i] = item{id: row.Metadata, distance: distance}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].distance < items[j].distance })
	if k > len(items) {
		k = len(items)
	}
	ids := make([]string, k)
	for i := range ids {
		ids[i] = items[i].id
	}
	return ids, nil
}
