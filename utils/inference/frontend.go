package inference

import (
	"InkFlow/global"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	ws "InkFlow/utils/websocket"
	"go.uber.org/zap"
)

const (
	frontendRerankTimeout          = 20 * time.Second
	frontendRerankMaxDocs          = 4
	frontendReconnectMaxRetries    = 5
	frontendReconnectRetryBaseWait = 250 * time.Millisecond
)

var frontendRequestMu sync.Mutex

func logFrontendInference(level string, message string, fields ...zap.Field) {
	if global.GVA_LOG == nil {
		return
	}
	if level == "warn" {
		global.GVA_LOG.Warn(message, fields...)
		return
	}
	global.GVA_LOG.Info(message, fields...)
}

type FrontendProvider struct {
	Broker *ws.Broker
}

type frontendEmbeddingResult struct {
	Vector  []float32      `json:"vector"`
	Timings map[string]any `json:"timings"`
}

type frontendRerankResult struct {
	Results []ScoredDocument `json:"results"`
	Timings map[string]any   `json:"timings"`
}

func (p FrontendProvider) broker(ctx context.Context) *ws.Broker {
	if p.Broker != nil {
		return p.Broker
	}
	return FrontendClients.BrokerForContext(ctx)
}

func requestFrontendWithReconnectRetry(
	ctx context.Context,
	operation string,
	maxRetries int,
	baseWait time.Duration,
	request func() (ws.Response, error),
) (ws.Response, int, error) {
	for retry := 0; ; retry++ {
		response, err := request()
		if err == nil {
			return response, retry, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ws.Response{}, retry, ctxErr
		}
		if retry >= maxRetries || !ws.IsRetryableWorkerError(err) {
			return ws.Response{}, retry, err
		}

		wait := baseWait << retry
		if wait > 2*time.Second {
			wait = 2 * time.Second
		}
		logFrontendInference(
			"warn",
			"frontend inference worker connection interrupted; retrying",
			zap.String("operation", operation),
			zap.Int("retry", retry+1),
			zap.Int("max_retries", maxRetries),
			zap.Duration("retry_wait", wait),
			zap.Error(err),
		)
		if wait <= 0 {
			continue
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ws.Response{}, retry, ctx.Err()
		case <-timer.C:
		}
	}
}

func (p FrontendProvider) Embedding(ctx context.Context, text string) ([]float32, error) {
	finishProgress := BeginProgress(ctx, "embedding", fmt.Sprintf("%d 字", len([]rune(text))))
	var opErr error
	defer func() { finishProgress(opErr) }()
	queueStart := time.Now()
	frontendRequestMu.Lock()
	queueWait := time.Since(queueStart)
	defer frontendRequestMu.Unlock()
	start := time.Now()
	msg, retries, err := requestFrontendWithReconnectRetry(
		ctx,
		"embed",
		frontendReconnectMaxRetries,
		frontendReconnectRetryBaseWait,
		func() (ws.Response, error) {
			return p.broker(ctx).Request(ctx, "embed", map[string]string{"text": text})
		},
	)
	if err != nil {
		opErr = err
		logFrontendInference("warn", "frontend embedding request failed", zap.Duration("queue_wait", queueWait), zap.Duration("elapsed", time.Since(start)), zap.Duration("total", time.Since(queueStart)), zap.Int("chars", len([]rune(text))), zap.Int("retries", retries), zap.Error(err))
		return nil, err
	}
	var response frontendEmbeddingResult
	if err := json.Unmarshal(msg.Result, &response); err != nil {
		var legacyVector []float32
		if legacyErr := json.Unmarshal(msg.Result, &legacyVector); legacyErr == nil {
			response.Vector = legacyVector
		} else {
			opErr = err
			logFrontendInference("warn", "frontend embedding decode failed", zap.Duration("queue_wait", queueWait), zap.Duration("elapsed", time.Since(start)), zap.Duration("total", time.Since(queueStart)), zap.Int("chars", len([]rune(text))), zap.Error(err))
			return nil, fmt.Errorf("decode frontend embedding result: %w", err)
		}
	}
	vec := response.Vector
	if len(vec) == 0 {
		decodeErr := fmt.Errorf("frontend embedding returned an empty vector")
		opErr = decodeErr
		logFrontendInference("warn", "frontend embedding decode failed", zap.Duration("queue_wait", queueWait), zap.Duration("elapsed", time.Since(start)), zap.Duration("total", time.Since(queueStart)), zap.Int("chars", len([]rune(text))), zap.Error(decodeErr))
		return nil, decodeErr
	}
	websocketRoundTrip := time.Since(start)
	websocketTransport := websocketRoundTrip - time.Duration(frontendTimingMilliseconds(response.Timings, "browser_worker_ms")*float64(time.Millisecond))
	if websocketTransport < 0 {
		websocketTransport = 0
	}
	logFrontendInference("info", "frontend embedding request done", zap.Duration("queue_wait", queueWait), zap.Duration("websocket_round_trip", websocketRoundTrip), zap.Duration("websocket_transport", websocketTransport), zap.Duration("total", time.Since(queueStart)), zap.Int("chars", len([]rune(text))), zap.Int("dim", len(vec)), zap.Int("retries", retries), zap.Any("browser_timing_ms", response.Timings))
	return vec, nil
}

func frontendTimingMilliseconds(timings map[string]any, key string) float64 {
	if timings == nil {
		return 0
	}
	value, ok := timings[key]
	if !ok {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return typed
	case float32:
		return float64(typed)
	case int:
		return float64(typed)
	case int64:
		return float64(typed)
	case json.Number:
		parsed, _ := typed.Float64()
		return parsed
	default:
		return 0
	}
}

func (p FrontendProvider) Rerank(ctx context.Context, query string, docs []string, topN int) ([]ScoredDocument, error) {
	if len(docs) > frontendRerankMaxDocs {
		docs = docs[:frontendRerankMaxDocs]
	}
	if topN > len(docs) {
		topN = len(docs)
	}
	finishProgress := BeginProgress(ctx, "rerank", fmt.Sprintf("%d 条候选", len(docs)))
	var opErr error
	defer func() { finishProgress(opErr) }()
	queueStart := time.Now()
	frontendRequestMu.Lock()
	queueWait := time.Since(queueStart)
	defer frontendRequestMu.Unlock()
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, frontendRerankTimeout)
	defer cancel()
	msg, retries, err := requestFrontendWithReconnectRetry(
		ctx,
		"rerank",
		frontendReconnectMaxRetries,
		frontendReconnectRetryBaseWait,
		func() (ws.Response, error) {
			return p.broker(ctx).Request(ctx, "rerank", map[string]any{"query": query, "docs": docs, "top_n": topN})
		},
	)
	if err != nil {
		opErr = err
		logFrontendInference("warn", "frontend rerank request failed", zap.Duration("queue_wait", queueWait), zap.Duration("elapsed", time.Since(start)), zap.Duration("total", time.Since(queueStart)), zap.Int("docs", len(docs)), zap.Int("top_n", topN), zap.Int("retries", retries), zap.Error(err))
		return nil, err
	}
	var response frontendRerankResult
	if err := json.Unmarshal(msg.Result, &response); err != nil {
		var legacyResults []ScoredDocument
		if legacyErr := json.Unmarshal(msg.Result, &legacyResults); legacyErr == nil {
			response.Results = legacyResults
		} else {
			opErr = err
			logFrontendInference("warn", "frontend rerank decode failed", zap.Duration("queue_wait", queueWait), zap.Duration("elapsed", time.Since(start)), zap.Duration("total", time.Since(queueStart)), zap.Int("docs", len(docs)), zap.Int("top_n", topN), zap.Error(err))
			return nil, fmt.Errorf("decode frontend rerank result: %w", err)
		}
	}
	results := response.Results
	if topN > 0 && topN < len(results) {
		results = results[:topN]
	}
	websocketRoundTrip := time.Since(start)
	websocketTransport := websocketRoundTrip - time.Duration(frontendTimingMilliseconds(response.Timings, "browser_worker_ms")*float64(time.Millisecond))
	if websocketTransport < 0 {
		websocketTransport = 0
	}
	logFrontendInference("info", "frontend rerank request done", zap.Duration("queue_wait", queueWait), zap.Duration("websocket_round_trip", websocketRoundTrip), zap.Duration("websocket_transport", websocketTransport), zap.Duration("total", time.Since(queueStart)), zap.Int("docs", len(docs)), zap.Int("top_n", topN), zap.Int("results", len(results)), zap.Int("retries", retries), zap.Any("browser_timing_ms", response.Timings))
	return results, nil
}
