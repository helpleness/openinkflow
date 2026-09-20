// Package mcpclient adapts a remote MCP server to the application's
// provider-neutral tool representation. Connection settings are supplied by
// the persisted system MCP-server configuration, never by application YAML.
package mcpclient

import (
	"InkFlow/internal/ai/llm"
	securehttp "InkFlow/internal/http"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const DefaultTimeout = securehttp.DefaultTimeout

// Config is the validated runtime form of a stored remote MCP-server record.
// BearerToken is deliberately runtime-only and must never be serialized or
// logged by callers.
type Config struct {
	Endpoint    string
	BearerToken string
	Timeout     time.Duration
}

// Client owns one MCP session to a remote server.
type Client struct {
	session *mcp.ClientSession
	timeout time.Duration
}

// Connect completes the MCP initialization handshake over Streamable HTTP.
// The shared HTTP client enforces the network boundary for user-configured
// remote endpoints; this package never starts a local process.
func Connect(ctx context.Context, cfg Config) (*Client, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	httpClient, endpoint, err := securehttp.NewPublicHTTPSClient(cfg.Endpoint, cfg.BearerToken, timeout)
	if err != nil {
		return nil, err
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "llm-mcp-agent", Version: "1.0.0"}, nil)
	transport := &mcp.StreamableClientTransport{
		Endpoint:             endpoint.String(),
		HTTPClient:           httpClient,
		MaxRetries:           -1,
		DisableStandaloneSSE: true,
	}
	connectCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	session, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("connect remote MCP endpoint: %w", err)
	}
	return &Client{session: session, timeout: timeout}, nil
}

// Close ends the MCP session. The shared HTTP connection pool stays available.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	if c.session != nil {
		return c.session.Close()
	}
	return nil
}

// Tools retrieves all MCP tools, including paginated responses, and converts
// their input schemas to the LLM's provider-neutral function definitions.
func (c *Client) Tools(ctx context.Context) ([]llm.ToolDefinition, error) {
	if c == nil || c.session == nil {
		return nil, fmt.Errorf("MCP session is not connected")
	}

	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	var definitions []llm.ToolDefinition
	cursor := ""
	for page := 0; page < 100; page++ {
		result, err := c.session.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return nil, fmt.Errorf("list MCP tools: %w", err)
		}
		for _, tool := range result.Tools {
			if tool == nil {
				continue
			}
			schema, err := schemaMap(tool.InputSchema)
			if err != nil {
				return nil, fmt.Errorf("convert MCP tool %q schema: %w", tool.Name, err)
			}
			definitions = append(definitions, llm.ToolDefinition{
				Name:        tool.Name,
				Description: tool.Description,
				InputSchema: schema,
			})
		}
		if result.NextCursor == "" {
			return definitions, nil
		}
		if result.NextCursor == cursor {
			return nil, fmt.Errorf("list MCP tools: server returned a repeated cursor %q", cursor)
		}
		cursor = result.NextCursor
	}
	return nil, fmt.Errorf("list MCP tools exceeded 100 pages")
}

// Call executes a model-requested tool. The returned string is the complete
// MCP CallToolResult JSON, preserving content, structuredContent and isError
// for the next LLM tool message.
func (c *Client) Call(ctx context.Context, name string, rawArguments json.RawMessage) (string, error) {
	if c == nil || c.session == nil {
		return "", fmt.Errorf("MCP session is not connected")
	}
	ctx, cancel := c.withTimeout(ctx)
	defer cancel()

	arguments, err := objectArguments(rawArguments)
	if err != nil {
		return "", fmt.Errorf("decode arguments for MCP tool %q: %w", name, err)
	}
	result, err := c.session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return "", fmt.Errorf("call MCP tool %q: %w", name, err)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("encode MCP tool %q result: %w", name, err)
	}
	return string(payload), nil
}

func schemaMap(schema any) (map[string]any, error) {
	if schema == nil {
		return map[string]any{"type": "object"}, nil
	}
	payload, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("schema must be a JSON object")
	}
	return result, nil
}

func objectArguments(raw json.RawMessage) (map[string]any, error) {
	if len(raw) == 0 {
		return map[string]any{}, nil
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("arguments must be a JSON object")
	}
	return result, nil
}

func (c *Client) withTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if ctx == nil {
		ctx = context.Background()
	}
	if c.timeout <= 0 {
		return ctx, func() {}
	}
	if _, hasDeadline := ctx.Deadline(); hasDeadline {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, c.timeout)
}
