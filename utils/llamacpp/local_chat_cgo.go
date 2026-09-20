//go:build cgo && (windows || linux || darwin)

package llamacpp

/*
#cgo CFLAGS: -I${SRCDIR}/../../llama/llama.cpp/include -I${SRCDIR}/../../llama/llama.cpp/ggml/include

#include "llama.h"
#include "ggml-cpu.h"
#include <stdbool.h>
#include <stdlib.h>
#include <string.h>

static void inkflow_batch_add(struct llama_batch * batch, llama_token id, llama_pos pos, llama_seq_id seq_id, bool logits) {
    batch->token[batch->n_tokens]    = id;
    batch->pos[batch->n_tokens]      = pos;
    batch->n_seq_id[batch->n_tokens] = 1;
    batch->seq_id[batch->n_tokens][0]= seq_id;
    batch->logits[batch->n_tokens]   = logits;
    batch->n_tokens++;
}

static bool inkflow_rerank_uses_last_token(const struct llama_model * model) {
    char arch[64] = {0};
    const int32_t n = llama_model_meta_val_str(model, "general.architecture", arch, sizeof(arch));
    return n > 0 && (strcmp(arch, "qwen3") == 0 || strcmp(arch, "qwen3vl") == 0);
}
*/
import "C"

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unsafe"
)

func (e *LocalEngine) Reset() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.model == nil || e.ctx == nil || e.vocab == nil || e.smpl == nil {
		return errors.New("llamacpp engine is closed")
	}

	mem := C.llama_get_memory(e.ctx)
	C.llama_memory_clear(mem, true)
	C.llama_sampler_reset(e.smpl)

	e.sessionTokens = nil
	e.nPast = 0
	return nil
}

func (e *LocalEngine) ensureSampler(opt Options) error {
	// sampler 参数变化时重建 chain（否则温度/TopP/seed 不生效）
	if e.smpl != nil &&
		e.curTopP == opt.TopP &&
		e.curTemp == opt.Temperature &&
		e.curMinKeep == opt.MinKeep &&
		e.curSeed == uint32(opt.Seed) {
		return nil
	}

	if e.smpl != nil {
		C.llama_sampler_free(e.smpl)
		e.smpl = nil
	}

	sp := C.llama_sampler_chain_default_params()
	smpl := C.llama_sampler_chain_init(sp)
	if smpl == nil {
		return errors.New("llama_sampler_chain_init failed")
	}

	C.llama_sampler_chain_add(smpl, C.llama_sampler_init_top_p(C.float(opt.TopP), C.size_t(opt.MinKeep)))
	C.llama_sampler_chain_add(smpl, C.llama_sampler_init_temp(C.float(opt.Temperature)))
	seed := uint32(opt.Seed)
	C.llama_sampler_chain_add(smpl, C.llama_sampler_init_dist(C.uint32_t(seed)))

	e.smpl = smpl
	e.curTopP = opt.TopP
	e.curTemp = opt.Temperature
	e.curMinKeep = opt.MinKeep
	e.curSeed = seed

	return nil
}

func (e *LocalEngine) Chat(messages []Message, opt Options) (string, error) {
	opt.applyDefaults()

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.model == nil || e.ctx == nil || e.vocab == nil {
		return "", errors.New("llamacpp engine is closed")
	}
	if err := e.ensureSampler(opt); err != nil {
		return "", err
	}

	// 若调用方未显式带 system，则使用 opt.SystemPrompt 兜底
	if len(messages) == 0 || strings.ToLower(strings.TrimSpace(messages[0].Role)) != "system" {
		messages = append([]Message{{Role: "system", Content: opt.SystemPrompt}}, messages...)
	}

	// 1) build prompt string via llama_chat_apply_template
	totalChars := 0
	for _, m := range messages {
		totalChars += len(m.Role) + len(m.Content) + 8
	}
	buf := make([]byte, maxInt(256, 2*totalChars+256))

	chat := make([]C.struct_llama_chat_message, len(messages))
	cStrs := make([]unsafe.Pointer, 0, len(messages)*2+1)
	for i, m := range messages {
		r := C.CString(m.Role)
		c := C.CString(m.Content)
		cStrs = append(cStrs, unsafe.Pointer(r), unsafe.Pointer(c))
		chat[i].role = r
		chat[i].content = c
	}
	defer func() {
		for _, p := range cStrs {
			C.free(p)
		}
	}()

	tmpl := C.llama_model_chat_template(e.model, nil)
	var tmpFallback *C.char
	if tmpl == nil {
		tmpFallback = C.CString("chatml")
		defer C.free(unsafe.Pointer(tmpFallback))
		tmpl = tmpFallback
	}

	promptLen := C.llama_chat_apply_template(
		tmpl,
		(*C.struct_llama_chat_message)(unsafe.Pointer(&chat[0])),
		C.size_t(len(chat)),
		true,
		(*C.char)(unsafe.Pointer(&buf[0])),
		C.int32_t(len(buf)),
	)
	if promptLen < 0 {
		return "", errors.New("llama_chat_apply_template failed")
	}
	if int(promptLen) > len(buf) {
		buf = make([]byte, int(promptLen)*2+256)
		promptLen = C.llama_chat_apply_template(
			tmpl,
			(*C.struct_llama_chat_message)(unsafe.Pointer(&chat[0])),
			C.size_t(len(chat)),
			true,
			(*C.char)(unsafe.Pointer(&buf[0])),
			C.int32_t(len(buf)),
		)
		if promptLen < 0 {
			return "", errors.New("llama_chat_apply_template failed (retry)")
		}
	}
	prompt := string(buf[:int(promptLen)])

	// 2) tokenize prompt
	cPrompt := C.CString(prompt)
	defer C.free(unsafe.Pointer(cPrompt))

	capTokens := len(prompt) + 16
	pTokens := make([]C.llama_token, capTokens)
	n := C.llama_tokenize(
		e.vocab,
		cPrompt,
		C.int(len(prompt)),
		(*C.llama_token)(unsafe.Pointer(&pTokens[0])),
		C.int(len(pTokens)),
		true,
		true,
	)
	if n < 0 {
		capTokens = int(-n) + 8
		pTokens = make([]C.llama_token, capTokens)
		n = C.llama_tokenize(
			e.vocab,
			cPrompt,
			C.int(len(prompt)),
			(*C.llama_token)(unsafe.Pointer(&pTokens[0])),
			C.int(len(pTokens)),
			true,
			true,
		)
	}
	if n < 0 {
		return "", fmt.Errorf("llama_tokenize failed: %d", int(n))
	}
	pTokens = pTokens[:int(n)]

	// 3) eval delta tokens (reuse KV cache if prompt is an extension of previous tokens)
	common := 0
	if len(e.sessionTokens) > 0 {
		for common < len(e.sessionTokens) && common < len(pTokens) && e.sessionTokens[common] == pTokens[common] {
			common++
		}
		if common != len(e.sessionTokens) {
			// prompt diverged, reset to keep correctness
			mem := C.llama_get_memory(e.ctx)
			C.llama_memory_clear(mem, true)
			C.llama_sampler_reset(e.smpl)
			e.sessionTokens = nil
			e.nPast = 0
			common = 0
		}
	}

	if common < len(pTokens) {
		if err := e.evalTokensLocked(pTokens[common:]); err != nil {
			return "", err
		}
		e.sessionTokens = append(e.sessionTokens, pTokens[common:]...)
		e.nPast = len(e.sessionTokens)
	}

	// 4) generate assistant
	var out []byte
	batch1 := C.llama_batch_init(1, 0, 1)
	defer C.llama_batch_free(batch1)
	for i := 0; i < opt.MaxTokens; i++ {
		tok := C.llama_sampler_sample(e.smpl, e.ctx, -1)
		if C.llama_vocab_is_eog(e.vocab, tok) {
			break
		}

		bufPiece := make([]byte, 256)
		nPiece := C.llama_token_to_piece(
			e.vocab,
			tok,
			(*C.char)(unsafe.Pointer(&bufPiece[0])),
			C.int(len(bufPiece)),
			0,
			true,
		)
		if nPiece > 0 && int(nPiece) <= len(bufPiece) {
			out = append(out, bufPiece[:int(nPiece)]...)
		}

		// decode sampled token
		batch1.n_tokens = 0
		C.inkflow_batch_add(&batch1, tok, C.llama_pos(e.nPast), 0, true)
		if C.llama_decode(e.ctx, batch1) != 0 {
			return "", errors.New("llama_decode failed (gen)")
		}

		C.llama_sampler_accept(e.smpl, tok)
		e.sessionTokens = append(e.sessionTokens, tok)
		e.nPast++
	}

	return strings.TrimSpace(string(out)), nil
}

func (e *LocalEngine) evalTokensLocked(tokens []C.llama_token) error {
	if len(tokens) == 0 {
		return nil
	}
	batch := C.llama_batch_init(2048, 0, 1)
	defer C.llama_batch_free(batch)

	for idx := 0; idx < len(tokens); {
		batch.n_tokens = 0
		end := idx + 2048
		if end > len(tokens) {
			end = len(tokens)
		}
		for i := idx; i < end; i++ {
			logits := C.bool(i == end-1)
			C.inkflow_batch_add(&batch, tokens[i], C.llama_pos(e.nPast+(i-idx)), 0, logits)
		}
		if C.llama_decode(e.ctx, batch) != 0 {
			return errors.New("llama_decode failed (prompt)")
		}
		for i := idx; i < end; i++ {
			C.llama_sampler_accept(e.smpl, tokens[i])
		}
		e.nPast += (end - idx)
		idx = end
	}

	return nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (e *LocalEngine) Embedding(text string) ([]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.model == nil || e.ctx == nil || e.vocab == nil {
		return nil, errors.New("llamacpp engine is closed")
	}

	mem := C.llama_get_memory(e.ctx)
	C.llama_memory_clear(mem, true)
	C.llama_set_embeddings(e.ctx, true)
	C.llama_set_causal_attn(e.ctx, false)

	// 2. Tokenize
	cPrompt := C.CString(text)
	defer C.free(unsafe.Pointer(cPrompt))

	// 预估容量
	nTokens := len(text) + 16
	tokens := make([]C.llama_token, nTokens)

	n := C.llama_tokenize(
		e.vocab,
		cPrompt,
		C.int(len(text)),
		(*C.llama_token)(unsafe.Pointer(&tokens[0])),
		C.int(len(tokens)),
		true, // add_bos
		true, // special
	)
	if n < 0 {
		tokens = make([]C.llama_token, -int(n))
		n = C.llama_tokenize(e.vocab, cPrompt, C.int(len(text)), (*C.llama_token)(unsafe.Pointer(&tokens[0])), C.int(len(tokens)), true, true)
	}
	if n < 0 {
		return nil, fmt.Errorf("tokenize failed")
	}
	tokens = tokens[:n]
	maxTokens := int(C.llama_n_ctx(e.ctx))
	if maxTokens > 0 && len(tokens) > maxTokens {
		return nil, fmt.Errorf("embedding input too long: %d tokens exceeds context size %d", len(tokens), maxTokens)
	}

	batch := C.llama_batch_init(C.int32_t(n), 0, 1)
	defer C.llama_batch_free(batch)

	for i := 0; i < int(n); i++ {
		C.inkflow_batch_add(&batch, tokens[i], 0, 0, true)
	}

	if ret := C.llama_decode(e.ctx, batch); ret != 0 {
		return nil, fmt.Errorf("decode failed: %d", ret)
	}

	// 5. 【关键修复 2】：使用 llama_get_embeddings_seq 获取池化后的向量
	rawPtr := C.llama_get_embeddings_seq(e.ctx, 0)
	if rawPtr == nil {
		return nil, errors.New("llama_get_embeddings_seq returned nil")
	}

	nEmbd := int(C.llama_model_n_embd(e.model))
	out := make([]float32, nEmbd)
	cSlice := (*[1 << 30]float32)(unsafe.Pointer(rawPtr))[:nEmbd:nEmbd]
	copy(out, cSlice)

	// 5. L2 归一化 (Normalize)
	var sum float64
	for _, v := range out {
		sum += float64(v) * float64(v)
	}
	norm := float32(math.Sqrt(sum))
	if norm > 0 {
		for i := range out {
			out[i] /= norm
		}
	}

	return out, nil
}

// Rerank batches independent query-document sequences into one rank graph.
// This keeps the AVX2 kernels busy and avoids a full graph/scheduler barrier per document.
func (e *LocalEngine) Rerank(query string, documents []string) ([]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.model == nil || e.ctx == nil || e.vocab == nil {
		return nil, errors.New("llamacpp engine is closed")
	}
	if !e.isRerank {
		return nil, errors.New("llamacpp engine is not configured for reranking")
	}
	if len(documents) == 0 {
		return []float32{}, nil
	}

	type rerankSequence struct {
		docIndex int
		tokens   []C.llama_token
	}

	tokenize := func(text string) ([]C.llama_token, error) {
		capacity := len(text) + 16
		tokens := make([]C.llama_token, capacity)
		cText := C.CString(text)
		n := C.llama_tokenize(e.vocab, cText, C.int(len(text)), (*C.llama_token)(unsafe.Pointer(&tokens[0])), C.int(len(tokens)), true, true)
		C.free(unsafe.Pointer(cText))
		if n < 0 {
			tokens = make([]C.llama_token, -int(n))
			cText = C.CString(text)
			n = C.llama_tokenize(e.vocab, cText, C.int(len(text)), (*C.llama_token)(unsafe.Pointer(&tokens[0])), C.int(len(tokens)), true, true)
			C.free(unsafe.Pointer(cText))
		}
		if n < 0 {
			return nil, errors.New("tokenize failed for rerank document")
		}
		tokens = tokens[:int(n)]
		if e.rerankMaxTokens > 1 && len(tokens) > e.rerankMaxTokens {
			// Keep the leading CLS/BOS token and the trailing SEP/EOS token;
			// trim only the middle document content.
			limit := e.rerankMaxTokens
			trimmed := make([]C.llama_token, 0, limit)
			trimmed = append(trimmed, tokens[:limit-1]...)
			trimmed = append(trimmed, tokens[len(tokens)-1])
			tokens = trimmed
		}
		return tokens, nil
	}

	inputs := make([]rerankSequence, len(documents))
	maxContext := int(C.llama_n_ctx(e.ctx))
	for index, document := range documents {
		tokens, err := tokenize(query + " " + document)
		if err != nil {
			return nil, err
		}
		if maxContext > 0 && len(tokens) > maxContext {
			return nil, fmt.Errorf("rerank input %d is %d tokens, exceeds context size %d", index, len(tokens), maxContext)
		}
		inputs[index] = rerankSequence{docIndex: index, tokens: tokens}
	}
	scores := make([]float32, len(documents))
	maxBatchTokens := int(C.llama_n_batch(e.ctx))
	physicalBatchTokens := int(C.llama_n_ubatch(e.ctx))
	if physicalBatchTokens > 0 && (maxBatchTokens <= 0 || physicalBatchTokens < maxBatchTokens) {
		maxBatchTokens = physicalBatchTokens
	}
	if maxBatchTokens <= 0 {
		maxBatchTokens = maxContext
	}
	if maxBatchTokens <= 0 {
		maxBatchTokens = 1
	}
	C.llama_set_embeddings(e.ctx, true)
	C.llama_set_causal_attn(e.ctx, false)
	useLastRankToken := C.inkflow_rerank_uses_last_token(e.model) != C.bool(false)

	batchIndex := 0
	maxSequences := e.rerankMaxSequences
	if maxSequences <= 0 {
		maxSequences = maxRerankBatchSequences
	}
	for offset := 0; offset < len(inputs); {
		batchEnd := offset
		totalTokens := 0
		maxSequenceLength := 0
		for batchEnd < len(inputs) && batchEnd-offset < maxSequences {
			sequenceTokens := len(inputs[batchEnd].tokens)
			if totalTokens > 0 && totalTokens+sequenceTokens > maxBatchTokens {
				break
			}
			if totalTokens == 0 && sequenceTokens > maxBatchTokens {
				return nil, fmt.Errorf("rerank input %d is larger than batch capacity %d", inputs[batchEnd].docIndex, maxBatchTokens)
			}
			totalTokens += sequenceTokens
			if sequenceTokens > maxSequenceLength {
				maxSequenceLength = sequenceTokens
			}
			batchEnd++
		}

		mem := C.llama_get_memory(e.ctx)
		C.llama_memory_clear(mem, true)
		sequenceCount := batchEnd - offset
		batch := C.llama_batch_init(C.int32_t(totalTokens), 0, C.int32_t(sequenceCount))
		for sequenceIndex := offset; sequenceIndex < batchEnd; sequenceIndex++ {
			sequence := inputs[sequenceIndex]
			seqID := C.llama_seq_id(sequenceIndex - offset)
			for position, token := range sequence.tokens {
				// BERT rank pooling uses the CLS token; Qwen3 rank models use
				// the last token. Mark only the row consumed by the rank head.
				outputToken := position == 0
				if useLastRankToken {
					outputToken = position == len(sequence.tokens)-1
				}
				C.inkflow_batch_add(&batch, token, C.llama_pos(position), seqID, C.bool(outputToken))
			}
		}

		encodeStart := time.Now()
		if C.llama_encode(e.ctx, batch) != 0 {
			C.llama_batch_free(batch)
			return nil, errors.New("llama_encode failed during rerank batch")
		}
		if e.onRerankBatch != nil {
			e.onRerankBatch(RerankBatchStats{
				BatchIndex:        batchIndex,
				DocumentCount:     sequenceCount,
				TokenCount:        totalTokens,
				MaxSequenceLength: maxSequenceLength,
				EncodeDuration:    time.Since(encodeStart),
			})
		}

		for sequenceIndex := offset; sequenceIndex < batchEnd; sequenceIndex++ {
			sequence := inputs[sequenceIndex]
			rank := C.llama_get_embeddings_seq(e.ctx, C.llama_seq_id(sequenceIndex-offset))
			if rank == nil {
				C.llama_batch_free(batch)
				return nil, fmt.Errorf("llama_get_embeddings_seq returned nil for rerank sequence %d", sequence.docIndex)
			}
			scores[sequence.docIndex] = float32(*rank)
		}
		C.llama_batch_free(batch)
		offset = batchEnd
		batchIndex++
	}

	return scores, nil
}

// rerankSequentialLegacy is kept as a reference implementation for regression
// checks; production reranking uses the batched graph above.
func (e *LocalEngine) rerankSequentialLegacy(query string, documents []string) ([]float32, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.model == nil || e.ctx == nil || e.vocab == nil {
		return nil, errors.New("llamacpp engine is closed")
	}

	scores := make([]float32, 0, len(documents))

	// 获取分隔符 Token ID (SEP)
	// 对于 BGE/BERT 类模型，通常需要手动拼接： [CLS] query [SEP] doc [SEP]
	// 幸运的是，llama_tokenize 会自动处理特殊 token 模板（如果 GGUF metadata 正确）
	// 但为了保险，我们构建一种通用的拼接方式，或者依赖模型的模板。
	// 简单起见，我们这里依赖 llama_tokenize 处理模板，我们将 query 和 doc 作为两段文本输入
	//
	// 注：更底层的做法是手动插入 token，但不同模型 ID 不同。
	// 这里采用 "Query: <q> Document: <d>" 的通用拼接或直接拼接，具体视模型而定。
	// 对于 bge-reranker，直接拼接 "query" + " " + "document" 通常也能由 tokenizer 处理好 [CLS]/[SEP]。

	for _, doc := range documents {
		// 1. 清空 KV Cache (每次打分都是独立的)
		mem := C.llama_get_memory(e.ctx)
		C.llama_memory_clear(mem, true)

		// 2. 构造 Pair 文本
		// BGE-Reranker 的标准输入格式通常依赖 Tokenizer 自动加特殊符
		// 这里我们简单拼接，让 tokenizer 去加 BOS/EOS/SEP
		// 如果发现效果不好，可能需要手动根据模型类型加特殊字符
		pairText := query + " " + doc
		cPrompt := C.CString(pairText)

		// 3. Tokenize
		// 预估长度
		nTokens := len(pairText) + 16
		tokens := make([]C.llama_token, nTokens)

		n := C.llama_tokenize(
			e.vocab,
			cPrompt,
			C.int(len(pairText)),
			(*C.llama_token)(unsafe.Pointer(&tokens[0])),
			C.int(len(tokens)),
			true, // add_bos (Reranker 通常需要 BOS/CLS 作为起始)
			true, // special
		)
		C.free(unsafe.Pointer(cPrompt)) // 释放 CString

		if n < 0 {
			// 扩容重试
			tokens = make([]C.llama_token, -int(n))
			cPrompt = C.CString(pairText)
			n = C.llama_tokenize(e.vocab, cPrompt, C.int(len(pairText)), (*C.llama_token)(unsafe.Pointer(&tokens[0])), C.int(len(tokens)), true, true)
			C.free(unsafe.Pointer(cPrompt))
		}
		if n < 0 {
			return nil, fmt.Errorf("tokenize failed for doc")
		}
		tokens = tokens[:n]

		// 4. Batch Decode
		batch := C.llama_batch_init(C.int32_t(n), 0, 1)

		for i := 0; i < int(n); i++ {
			// 关键：Reranker 的分数通常输出在最后一个 token 上
			isLast := (i == int(n)-1)
			C.inkflow_batch_add(&batch, tokens[i], C.llama_pos(i), 0, C.bool(isLast))
		}

		if C.llama_decode(e.ctx, batch) != 0 {
			C.llama_batch_free(batch)
			return nil, errors.New("llama_decode failed during rerank")
		}
		C.llama_batch_free(batch)

		// 5. 获取 Logits (分数)
		// llama_get_logits_ith 返回第 i 个 token 的 logits 数组指针
		// 我们取最后一个 token (index = n-1)
		logitsPtr := C.llama_get_logits_ith(e.ctx, C.int32_t(n-1))
		if logitsPtr == nil {
			return nil, errors.New("llama_get_logits_ith returned nil")
		}

		// Reranker 模型（如 BGE）通常在输出层是一个二分类或回归
		// 大多数 GGUF 转换后的 Reranker，logits[0] 就是相关性分数 (Score)
		// 有些模型可能是 logits[1] (Yes) - logits[0] (No)，但 bge-gguf 通常直接用 logits[0]
		score := float32(*logitsPtr)
		scores = append(scores, score)
	}

	return scores, nil
}
