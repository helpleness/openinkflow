package toolchain

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mcpclient "github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"
)

// MCPStdioServer 描述一个通过标准输入输出通信的 MCP 服务进程。
type MCPStdioServer struct {
	// Name 是注册到 Registry 时使用的命名空间前缀；空值使用 mcp。
	Name string
	// Command 是 MCP 服务的可执行命令，例如 npx 或本地二进制文件路径。
	Command string
	// Args 会按顺序传给 Command。
	Args []string
	// Env 是追加给子进程的环境变量，格式为 KEY=VALUE。
	// mcp-go 会在其前面保留父进程环境，因此无需重复传入 PATH 等系统变量。
	Env []string
}

// MCPToolInfo 是供前端或诊断页展示的 MCP 工具元数据。
type MCPToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

// ListMCPStdioTools 连接 MCP 服务并返回其工具定义，不会修改当前注册表。
func ListMCPStdioTools(ctx context.Context, server MCPStdioServer) ([]MCPToolInfo, error) {
	server.Name = strings.TrimSpace(server.Name)
	if server.Name == "" {
		server.Name = "mcp"
	}
	// newInitializedMCPClient 内部调用 mcpclient.NewStdioMCPClient：该函数会立即启动
	// Command 子进程，并建立“子进程 stdin/stdout ↔ JSON-RPC”的传输层；随后完成初始化握手。
	client, err := newInitializedMCPClient(ctx, server)
	if err != nil {
		return nil, err
	}
	// Client.Close 会关闭 stdin、终止未完成请求并等待 MCP 子进程退出，避免工具列表预览遗留进程。
	defer client.Close()

	// mcpclient.Client.ListTools 会发送 tools/list 请求；当服务端返回 nextCursor 时，
	// mcp-go 会自动继续请求后续页并合并结果，所以这里拿到的是完整工具列表。
	tools, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		return nil, err
	}
	out := make([]MCPToolInfo, 0, len(tools.Tools))
	for _, tool := range tools.Tools {
		out = append(out, MCPToolInfo{
			Name:        server.Name + "." + tool.Name,
			Description: tool.Description,
			Parameters:  mcpToolSchema(tool),
		})
	}
	return out, nil
}

// RegisterMCPStdioServer 发现 MCP 工具并将其适配为 Registry 工具。
//
// MCP 的工具定义没有统一的副作用标记，因此适配后的工具默认按 KindQuery 管理；对可能产生
// 写入的 MCP 工具，应在服务接入层额外限制其可用范围。每次实际调用都会新建客户端，使连接、
// 上下文和错误相互隔离，代价是单次调用需要完成一次 MCP 初始化握手。
func RegisterMCPStdioServer(ctx context.Context, registry *Registry, server MCPStdioServer) error {
	if registry == nil {
		return fmt.Errorf("tool registry is nil")
	}
	server.Name = strings.TrimSpace(server.Name)
	if server.Name == "" {
		server.Name = "mcp"
	}
	if strings.TrimSpace(server.Command) == "" {
		return fmt.Errorf("mcp command is required")
	}
	// 注册阶段只需短暂连接：初始化后调用 ListTools 获取服务端当前公开的工具清单。
	client, err := newInitializedMCPClient(ctx, server)
	if err != nil {
		return err
	}
	tools, err := client.ListTools(ctx, mcp.ListToolsRequest{})
	if err != nil {
		_ = client.Close()
		return err
	}
	// 工具元数据已复制到本地 Registry，发现连接不再需要，立即释放对应子进程。
	_ = client.Close()
	for _, mcpTool := range tools.Tools {
		tool := mcpTool
		safeName := server.Name + "." + tool.Name
		if err := registry.Register(Tool{
			Name:        safeName,
			Kind:        KindQuery,
			Description: "[MCP:" + server.Name + "] " + tool.Description,
			Parameters:  mcpToolSchema(tool),
			Handler: func(ctx context.Context, raw json.RawMessage) (any, error) {
				var args map[string]any
				// Registry 传入的是已经通过模型 schema 生成的 JSON。这里还原成通用 map，
				// 以匹配 mcp.CallToolParams.Arguments 的任意 JSON 值类型。
				if len(raw) > 0 {
					if err := json.Unmarshal(raw, &args); err != nil {
						return nil, err
					}
				}
				// 不复用注册阶段客户端：每次 Handler 调用都对应独立的 MCP 子进程、
				// JSON-RPC 请求序列和 context 取消范围，避免不同用户请求相互影响。
				callClient, err := newInitializedMCPClient(ctx, server)
				if err != nil {
					return nil, err
				}
				defer callClient.Close()
				// Client.CallTool 固定发送 MCP 的 tools/call JSON-RPC 方法，并等待响应。
				// Params.Name 必须使用 MCP 服务原始工具名（不能使用 InkFlow 的 server.tool 名）；
				// Params.Arguments 则是服务端 inputSchema 所定义的参数对象。
				result, err := callClient.CallTool(ctx, mcp.CallToolRequest{
					// CallTool 当前依据 Params 构造请求；这里保留 Method 是为了使请求结构
					// 与 MCP 协议语义一致，便于未来切换到直接传输层调用时阅读。
					Request: mcp.Request{Method: string(mcp.MethodToolsCall)},
					Params:  mcp.CallToolParams{Name: tool.Name, Arguments: args},
				})
				if err != nil {
					return nil, err
				}
				// mcp-go 将 JSON-RPC/传输失败作为 error 返回；工具自身的可恢复业务错误
				// 则按 MCP 规范放在成功响应的 result.IsError 中，必须透传给模型以便修正参数。
				// StructuredContent 是服务端提供的机器可读结果，Content 保存文本、图片、资源等内容块。
				return map[string]any{
					"is_error":           result.IsError,
					"structured_content": result.StructuredContent,
					"content":            mcpContentText(result.Content),
				}, nil
			},
		}); err != nil {
			_ = client.Close()
			return err
		}
	}
	return nil
}

// newInitializedMCPClient 创建 stdio 客户端并完成 MCP 初始化握手。
// 调用方获得客户端后负责 Close；初始化失败时本函数会自行关闭已创建的客户端。
func newInitializedMCPClient(ctx context.Context, server MCPStdioServer) (*mcpclient.Client, error) {
	if strings.TrimSpace(server.Command) == "" {
		return nil, fmt.Errorf("mcp command is required")
	}
	// NewStdioMCPClient 使用 os/exec 启动服务进程，打开其 stdin/stdout/stderr，并在后台
	// 读取 stdout 中的 JSON-RPC 响应与通知。它已自动启动传输层，但尚不能调用 ListTools、
	// CallTool 等请求方法，必须先 Initialize。
	client, err := mcpclient.NewStdioMCPClient(server.Command, server.Env, server.Args...)
	if err != nil {
		return nil, err
	}
	// Client.Initialize 发送 initialize 请求、校验服务端协商的协议版本、记录服务端能力，
	// 并自动发送 notifications/initialized；成功后 mcp-go 才允许后续请求。
	// LATEST_PROTOCOL_VERSION 表示 InkFlow 以当前依赖支持的最新 MCP 版本发起协商，
	// ClientInfo 用于让服务端日志或诊断页识别调用方；空 Capabilities 表示本客户端不声明
	// roots、sampling、elicitation 等额外双向能力。
	if _, err := client.Initialize(ctx, mcp.InitializeRequest{
		Request: mcp.Request{Method: string(mcp.MethodInitialize)},
		Params: mcp.InitializeParams{
			ProtocolVersion: mcp.LATEST_PROTOCOL_VERSION,
			ClientInfo:      mcp.Implementation{Name: "InkFlow", Version: "0.1.0"},
			Capabilities:    mcp.ClientCapabilities{},
		},
	}); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

// mcpToolSchema 将 MCP 工具的输入 schema 统一转换为普通 map，供 Registry 和 LLM 使用。
//
// mcp.Tool.InputSchema 是 mcp-go 提供的强类型 schema；RawInputSchema 则允许服务端保留
// 任意合法 JSON Schema。优先使用后者可完整传递不在强类型结构中的约束，例如 oneOf 或自定义字段。
func mcpToolSchema(tool mcp.Tool) map[string]any {
	schema := map[string]any{"type": "object", "properties": map[string]any{}}
	if len(tool.RawInputSchema) > 0 {
		_ = json.Unmarshal(tool.RawInputSchema, &schema)
		return schema
	}
	if b, err := json.Marshal(tool.InputSchema); err == nil {
		_ = json.Unmarshal(b, &schema)
	}
	return schema
}

// mcpContentText 将 MCP 返回内容转换为可 JSON 序列化的字符串列表。
//
// mcp.AsTextContent 是 mcp-go 对 Content 接口的安全类型断言，成功时返回 TextContent，
// 因而文本无需再次编码。非文本内容（图片、音频、嵌入资源等）通过 mcp.MarshalContent
// 序列化为其 MCP JSON 形式，以免在适配层丢失类型和元数据。
func mcpContentText(contents []mcp.Content) []string {
	out := make([]string, 0, len(contents))
	for _, content := range contents {
		if text, ok := mcp.AsTextContent(content); ok {
			out = append(out, text.Text)
			continue
		}
		if b, err := mcp.MarshalContent(content); err == nil {
			out = append(out, string(b))
		}
	}
	return out
}
