//go:build litertlm && cgo && (windows || linux || darwin)

package litertlm

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/LiteRT-LM
#cgo windows LDFLAGS: -L${SRCDIR}/../../third_party/LiteRT-LM/bazel-bin/c -l:engine_cpu_dll.if.lib

#include "c/engine.h"
#include <stdlib.h>
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"unsafe"
)

type LocalEngine struct {
	engine       *C.LiteRtLmEngine
	conversation *C.LiteRtLmConversation
	history      []Message
	lastConfig   string

	mu sync.Mutex
}

func newLocalEngine(modelPath string, opt Options) (Engine, error) {
	opt.applyDefaults()

	cModelPath := C.CString(modelPath)
	defer C.free(unsafe.Pointer(cModelPath))

	cBackend := C.CString(opt.Backend)
	defer C.free(unsafe.Pointer(cBackend))

	var cVisionBackend *C.char
	if opt.VisionBackend != "" {
		cVisionBackend = C.CString(opt.VisionBackend)
		defer C.free(unsafe.Pointer(cVisionBackend))
	}

	var cAudioBackend *C.char
	if opt.AudioBackend != "" {
		cAudioBackend = C.CString(opt.AudioBackend)
		defer C.free(unsafe.Pointer(cAudioBackend))
	}

	settings := C.litert_lm_engine_settings_create(
		cModelPath,
		cBackend,
		cVisionBackend,
		cAudioBackend,
	)
	if settings == nil {
		return nil, fmt.Errorf("litertlm: failed to create engine settings for %s", modelPath)
	}
	defer C.litert_lm_engine_settings_delete(settings)

	if opt.MaxNumTokens > 0 {
		C.litert_lm_engine_settings_set_max_num_tokens(settings, C.int(opt.MaxNumTokens))
	}
	if opt.CacheDir != "" {
		cCacheDir := C.CString(opt.CacheDir)
		C.litert_lm_engine_settings_set_cache_dir(settings, cCacheDir)
		C.free(unsafe.Pointer(cCacheDir))
	}
	if opt.ActivationDataType != 0 {
		C.litert_lm_engine_settings_set_activation_data_type(settings, C.int(opt.ActivationDataType))
	}
	if opt.PrefillChunkSize > 0 {
		C.litert_lm_engine_settings_set_prefill_chunk_size(settings, C.int(opt.PrefillChunkSize))
	}
	if opt.EnableBenchmark {
		C.litert_lm_engine_settings_enable_benchmark(settings)
	}
	if opt.BenchmarkPrefillTokens > 0 {
		C.litert_lm_engine_settings_set_num_prefill_tokens(settings, C.int(opt.BenchmarkPrefillTokens))
	}
	if opt.BenchmarkDecodeTokens > 0 {
		C.litert_lm_engine_settings_set_num_decode_tokens(settings, C.int(opt.BenchmarkDecodeTokens))
	}

	engine := C.litert_lm_engine_create(settings)
	if engine == nil {
		return nil, errors.New("litertlm: failed to create engine")
	}

	return &LocalEngine{engine: engine}, nil
}

func (e *LocalEngine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.resetConversationLocked()
	if e.engine != nil {
		C.litert_lm_engine_delete(e.engine)
		e.engine = nil
	}
	return nil
}

func (e *LocalEngine) Reset() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.engine == nil {
		return errors.New("litertlm engine is closed")
	}
	e.resetConversationLocked()
	return nil
}

func (e *LocalEngine) Complete(prompt string, opt Options) (string, error) {
	if err := e.Reset(); err != nil {
		return "", err
	}
	return e.Chat([]Message{{Role: "user", Content: prompt}}, opt)
}

func (e *LocalEngine) Embedding(text string) ([]float32, error) {
	return nil, errors.New("litertlm local backend: embedding is not exposed by the current wrapper")
}

func (e *LocalEngine) Rerank(query string, documents []string) ([]float32, error) {
	return nil, errors.New("litertlm local backend: rerank is not exposed by the current wrapper")
}

func (e *LocalEngine) Chat(messages []Message, opt Options) (string, error) {
	opt.applyDefaults()

	e.mu.Lock()
	defer e.mu.Unlock()

	if e.engine == nil {
		return "", errors.New("litertlm engine is closed")
	}

	normalized := normalizeMessages(messages, opt)
	if len(normalized) == 0 {
		return "", errors.New("litertlm: empty messages")
	}

	configKey, err := buildConfigKey(opt)
	if err != nil {
		return "", err
	}

	if e.canContinueLocked(normalized, configKey) {
		return e.sendOnConversationLocked(normalized[len(normalized)-1], opt)
	}

	if err := e.rebuildConversationLocked(normalized, opt, configKey); err != nil {
		return "", err
	}
	return e.sendOnConversationLocked(normalized[len(normalized)-1], opt)
}

func (e *LocalEngine) canContinueLocked(messages []Message, configKey string) bool {
	if e.conversation == nil || e.lastConfig != configKey {
		return false
	}
	if len(messages) != len(e.history)+1 {
		return false
	}
	for i := range e.history {
		if !reflect.DeepEqual(e.history[i], messages[i]) {
			return false
		}
	}
	return true
}

func (e *LocalEngine) rebuildConversationLocked(messages []Message, opt Options, configKey string) error {
	e.resetConversationLocked()

	systemJSON, prefaceMessages, _, err := splitConversationInputs(messages)
	if err != nil {
		return err
	}

	sessionConfig := C.litert_lm_session_config_create()
	if sessionConfig == nil {
		return errors.New("litertlm: failed to create session config")
	}
	defer C.litert_lm_session_config_delete(sessionConfig)

	C.litert_lm_session_config_set_max_output_tokens(sessionConfig, C.int(opt.MaxTokens))
	sampler := C.LiteRtLmSamplerParams{
		toCSamplerType(opt.SamplerType),
		C.int32_t(opt.TopK),
		C.float(opt.TopP),
		C.float(opt.Temperature),
		C.int32_t(opt.Seed),
	}
	C.litert_lm_session_config_set_sampler_params(sessionConfig, &sampler)

	var cSystemJSON *C.char
	if systemJSON != "" {
		cSystemJSON = C.CString(systemJSON)
		defer C.free(unsafe.Pointer(cSystemJSON))
	}

	var cToolsJSON *C.char
	if opt.ToolsJSON != "" {
		cToolsJSON = C.CString(opt.ToolsJSON)
		defer C.free(unsafe.Pointer(cToolsJSON))
	}

	var cMessagesJSON *C.char
	if len(prefaceMessages) > 0 {
		raw, err := json.Marshal(prefaceMessages)
		if err != nil {
			return fmt.Errorf("litertlm: marshal preface messages: %w", err)
		}
		cMessagesJSON = C.CString(string(raw))
		defer C.free(unsafe.Pointer(cMessagesJSON))
	}

	config := C.litert_lm_conversation_config_create()
	if config == nil {
		return errors.New("litertlm: failed to create conversation config")
	}
	defer C.litert_lm_conversation_config_delete(config)

	C.litert_lm_conversation_config_set_session_config(config, sessionConfig)
	if cSystemJSON != nil {
		C.litert_lm_conversation_config_set_system_message(config, cSystemJSON)
	}
	if cToolsJSON != nil {
		C.litert_lm_conversation_config_set_tools(config, cToolsJSON)
	}
	if cMessagesJSON != nil {
		C.litert_lm_conversation_config_set_messages(config, cMessagesJSON)
	}
	C.litert_lm_conversation_config_set_enable_constrained_decoding(
		config,
		C.bool(opt.EnableConstrainedDecoding),
	)

	conversation := C.litert_lm_conversation_create(e.engine, config)
	if conversation == nil {
		return errors.New("litertlm: failed to create conversation")
	}

	e.conversation = conversation
	e.history = append([]Message(nil), messages[:len(messages)-1]...)
	e.lastConfig = configKey
	return nil
}

func (e *LocalEngine) sendOnConversationLocked(message Message, opt Options) (string, error) {
	if e.conversation == nil {
		return "", errors.New("litertlm: conversation is not initialized")
	}

	if strings.EqualFold(strings.TrimSpace(message.Role), "assistant") ||
		strings.EqualFold(strings.TrimSpace(message.Role), "model") {
		return "", errors.New("litertlm: last message must be a prompt to the model, not an assistant/model message")
	}

	messageRaw, err := json.Marshal(message)
	if err != nil {
		return "", fmt.Errorf("litertlm: marshal message: %w", err)
	}

	cMessage := C.CString(string(messageRaw))
	defer C.free(unsafe.Pointer(cMessage))

	var cExtraContext *C.char
	if opt.ExtraContextJSON != "" {
		cExtraContext = C.CString(opt.ExtraContextJSON)
		defer C.free(unsafe.Pointer(cExtraContext))
	}

	var optionalArgs *C.LiteRtLmConversationOptionalArgs
	if opt.VisualTokenBudget > 0 {
		optionalArgs = C.litert_lm_conversation_optional_args_create()
		if optionalArgs == nil {
			return "", errors.New("litertlm: failed to create conversation optional args")
		}
		defer C.litert_lm_conversation_optional_args_delete(optionalArgs)
		C.litert_lm_conversation_optional_args_set_visual_token_budget(
			optionalArgs,
			C.int(opt.VisualTokenBudget),
		)
	}

	resp := C.litert_lm_conversation_send_message(
		e.conversation,
		cMessage,
		cExtraContext,
		optionalArgs,
	)
	if resp == nil {
		return "", errors.New("litertlm: send_message returned nil")
	}
	defer C.litert_lm_json_response_delete(resp)

	raw := C.litert_lm_json_response_get_string(resp)
	if raw == nil {
		return "", errors.New("litertlm: empty json response")
	}

	assistantText, err := extractAssistantText(C.GoString(raw))
	if err != nil {
		return "", err
	}

	e.history = append(e.history, message, Message{Role: "assistant", Content: assistantText})
	return assistantText, nil
}

func (e *LocalEngine) resetConversationLocked() {
	if e.conversation != nil {
		C.litert_lm_conversation_delete(e.conversation)
		e.conversation = nil
	}
	e.history = nil
	e.lastConfig = ""
}

func normalizeMessages(messages []Message, opt Options) []Message {
	out := make([]Message, 0, len(messages)+1)
	out = append(out, messages...)

	if len(out) == 0 || !strings.EqualFold(strings.TrimSpace(out[0].Role), "system") {
		if strings.TrimSpace(opt.SystemPrompt) != "" {
			out = append([]Message{{Role: "system", Content: opt.SystemPrompt}}, out...)
		}
	}
	return out
}

func splitConversationInputs(messages []Message) (systemJSON string, preface []Message, last Message, err error) {
	if len(messages) == 0 {
		return "", nil, Message{}, errors.New("litertlm: no messages to send")
	}

	work := make([]Message, len(messages))
	copy(work, messages)

	if strings.EqualFold(strings.TrimSpace(work[0].Role), "system") {
		raw, marshalErr := json.Marshal(work[0].Content)
		if marshalErr != nil {
			return "", nil, Message{}, fmt.Errorf("litertlm: marshal system prompt: %w", marshalErr)
		}
		systemJSON = string(raw)
		work = work[1:]
	}

	if len(work) == 0 {
		return "", nil, Message{}, errors.New("litertlm: missing final prompt message")
	}

	last = work[len(work)-1]
	preface = append([]Message(nil), work[:len(work)-1]...)
	return systemJSON, preface, last, nil
}

func buildConfigKey(opt Options) (string, error) {
	key := struct {
		MaxTokens                 int
		SamplerType               string
		TopK                      int
		TopP                      float32
		Temperature               float32
		Seed                      int32
		SystemPrompt              string
		ToolsJSON                 string
		EnableConstrainedDecoding bool
	}{
		MaxTokens:                 opt.MaxTokens,
		SamplerType:               opt.SamplerType,
		TopK:                      opt.TopK,
		TopP:                      opt.TopP,
		Temperature:               opt.Temperature,
		Seed:                      opt.Seed,
		SystemPrompt:              opt.SystemPrompt,
		ToolsJSON:                 opt.ToolsJSON,
		EnableConstrainedDecoding: opt.EnableConstrainedDecoding,
	}
	raw, err := json.Marshal(key)
	if err != nil {
		return "", fmt.Errorf("litertlm: build config key: %w", err)
	}
	return string(raw), nil
}

func extractAssistantText(raw string) (string, error) {
	var msg struct {
		Role    string `json:"role"`
		Content any    `json:"content"`
	}
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		return "", fmt.Errorf("litertlm: unmarshal response: %w", err)
	}

	text := strings.TrimSpace(extractTextFromContent(msg.Content))
	if text != "" {
		return text, nil
	}

	if msg.Content == nil {
		return "", nil
	}

	fallback, err := json.Marshal(msg.Content)
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(string(fallback)), nil
}

func extractTextFromContent(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []any:
		var parts []string
		for _, item := range v {
			if s := extractTextFromContent(item); strings.TrimSpace(s) != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, "")
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return text
		}
		if inner, ok := v["content"]; ok {
			return extractTextFromContent(inner)
		}
	case nil:
		return ""
	}
	return ""
}

func toCSamplerType(kind string) C.LiteRtLmSamplerType {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "top_k", "topk":
		return C.kLiteRtLmSamplerTypeTopK
	case "greedy":
		return C.kLiteRtLmSamplerTypeGreedy
	default:
		return C.kLiteRtLmSamplerTypeTopP
	}
}
