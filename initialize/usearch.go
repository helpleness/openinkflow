package initialize

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"InkFlow/config"
	"InkFlow/global"
	"InkFlow/utils/vectorstore"

	usearch "github.com/unum-cloud/usearch/golang"
)

const (
	usearchIndexExtension     = ".usearch"
	defaultHNSWM              = 32
	defaultHNSWEFConstruction = 256
	defaultHNSWEFSearch       = 64
)

var usearchClose func() error

// initializeUSearch 创建并打开本机 HNSW 索引。每个逻辑集合各有一个索引文件，
// 因而业务主表自增 ID 可直接作为 Key；索引目录、HNSW 参数和集合白名单都属于初始化层。
func initializeUSearch(ctx context.Context) (vectorstore.Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cfg := global.GVA_CONFIG.RAG
	dimension := cfg.VectorDimension
	if dimension <= 0 {
		dimension = vectorstore.DefaultDimension
	}
	path := strings.TrimSpace(cfg.VectorPath)
	if path == "" {
		path = "./data/inkflow_vectors"
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve USearch path: %w", err)
	}
	if err := os.MkdirAll(absPath, 0o755); err != nil {
		return nil, fmt.Errorf("create USearch directory: %w", err)
	}

	collections := usearchCollections()
	indexes := make(map[vectorstore.Collection]*usearch.Index, len(collections))
	closeIndexes := func() error {
		var result error
		for _, index := range indexes {
			if index != nil {
				if err := index.Destroy(); err != nil && result == nil {
					result = err
				}
			}
		}
		return result
	}
	for _, collection := range collections {
		index, err := openUSearchIndex(usearchIndexPath(absPath, collection), dimension, cfg)
		if err != nil {
			_ = closeIndexes()
			return nil, err
		}
		indexes[collection] = index
	}

	store := &vectorstore.USearchStore{
		Indexes:   indexes,
		Dimension: dimension,
		IndexPath: func(collection vectorstore.Collection) string {
			return usearchIndexPath(absPath, collection)
		},
	}
	usearchClose = closeIndexes
	return store, nil
}

// closeUSearch 关闭由 initializeUSearch 创建的 HNSW 索引，调用多次是安全的。
func closeUSearch() error {
	if usearchClose == nil {
		return nil
	}
	closeResource := usearchClose
	usearchClose = nil
	return closeResource()
}

func usearchCollections() []vectorstore.Collection {
	return []vectorstore.Collection{"officialdoc_knowledge_chunks"}
}

func usearchIndexPath(directory string, collection vectorstore.Collection) string {
	return filepath.Join(directory, string(collection)+usearchIndexExtension)
}

func openUSearchIndex(path string, dimension int, cfg config.RAG) (*usearch.Index, error) {
	options := usearch.DefaultConfig(uint(dimension))
	options.Metric = usearch.Cosine
	options.Quantization = usearch.F32
	options.Connectivity = uint(defaultIfNonPositive(cfg.HNSWM, defaultHNSWM))
	options.ExpansionAdd = uint(defaultIfNonPositive(cfg.HNSWEFConstruction, defaultHNSWEFConstruction))
	options.ExpansionSearch = uint(defaultIfNonPositive(cfg.HNSWEFSearch, defaultHNSWEFSearch))

	index, err := usearch.NewIndex(options)
	if err != nil {
		return nil, fmt.Errorf("create USearch index %q: %w", path, err)
	}
	if _, err := os.Stat(path); err == nil {
		if err := index.Load(path); err != nil {
			_ = index.Destroy()
			return nil, fmt.Errorf("load USearch index %q: %w", path, err)
		}
		dimensions, err := index.Dimensions()
		if err != nil {
			_ = index.Destroy()
			return nil, fmt.Errorf("read USearch index dimensions %q: %w", path, err)
		}
		if dimensions != uint(dimension) {
			_ = index.Destroy()
			return nil, fmt.Errorf("USearch index %q has dimension %d, expected %d; rebuild the local vector index", path, dimensions, dimension)
		}
	} else if !os.IsNotExist(err) {
		_ = index.Destroy()
		return nil, fmt.Errorf("inspect USearch index %q: %w", path, err)
	}
	threads := uint(max(1, runtime.NumCPU()))
	if err := index.ChangeThreadsAdd(threads); err != nil {
		_ = index.Destroy()
		return nil, fmt.Errorf("configure USearch write threads %q: %w", path, err)
	}
	if err := index.ChangeThreadsSearch(threads); err != nil {
		_ = index.Destroy()
		return nil, fmt.Errorf("configure USearch search threads %q: %w", path, err)
	}
	return index, nil
}

func defaultIfNonPositive(value int, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
