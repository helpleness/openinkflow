//go:build windows && cgo

package llamacpp

/*
#cgo CFLAGS: -I${SRCDIR}/../../llama/llama.cpp/include -I${SRCDIR}/../../llama/llama.cpp/ggml/include

#include "llama.h"
#include "ggml-cpu.h"
#include <stdbool.h>
#include <stdlib.h>

static void inkflow_batch_add(struct llama_batch * batch, llama_token id, llama_pos pos, llama_seq_id seq_id, bool logits) {
    batch->token[batch->n_tokens]    = id;
    batch->pos[batch->n_tokens]      = pos;
    batch->n_seq_id[batch->n_tokens] = 1;
    batch->seq_id[batch->n_tokens][0]= seq_id;
    batch->logits[batch->n_tokens]   = logits;
    batch->n_tokens++;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"unsafe"
)

var backendOnce sync.Once
var cpuSIMDOnce sync.Once
var cpuAVX2Available bool

func initBackend() {
	backendOnce.Do(func() {
		C.llama_backend_init()
		C.llama_numa_init(C.GGML_NUMA_STRATEGY_DISABLED)
	})
}

func requireAVX2() error {
	cpuSIMDOnce.Do(func() {
		cpuAVX2Available = C.ggml_cpu_has_avx2() != 0
		if cpuAVX2Available {
			fmt.Println("llama.cpp CPU SIMD: AVX2 (256-bit); rerank/embedding use AVX2 kernels")
		} else {
			fmt.Println("llama.cpp CPU SIMD: AVX2 (256-bit) is unavailable")
		}
	})
	if !cpuAVX2Available {
		return errors.New("llama.cpp local CPU backend requires AVX2 (256-bit) support")
	}
	return nil
}

type LocalEngine struct {
	model              *C.struct_llama_model
	ctx                *C.struct_llama_context
	vocab              *C.struct_llama_vocab
	smpl               *C.struct_llama_sampler
	isRerank           bool
	rerankMaxSequences int
	rerankMaxTokens    int
	onRerankBatch      RerankBatchObserver

	// chat state (used by ChatSession + Chat incremental eval)
	sessionTokens []C.llama_token
	nPast         int

	// current sampler config (for ensureSampler)
	curTopP    float32
	curTemp    float32
	curMinKeep int
	curSeed    uint32

	mu sync.Mutex
}

func newLocalEngine(modelPath string, opt Options) (Engine, error) {
	initBackend()
	if err := requireAVX2(); err != nil {
		return nil, err
	}
	opt.applyDefaults()

	cModelPath := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cModelPath))

	mp := C.llama_model_default_params()
	// The CGo link file is selected at build time (CPU/CUDA/Vulkan). These
	// parameters select whether the compiled backend is actually used at
	// runtime. gpu-layers=0 is deliberately forced to CPU-only mode.
	wantOffload := opt.GPULayers != 0 ||
		(strings.TrimSpace(strings.ToLower(opt.SplitMode)) != "" && strings.TrimSpace(strings.ToLower(opt.SplitMode)) != "none") ||
		len(opt.TensorSplit) > 0
	if !wantOffload {
		mp.n_gpu_layers = 0
		mp.split_mode = C.LLAMA_SPLIT_MODE_NONE
		mp.main_gpu = -1
	} else if !C.llama_supports_gpu_offload() {
		return nil, errors.New("llama.cpp was built without CUDA/Vulkan GPU offload; set gpu-layers to 0 or rebuild with the selected backend")
	} else {
		if opt.GPULayers != 0 {
			mp.n_gpu_layers = C.int32_t(opt.GPULayers)
		}
		switch strings.TrimSpace(strings.ToLower(opt.SplitMode)) {
		case "layer":
			mp.split_mode = C.LLAMA_SPLIT_MODE_LAYER
		case "row":
			mp.split_mode = C.LLAMA_SPLIT_MODE_ROW
		default:
			mp.split_mode = C.LLAMA_SPLIT_MODE_NONE
		}
		if mp.split_mode == C.LLAMA_SPLIT_MODE_NONE && opt.MainGPU != 0 {
			mp.main_gpu = C.int32_t(opt.MainGPU)
		}
		if len(opt.TensorSplit) > 0 && mp.split_mode != C.LLAMA_SPLIT_MODE_NONE {
			n := len(opt.TensorSplit)
			buf := C.malloc(C.size_t(n) * C.size_t(unsafe.Sizeof(C.float(0))))
			if buf != nil {
				defer C.free(buf)
				split := (*[1 << 30]C.float)(buf)[:n:n]
				for i, value := range opt.TensorSplit {
					split[i] = C.float(value)
				}
				mp.tensor_split = (*C.float)(buf)
			}
		}
	}
	model := C.llama_model_load_from_file(cModelPath, mp)
	if model == nil {
		return nil, fmt.Errorf("llama_model_load_from_file failed: %s", modelPath)
	}

	cp := C.llama_context_default_params()
	cp.n_ctx = C.uint32_t(opt.ContextSize)
	cp.n_threads = C.int(opt.Threads)
	cp.n_threads_batch = C.int(opt.ThreadsBatch)
	batchSize, ubatchSize := opt.resolvedBatchSizes()
	cp.n_batch = C.uint32_t(batchSize)
	cp.n_ubatch = C.uint32_t(ubatchSize)
	if opt.DisableFlashAttn {
		cp.flash_attn_type = C.LLAMA_FLASH_ATTN_TYPE_DISABLED
	} else if opt.FlashAttnAuto {
		cp.flash_attn_type = C.LLAMA_FLASH_ATTN_TYPE_AUTO
	}
	cp.embeddings = true
	if opt.IsRerank {
		cp.pooling_type = 4 // LLAMA_POOLING_TYPE_RANK
		cp.n_seq_max = C.uint32_t(opt.RerankMaxSequences)
		// Rerank uses independent encoder sequences and has no KV cache. Keep
		// ContextSize as the per-sequence context instead of dividing it by
		// n_seq_max (8192 / 16 = 512).
		cp.kv_unified = true
	} else if opt.IsEmbedding {
		cp.pooling_type = 1
	}
	ctx := C.llama_init_from_model(model, cp)
	if ctx == nil {
		C.llama_model_free(model)
		return nil, errors.New("llama_init_from_model failed")
	}

	vocab := C.llama_model_get_vocab(model)
	if vocab == nil {
		C.llama_free(ctx)
		C.llama_model_free(model)
		return nil, errors.New("llama_model_get_vocab returned nil")
	}

	// Sampler chain: top_p + temp + dist(seed)
	sp := C.llama_sampler_chain_default_params()
	smpl := C.llama_sampler_chain_init(sp)
	if smpl == nil {
		C.llama_free(ctx)
		C.llama_model_free(model)
		return nil, errors.New("llama_sampler_chain_init failed")
	}
	C.llama_sampler_chain_add(smpl, C.llama_sampler_init_top_p(C.float(opt.TopP), C.size_t(opt.MinKeep)))
	C.llama_sampler_chain_add(smpl, C.llama_sampler_init_temp(C.float(opt.Temperature)))
	C.llama_sampler_chain_add(smpl, C.llama_sampler_init_dist(C.uint32_t(uint32(opt.Seed))))

	return &LocalEngine{
		model:              model,
		ctx:                ctx,
		vocab:              vocab,
		smpl:               smpl,
		isRerank:           opt.IsRerank,
		rerankMaxSequences: opt.RerankMaxSequences,
		rerankMaxTokens:    opt.RerankMaxTokensPerSequence,
		onRerankBatch:      opt.OnRerankBatch,
		curTopP:            opt.TopP,
		curTemp:            opt.Temperature,
		curMinKeep:         opt.MinKeep,
		curSeed:            uint32(opt.Seed),
	}, nil
}

func (e *LocalEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.smpl != nil {
		C.llama_sampler_free(e.smpl)
		e.smpl = nil
	}
	if e.ctx != nil {
		C.llama_free(e.ctx)
		e.ctx = nil
	}
	if e.model != nil {
		C.llama_model_free(e.model)
		e.model = nil
	}
	e.vocab = nil
	return nil
}

// Complete 是一个最小可用的推理封装：输入纯文本 prompt，输出生成文本。
func (e *LocalEngine) Complete(prompt string, opt Options) (string, error) {
	// Complete 保持“无状态”语义：每次都 Reset，再以单轮 user 的 messages 生成。
	if err := e.Reset(); err != nil {
		return "", err
	}
	return e.Chat([]Message{{Role: "user", Content: prompt}}, opt)
}
