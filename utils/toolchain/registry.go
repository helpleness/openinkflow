package toolchain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	llmutil "InkFlow/utils/llm"
)

// Kind 标识工具的副作用，编排器据此处理缓存、调用上限和变更后的查询失效。
type Kind string

const (
	// KindQuery 表示只读取数据的工具。编排器会在数据版本未变化且参数相同时复用成功结果。
	KindQuery Kind = "query"
	// KindMutation 表示会修改数据的工具。每次成功调用都会使查询缓存版本递增。
	KindMutation Kind = "mutation"
	// KindLLM 表示会额外消耗模型调用额度的工具，例如模型审核或生成工具。
	KindLLM Kind = "llm"
)

// Handler 是领域工具的具体实现。返回值应可 JSON 序列化，错误会写入工具调用轨迹。
type Handler func(ctx context.Context, args json.RawMessage) (any, error)

// Executor 是工具执行的应用层入口。
//
// Registry 只负责同步调用 Handler；实现 Executor 后可在不污染 Registry 的前提下加入
// 用户级公平排队、协程池、限速、请求合并与结果缓存。userName 应来自已认证身份，空值由
// 具体实现按匿名用户处理。
type Executor interface {
	Execute(ctx context.Context, userName, name string, args json.RawMessage) (any, Trace, error)
}

// TerminalResult 允许受控流程在工具成功后直接结束本轮编排。
// 它适用于已得出确定结论的受控流程，避免模型继续消耗调用额度。
type TerminalResult struct {
	Result  any    `json:"result"`
	Message string `json:"message"`
}

// Tool 描述一个可供编排器与 MCP/LLM 协议调用的领域工具。
type Tool struct {
	// Name 是领域内唯一名称，推荐使用“领域.动作”形式，例如 document.search。
	Name string
	// Description 和 Parameters 会直接提供给模型，应描述能力边界和合法参数。
	Description string
	// Kind 决定编排器如何统计调用次数及复用查询结果；零值按 KindQuery 处理。
	Kind       Kind
	Parameters map[string]any
	// Handler 是实际执行业务逻辑的函数。
	Handler Handler
	// MaxRetries 是该工具遇到可恢复错误时的重试次数配置；零值使用 llmutil 的默认策略。
	MaxRetries int
	// SummaryMaxRunes 是调用轨迹中短摘要的最大字符数；零值为 1200。
	SummaryMaxRunes int
	// ContextMaxRunes 是后续模型轮次可看到的工具结果最大字符数；零值为 4000。
	ContextMaxRunes int
	// StopOnError 为 true 时，调用失败立即终止整轮编排；否则允许模型修正参数后继续。
	StopOnError bool
	// RunOncePerRun 会在查询成功后将其从后续模型轮次的工具列表移除。
	// 写操作结果仍保存在进度账本中；适用于本轮无需重复读取的快照或列表工具。
	RunOncePerRun bool
	// MaxCallsPerRun 限制一个工具在单轮编排内的成功调用次数。零值不限制，
	// 适合防止检索工具无限扩展查询而挤占模型上下文。
	MaxCallsPerRun int
	// TerminalOnSuccess 让受控工作流在该工具成功后立即结束当前编排轮次。
	// 它适用于“检索 → 生成 → 固化版本”这类由服务端状态机决定下一步的流程，
	// 避免模型在已完成当前步骤后继续调用无关工具。
	TerminalOnSuccess bool
}

// Trace 记录一次工具调用的输入、简要输出、状态和耗时，供 SSE 与后续模型轮次使用。
type Trace struct {
	ToolName      string          `json:"tool_name"`
	Kind          Kind            `json:"kind"`
	Input         json.RawMessage `json:"input,omitempty"`
	OutputSummary string          `json:"output_summary"`
	Status        string          `json:"status"`
	Error         string          `json:"error,omitempty"`
	ElapsedMS     int64           `json:"elapsed_ms"`
	CreatedAt     time.Time       `json:"created_at"`
	// outputContext 保存给后续模型轮次使用的较长结果，不直接序列化给 API 客户端。
	outputContext string
	// outputTrimmed 表示完整结果过长，回传模型时只携带 outputContext 摘要。
	outputTrimmed bool
}

// Registry 维护领域名称和 LLM 协议名称的双向映射。
//
// tools 的键是领域名称；llmToolNames 记录“领域名称 → 协议名称”；canonicalNames 记录反向关系。
// Registry 只在应用启动期间注册工具，随后作为只读对象供一次或多次编排调用使用。
type Registry struct {
	tools          map[string]Tool
	llmToolNames   map[string]string
	canonicalNames map[string]string
}

// NewRegistry 创建一个空注册表。工具需在调用 RunWithTools 前完成注册。
func NewRegistry() *Registry {
	return &Registry{
		tools:          map[string]Tool{},
		llmToolNames:   map[string]string{},
		canonicalNames: map[string]string{},
	}
}

// Register 注册一个工具，并拒绝两个领域名称映射为同一个 LLM 协议名称的情况。
func (r *Registry) Register(tool Tool) error {
	tool.Name = strings.TrimSpace(tool.Name)
	if tool.Name == "" {
		return errors.New("tool name is required")
	}
	if tool.Handler == nil {
		return fmt.Errorf("tool %s handler is required", tool.Name)
	}
	if tool.Kind == "" {
		tool.Kind = KindQuery
	}
	if tool.Parameters == nil {
		tool.Parameters = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	llmName := protocolToolName(tool.Name)
	if existing, ok := r.canonicalNames[llmName]; ok && existing != tool.Name {
		return fmt.Errorf("tool names %q and %q map to the same LLM function name %q", existing, tool.Name, llmName)
	}
	r.tools[tool.Name] = tool
	r.llmToolNames[tool.Name] = llmName
	r.canonicalNames[llmName] = tool.Name
	return nil
}

// Get 按领域名称查询已注册工具。
func (r *Registry) Get(name string) (Tool, bool) {
	tool, ok := r.tools[name]
	return tool, ok
}

// List 返回按领域名称排序的工具快照，便于稳定地展示和生成模型工具定义。
func (r *Registry) List() []Tool {
	out := make([]Tool, 0, len(r.tools))
	for _, tool := range r.tools {
		out = append(out, tool)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// LLMTools 返回 OpenAI 兼容接口所需的函数工具定义。
// 返回的函数名是协议安全名称，领域名称转换由 ResolveLLMTool 负责还原。
func (r *Registry) LLMTools() []llmutil.Tool {
	tools := r.List()
	out := make([]llmutil.Tool, 0, len(tools))
	for _, tool := range tools {
		out = append(out, llmutil.Tool{
			Type: "function",
			Function: llmutil.ToolFunction{
				Name:        r.llmToolNames[tool.Name],
				Description: tool.Description,
				Parameters:  tool.Parameters,
			},
		})
	}
	return out
}

// ResolveLLMTool 将模型返回的工具名称还原为 InkFlow 的领域名称。
//
// 模型通常返回经过协议转换的安全名称，例如 document_search；本函数会通过
// canonicalNames 将其还原为 document.search。先直接查找领域名称，是为了兼容
// 能原样返回“领域.动作”名称的模型或本地测试调用。
func (r *Registry) ResolveLLMTool(name string) (string, Tool, bool) {
	if tool, ok := r.tools[name]; ok {
		return name, tool, true
	}
	canonical, ok := r.canonicalNames[name]
	if !ok {
		return "", Tool{}, false
	}
	tool, ok := r.tools[canonical]
	return canonical, tool, ok
}

// protocolToolName 将领域名称转换为仅含字母、数字、下划线和连字符的协议名称。
// 例如 document.search 会转换为 document_search；Register 会拒绝转换后发生碰撞的名称。
func protocolToolName(name string) string {
	var out strings.Builder
	for _, char := range name {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '_' || char == '-' {
			out.WriteRune(char)
		} else {
			out.WriteByte('_')
		}
	}
	if out.Len() == 0 {
		return "tool"
	}
	return out.String()
}

// Call 执行工具，并将 panic 转换为普通错误轨迹。
func (r *Registry) Call(ctx context.Context, name string, args json.RawMessage) (result any, trace Trace, err error) {
	start := time.Now()
	trace = Trace{ToolName: name, Input: args, Status: "ok", CreatedAt: start}
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("工具 %s 执行异常: %v", name, recovered)
			trace.Status = "error"
			trace.Error = err.Error()
			trace.ElapsedMS = time.Since(start).Milliseconds()
			result = nil
		}
	}()
	tool, ok := r.Get(name)
	if !ok {
		err = fmt.Errorf("tool not found: %s", name)
		trace.Status = "error"
		trace.Error = err.Error()
		trace.ElapsedMS = time.Since(start).Milliseconds()
		return nil, trace, err
	}
	trace.Kind = tool.Kind
	result, err = tool.Handler(ctx, args)
	trace.ElapsedMS = time.Since(start).Milliseconds()
	if err != nil {
		trace.Status = "error"
		trace.Error = err.Error()
		return nil, trace, err
	}
	summaryMaxRunes := tool.SummaryMaxRunes
	if summaryMaxRunes <= 0 {
		summaryMaxRunes = 1200
	}
	trace.OutputSummary = Summarize(result, summaryMaxRunes)
	contextMaxRunes := tool.ContextMaxRunes
	if contextMaxRunes <= 0 {
		contextMaxRunes = 4000
	}
	trace.outputContext = Summarize(result, contextMaxRunes)
	if encoded, marshalErr := json.Marshal(result); marshalErr == nil {
		trace.outputTrimmed = len([]rune(strings.TrimSpace(string(encoded)))) > contextMaxRunes
	}
	return result, trace, nil
}

// ResultTrace 根据一个已获得的成功结果重建调用轨迹，不会再次执行工具。
//
// 应用层的结果缓存命中后可使用本方法恢复 OutputSummary 和供后续模型轮次读取的
// outputContext，避免缓存结果只保留较短的序列化字段。name 未注册时会返回错误轨迹。
func (r *Registry) ResultTrace(name string, args json.RawMessage, result any) Trace {
	createdAt := time.Now()
	trace := Trace{ToolName: name, Input: args, Status: "ok", CreatedAt: createdAt}
	tool, ok := r.Get(name)
	if !ok {
		trace.Status = "error"
		trace.Error = fmt.Sprintf("tool not found: %s", name)
		return trace
	}
	trace.Kind = tool.Kind
	summaryMaxRunes := tool.SummaryMaxRunes
	if summaryMaxRunes <= 0 {
		summaryMaxRunes = 1200
	}
	trace.OutputSummary = Summarize(result, summaryMaxRunes)
	contextMaxRunes := tool.ContextMaxRunes
	if contextMaxRunes <= 0 {
		contextMaxRunes = 4000
	}
	trace.outputContext = Summarize(result, contextMaxRunes)
	if encoded, marshalErr := json.Marshal(result); marshalErr == nil {
		trace.outputTrimmed = len([]rune(strings.TrimSpace(string(encoded)))) > contextMaxRunes
	}
	return trace
}

// Summarize 将工具结果序列化为 JSON 并按 Unicode 字符数截断。
// 序列化失败时退化为 fmt.Sprint，确保工具轨迹仍可用于诊断。
func Summarize(v any, maxRunes int) string {
	if v == nil {
		return ""
	}
	b, err := json.Marshal(v)
	if err != nil {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	s := strings.TrimSpace(string(b))
	if maxRunes <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "...(truncated)"
}
