package inference

import (
	"context"
	"fmt"
	"slices"
	"time"

	"InkFlow/global"

	"go.uber.org/zap"
)

type LocalProvider struct{}

func (LocalProvider) Embedding(ctx context.Context, text string) ([]float32, error) {
	if global.GVA_LLM_EMBEDDING == nil {
		return nil, fmt.Errorf("本地 Embedding 引擎未就绪：请配置 llm-local.embedding.model-path 并重启服务")
	}

	// 1. 开始计时
	start := time.Now()

	// 2. 调用本地 CGO 推理引擎
	vec, err := global.GVA_LLM_EMBEDDING.Embedding(text)

	// 3. 计算耗时
	duration := time.Since(start)

	if err != nil {
		return nil, err
	}

	// 4. 将统计结果写入 Info 日志
	// 这样在导入 Markdown 片段时，控制台能清晰看到每个实体的向量化耗时
	global.GVA_LOG.Info("Embedding 推理完成",
		zap.Duration("duration", duration),
		zap.Int("text_len", len(text)),
		zap.Int("vec_dim", len(vec)),
	)

	return vec, nil
}

func (LocalProvider) Rerank(ctx context.Context, query string, docs []string, topN int) ([]ScoredDocument, error) {
	start := time.Now()
	if global.GVA_LOG != nil {
		modelInputs := make([]string, len(docs))
		for index, doc := range docs {
			modelInputs[index] = query + " " + doc
		}
		global.GVA_LOG.Info("Rerank model raw input",
			zap.String("query", query),
			zap.Strings("documents", docs),
			zap.Strings("model_inputs", modelInputs),
			zap.Int("doc_count", len(docs)),
			zap.Int("top_n", topN),
		)
	}
	engine := global.GVA_LLM_RERANK
	if engine == nil {
		err := fmt.Errorf("本地 Rerank 引擎未就绪：请配置 llm-local.rerank.model-path 并重启服务")
		if global.GVA_LOG != nil {
			global.GVA_LOG.Warn("Rerank 推理失败",
				zap.Duration("duration", time.Since(start)),
				zap.Int("query_len", len([]rune(query))),
				zap.Int("doc_count", len(docs)),
				zap.Int("top_n", topN),
				zap.Error(err),
			)
		}
		return nil, err
	}

	scores, err := engine.Rerank(query, docs)
	if err != nil {
		if global.GVA_LOG != nil {
			global.GVA_LOG.Warn("Rerank 推理失败",
				zap.Duration("duration", time.Since(start)),
				zap.Int("query_len", len([]rune(query))),
				zap.Int("doc_count", len(docs)),
				zap.Int("top_n", topN),
				zap.Error(err),
			)
		}
		return nil, err
	}

	results := make([]ScoredDocument, 0, len(scores))
	for i, score := range scores {
		text := ""
		if i >= 0 && i < len(docs) {
			text = docs[i]
		}
		results = append(results, ScoredDocument{
			Index: i,
			Score: score,
			Text:  text,
		})
	}

	slices.SortFunc(results, func(a, b ScoredDocument) int {
		if a.Score > b.Score {
			return -1
		}
		if a.Score < b.Score {
			return 1
		}
		return 0
	})
	if topN > 0 && topN < len(results) {
		results = results[:topN]
	}
	if global.GVA_LOG != nil {
		global.GVA_LOG.Info("Rerank 推理完成",
			zap.Duration("duration", time.Since(start)),
			zap.Int("query_len", len([]rune(query))),
			zap.Int("doc_count", len(docs)),
			zap.Int("top_n", topN),
			zap.Int("result_count", len(results)),
		)
	}
	return results, nil
}
