package vectorstore

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync"

	usearch "github.com/unum-cloud/usearch/golang"
	"gorm.io/gorm"
)

// USearchStore 是基于 initialize 层已打开 HNSW 索引的向量 CRUD 适配器。
// 每个逻辑集合对应一个独立索引，所以可以直接使用业务主表 ID 作为 USearch Key，
// 不会把任何小说领域的表名、字段或筛选逻辑带入本包。
type USearchStore struct {
	Indexes   map[Collection]*usearch.Index
	Dimension int
	IndexPath func(Collection) string

	mu sync.RWMutex
}

// 编译期断言：USearchStore 必须完整实现统一的三方法向量 CRUD 接口。
var _ Store = (*USearchStore)(nil)
var _ CollectionRebuilder = (*USearchStore)(nil)

// Upsert 将同一逻辑集合的一批向量写入对应 HNSW 索引并保存索引文件。
// 关系数据库依旧是事实源，因此调用方应先完成业务记录写入，再调用本方法同步索引。
func (s *USearchStore) Upsert(ctx context.Context, records []StoreRequest) error {
	if len(records) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	index, collection, err := s.indexForBatch(records)
	if err != nil {
		return err
	}
	records = deduplicateUSearchRecords(records)

	// USearch 的高层 Add 包装器不接受已存在的 key。先识别本次真正新增的
	// 向量，既避免为纯更新无谓扩容，也让下面的 Remove + Add 成为真正的 upsert。
	existing := make(map[uint]bool, len(records))
	newRecords := 0
	for _, record := range records {
		contains, err := index.Contains(usearch.Key(record.ID))
		if err != nil {
			return fmt.Errorf("check USearch vector %s/%d: %w", record.Collection, record.ID, err)
		}
		existing[record.ID] = contains
		if !contains {
			newRecords++
		}
	}
	if err := reserveUSearchCapacity(index, newRecords); err != nil {
		return fmt.Errorf("reserve USearch index %s: %w", collection, err)
	}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if existing[record.ID] {
			if err := index.Remove(usearch.Key(record.ID)); err != nil {
				return fmt.Errorf("replace USearch vector %s/%d: remove existing vector: %w", record.Collection, record.ID, err)
			}
		}
		if err := index.Add(usearch.Key(record.ID), record.Vector); err != nil {
			return fmt.Errorf("upsert USearch vector %s/%d: %w", record.Collection, record.ID, err)
		}
	}
	return s.saveIndex(collection, index)
}

// deduplicateUSearchRecords 让同一批请求中的同一个 key 只写入一次，最后出现的
// 向量为准。调用方常会聚合多个来源的数据；即使上层意外重复，也不应把重复 key
// 传给禁止重复 key 的 USearch 高层包装器。
func deduplicateUSearchRecords(records []StoreRequest) []StoreRequest {
	positions := make(map[uint]int, len(records))
	unique := make([]StoreRequest, 0, len(records))
	for _, record := range records {
		if position, exists := positions[record.ID]; exists {
			unique[position] = record
			continue
		}
		positions[record.ID] = len(unique)
		unique = append(unique, record)
	}
	return unique
}

// RebuildCollection 根据完整的关系数据库快照重建一个物理集合。替代索引会先在
// 内存中构建并保存成功，随后才切换给并发读请求使用；因此构建失败不会破坏当前可用索引。
func (s *USearchStore) RebuildCollection(ctx context.Context, collection Collection, records []StoreRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	current, err := s.indexForCollection(collection)
	if err != nil {
		return err
	}
	records = deduplicateUSearchRecords(records)
	if err := s.validateCollectionRecords(collection, records); err != nil {
		return err
	}

	replacement, err := usearch.NewIndex(current.GetConfig())
	if err != nil {
		return fmt.Errorf("create replacement USearch index %s: %w", collection, err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = replacement.Destroy()
		}
	}()

	threads := uint(runtime.NumCPU())
	if threads == 0 {
		threads = 1
	}
	if err := replacement.ChangeThreadsAdd(threads); err != nil {
		return fmt.Errorf("configure replacement USearch write threads %s: %w", collection, err)
	}
	if err := replacement.ChangeThreadsSearch(threads); err != nil {
		return fmt.Errorf("configure replacement USearch search threads %s: %w", collection, err)
	}
	if err := reserveUSearchCapacity(replacement, len(records)); err != nil {
		return fmt.Errorf("reserve replacement USearch index %s: %w", collection, err)
	}
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := replacement.Add(usearch.Key(record.ID), record.Vector); err != nil {
			return fmt.Errorf("rebuild USearch vector %s/%d: %w", collection, record.ID, err)
		}
	}
	if err := s.saveIndex(collection, replacement); err != nil {
		return err
	}

	s.Indexes[collection] = replacement
	committed = true
	_ = current.Destroy()
	return nil
}

// Delete 从对应的 HNSW 索引移除指定向量，并保存发生改变的索引文件。
// 删除不会影响 SQLite 或 PostgreSQL 中的业务记录。
func (s *USearchStore) Delete(ctx context.Context, keys []StoreRequest) error {
	if len(keys) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s == nil || s.Dimension <= 0 || s.IndexPath == nil {
		return ErrNotConfigured
	}
	changed := make(map[Collection]bool)
	for _, key := range keys {
		if key.ID == 0 {
			return ErrInvalidID
		}
		index, ok := s.Indexes[key.Collection]
		if !ok || index == nil {
			return fmt.Errorf("%w: %s", ErrUnknownCollection, key.Collection)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		contains, err := index.Contains(usearch.Key(key.ID))
		if err != nil {
			return fmt.Errorf("check USearch vector %s/%d: %w", key.Collection, key.ID, err)
		}
		if !contains {
			continue
		}
		if err := index.Remove(usearch.Key(key.ID)); err != nil {
			return fmt.Errorf("delete USearch vector %s/%d: %w", key.Collection, key.ID, err)
		}
		changed[key.Collection] = true
	}
	for collection := range changed {
		if err := s.saveIndex(collection, s.Indexes[collection]); err != nil {
			return err
		}
	}
	return nil
}

// Search 在指定集合的 HNSW 索引中取得命中的业务主键，再追加至调用方传入的 GORM 查询。
// 对结构化条件、项目权限和软删除的过滤仍由调用方的 Db 负责；本方法不定义任何业务字段。
func (s *USearchStore) Search(ctx context.Context, req StoreRequest) (gorm.DB, error) {
	if req.Db == nil {
		return gorm.DB{}, ErrNotConfigured
	}
	if req.Limit <= 0 {
		return *req.Db.WithContext(ctx).Where("1 = 0"), nil
	}
	if err := ctx.Err(); err != nil {
		return gorm.DB{}, err
	}
	if s == nil || s.Dimension <= 0 {
		return gorm.DB{}, ErrNotConfigured
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(req.Vector) != s.Dimension {
		return gorm.DB{}, ErrInvalidDimension
	}
	index, ok := s.Indexes[req.Collection]
	if !ok || index == nil {
		return gorm.DB{}, fmt.Errorf("%w: %s", ErrUnknownCollection, req.Collection)
	}

	keys, _, err := index.Search(req.Vector, uint(req.Limit))
	if err != nil {
		return gorm.DB{}, fmt.Errorf("search USearch collection %s: %w", req.Collection, err)
	}
	ids := make([]uint, 0, len(keys))
	for _, key := range keys {
		if uint64(key) > uint64(math.MaxUint) {
			continue
		}
		ids = append(ids, uint(key))
	}

	query := req.Db.WithContext(ctx)
	if len(ids) == 0 {
		// HNSW 中没有命中不能退化成不带条件的主表查询，否则会把当前集合
		// 的全部记录误当作检索结果返回给知识工具链。
		return *query.Where("1 = 0"), nil
	}
	// SQL 的 IN 不保证按 HNSW 返回距离排序。将业务主键顺序显式编码到
	// CASE 中，使上层融合排序能得到真实的向量名次，而不是数据库碰巧的行顺序。
	var order strings.Builder
	order.WriteString("CASE id")
	for position, id := range ids {
		fmt.Fprintf(&order, " WHEN %d THEN %d", id, position)
	}
	order.WriteString(" ELSE ")
	fmt.Fprintf(&order, "%d END", len(ids))
	return *query.Where("id IN ?", ids).Order(order.String()), nil
}

func (s *USearchStore) indexForBatch(records []StoreRequest) (*usearch.Index, Collection, error) {
	if len(records) == 0 {
		return nil, "", ErrInvalidID
	}
	collection := records[0].Collection
	index, err := s.indexForCollection(collection)
	if err != nil {
		return nil, "", err
	}
	if err := s.validateCollectionRecords(collection, records); err != nil {
		return nil, "", err
	}
	return index, collection, nil
}

func (s *USearchStore) indexForCollection(collection Collection) (*usearch.Index, error) {
	if s == nil || s.Dimension <= 0 || s.IndexPath == nil {
		return nil, ErrNotConfigured
	}
	index, ok := s.Indexes[collection]
	if !ok || index == nil {
		return nil, fmt.Errorf("%w: %s", ErrUnknownCollection, collection)
	}
	return index, nil
}

func (s *USearchStore) validateCollectionRecords(collection Collection, records []StoreRequest) error {
	for _, record := range records {
		if record.Collection != collection {
			return fmt.Errorf("USearch collection rebuild accepts one collection per batch, got %q and %q", collection, record.Collection)
		}
		if record.ID == 0 {
			return ErrInvalidID
		}
		if len(record.Vector) != s.Dimension {
			return ErrInvalidDimension
		}
	}
	return nil
}

func (s *USearchStore) saveIndex(collection Collection, index *usearch.Index) error {
	path := s.IndexPath(collection)
	if path == "" {
		return ErrNotConfigured
	}
	if err := index.Save(path); err != nil {
		return fmt.Errorf("save USearch index %s: %w", collection, err)
	}
	return nil
}

// reserveUSearchCapacity 在第一次写入前预留 HNSW 图的存储空间；后续批量写入时
// 按需扩容，避免调用方需要了解底层索引容量。
func reserveUSearchCapacity(index *usearch.Index, incoming int) error {
	if incoming <= 0 {
		return nil
	}
	capacity, err := index.Capacity()
	if err != nil {
		return err
	}
	length, err := index.Len()
	if err != nil {
		return err
	}
	needed := length + uint(incoming)
	if capacity >= needed {
		return nil
	}
	if capacity > needed/2 {
		needed = capacity * 2
	}
	return index.Reserve(needed)
}
