package system

import (
	"InkFlow/config"
	"InkFlow/global"
	corellm "InkFlow/internal/ai/llm"
	"InkFlow/internal/ai/llm/providers"
	model "InkFlow/model/system"
	request "InkFlow/model/system/request"
	response "InkFlow/model/system/response"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// aiChatStreamEmitter writes one named application event to the HTTP SSE
// response. Keeping the transport callback out of the service makes the
// execution and persistence logic independently testable.
type aiChatStreamEmitter func(event string, payload any)

// StreamMessage runs the same bounded MCP loop as SendMessage, but emits the
// durable user message, prompt-Skill state, MCP results, and assistant deltas
// as they become available. The final assistant response is persisted before
// the API emits its done event.
func (service *AIChatService) StreamMessage(ctx context.Context, tenantID, userID, conversationID uint, input request.AIChatSendMessage, emit aiChatStreamEmitter) (response.AIChatSendResult, error) {
	if emit == nil {
		emit = func(string, any) {}
	}

	content := strings.TrimSpace(input.Content)
	attachmentContent, images, err := parseAIChatAttachments(input.Attachments)
	if err != nil {
		return response.AIChatSendResult{}, err
	}
	if attachmentContent != "" {
		content = strings.TrimSpace(content + "\n\n" + attachmentContent)
	}
	if content == "" && len(images) > 0 {
		content = "请分析我附上的图片。"
	}
	if content == "" {
		return response.AIChatSendResult{}, errors.New("请输入消息或添加图片、文本附件")
	}
	if utf8.RuneCountInString(content) > 12000 {
		return response.AIChatSendResult{}, errors.New("单条消息不能超过 12000 个字符")
	}

	conversation, err := service.findConversation(ctx, tenantID, userID, conversationID)
	if err != nil {
		return response.AIChatSendResult{}, err
	}
	if err := ensureAIChatMember(ctx, tenantID, conversation.OrganizationID, userID); err != nil {
		return response.AIChatSendResult{}, err
	}
	userMessage := model.SysAIChatMessage{TenantID: tenantID, ConversationID: conversation.ID, Role: string(corellm.RoleUser), Content: content}
	if err := global.GVA_DB.WithContext(ctx).Create(&userMessage).Error; err != nil {
		return response.AIChatSendResult{}, err
	}
	emit("user", aiChatMessageView(userMessage))

	if conversation.Title == defaultAIChatTitle {
		conversation.Title = conversationTitle(content)
		if err := global.GVA_DB.WithContext(ctx).Model(&conversation).Update("title", conversation.Title).Error; err != nil {
			return response.AIChatSendResult{}, err
		}
	}

	recentHistory, err := service.listRecentPromptMessages(ctx, tenantID, conversation.ID)
	if err != nil {
		return response.AIChatSendResult{}, err
	}
	skillSelection, err := service.skillsForTurn(ctx, tenantID, userID, input.UseSkills, input.SkillIDs, aiChatSkillRoutingContext(recentHistory))
	if err != nil {
		return response.AIChatSendResult{}, err
	}
	modelConfig, err := (&SysModelSettingService{}).ResolvePrimaryLLM(ctx, tenantID, userID)
	if err != nil {
		return response.AIChatSendResult{}, err
	}
	provider, err := providers.New(modelConfig)
	if err != nil {
		return response.AIChatSendResult{}, err
	}

	tools, toolTargets, closeMCP, warnings := service.availableMCPTools(ctx, tenantID, userID)
	defer closeMCP()
	skillMessage := aiChatSkillMessage(skillSelection)
	if skillMessage != nil {
		emit("skill", aiChatMessageView(*skillMessage))
	}
	messages := service.buildAIChatMessages(ctx, tenantID, conversation.ID, recentHistory, content, skillSelection.Skills, tools)
	attachCurrentAIChatImages(messages, images)
	assistantContent, toolMessages, callWarnings, err := runAIChatWithToolsStream(ctx, provider, modelConfig, messages, tools, toolTargets, emit)
	warnings = append(warnings, callWarnings...)
	if err != nil {
		return response.AIChatSendResult{}, err
	}
	return service.completeAIChatTurn(ctx, tenantID, conversation, userMessage, skillMessage, toolMessages, assistantContent, warnings)
}

func runAIChatWithToolsStream(ctx context.Context, provider corellm.Provider, modelConfig config.LLM, messages []corellm.Message, tools []corellm.ToolDefinition, targets map[string]aiChatToolTarget, emit aiChatStreamEmitter) (string, []model.SysAIChatMessage, []string, error) {
	temperature, maxTokens, topP := 0.3, 4096, modelConfig.TopP
	if topP == 0 {
		topP = 0.9
	}
	request := corellm.ChatRequest{Model: modelConfig.ModelDefault, Temperature: &temperature, MaxTokens: &maxTokens, TopP: &topP, Tools: tools}
	if len(tools) > 0 {
		request.ToolChoice = &corellm.ToolChoice{Mode: corellm.ToolChoiceAuto}
	}

	toolMessages := make([]model.SysAIChatMessage, 0)
	warnings := make([]string, 0)
	for round := 0; round < maxAIChatToolRounds; round++ {
		request.Messages = messages
		assistant, sentContent, err := streamAIChatAssistant(ctx, provider, request, emit)
		if err != nil {
			return "", nil, nil, err
		}
		if len(assistant.ToolCalls) == 0 {
			content := strings.TrimSpace(assistant.Content)
			if content == "" {
				return "", nil, nil, errors.New("模型没有返回可显示的内容")
			}
			return content, toolMessages, warnings, nil
		}

		// Providers normally emit either text or tool calls. If a provider emits
		// both, discard the provisional text before showing the tool transcript;
		// the next round supplies the actual final answer.
		if sentContent {
			emit("assistant_reset", struct{}{})
		}
		messages = append(messages, assistant)
		for _, call := range assistant.ToolCalls {
			target, ok := targets[call.Name]
			toolContent := ""
			if !ok {
				toolContent = marshalAIChatToolError("模型请求了未授权的工具")
				warnings = append(warnings, "模型请求了未授权的 MCP 工具")
			} else {
				resultContent, callErr := target.Client.Call(ctx, target.RemoteName, call.Arguments)
				if callErr != nil {
					toolContent = marshalAIChatToolError(callErr.Error())
					warnings = append(warnings, fmt.Sprintf("MCP 工具“%s”调用失败：%v", target.DisplayName, callErr))
				} else {
					toolContent = truncateRunes(resultContent, maxAIChatToolResultRunes)
				}
			}
			messages = append(messages, corellm.Message{Role: corellm.RoleTool, Content: toolContent, ToolCallID: call.ID})
			toolName := call.Name
			if ok {
				toolName = target.DisplayName
			}
			toolMessage := model.SysAIChatMessage{Role: string(corellm.RoleTool), ToolName: toolName, Content: toolContent}
			toolMessages = append(toolMessages, toolMessage)
			emit("tool", aiChatMessageView(toolMessage))
		}
	}
	return "", nil, nil, fmt.Errorf("MCP 工具调用超过 %d 轮上限", maxAIChatToolRounds)
}

type aiChatStreamingToolCall struct {
	ID        string
	Name      string
	Arguments strings.Builder
}

func streamAIChatAssistant(ctx context.Context, provider corellm.Provider, request corellm.ChatRequest, emit aiChatStreamEmitter) (corellm.Message, bool, error) {
	stream, err := provider.Stream(ctx, request)
	if err != nil {
		return corellm.Message{}, false, err
	}
	defer stream.Close()

	assistant := corellm.Message{Role: corellm.RoleAssistant}
	toolCalls := make(map[int]*aiChatStreamingToolCall)
	sentContent := false
	for {
		event, recvErr := stream.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return corellm.Message{}, false, recvErr
		}
		if event.ContentDelta != "" {
			assistant.Content += event.ContentDelta
			sentContent = true
			emit("delta", response.AIChatStreamDelta{Content: event.ContentDelta})
		}
		if event.ReasoningDelta != "" {
			assistant.ReasoningContent += event.ReasoningDelta
		}
		for _, delta := range event.ToolCalls {
			call := toolCalls[delta.Index]
			if call == nil {
				call = &aiChatStreamingToolCall{}
				toolCalls[delta.Index] = call
			}
			if delta.ID != "" {
				call.ID = delta.ID
			}
			if delta.Name != "" {
				call.Name = delta.Name
			}
			if delta.Arguments != "" {
				call.Arguments.WriteString(delta.Arguments)
			}
		}
	}

	indices := make([]int, 0, len(toolCalls))
	for index := range toolCalls {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		call := toolCalls[index]
		if strings.TrimSpace(call.ID) == "" || strings.TrimSpace(call.Name) == "" {
			return corellm.Message{}, false, errors.New("模型返回了不完整的 MCP 工具调用")
		}
		arguments := strings.TrimSpace(call.Arguments.String())
		if arguments == "" {
			arguments = "{}"
		}
		if !json.Valid([]byte(arguments)) {
			return corellm.Message{}, false, errors.New("模型返回了无效的 MCP 工具参数")
		}
		assistant.ToolCalls = append(assistant.ToolCalls, corellm.ToolCall{ID: call.ID, Name: call.Name, Arguments: json.RawMessage(arguments)})
	}
	return assistant, sentContent, nil
}

func (service *AIChatService) completeAIChatTurn(ctx context.Context, tenantID uint, conversation model.SysAIChatConversation, userMessage model.SysAIChatMessage, skillMessage *model.SysAIChatMessage, toolMessages []model.SysAIChatMessage, assistantContent string, warnings []string) (response.AIChatSendResult, error) {
	assistantMessage := model.SysAIChatMessage{TenantID: tenantID, ConversationID: conversation.ID, Role: string(corellm.RoleAssistant), Content: assistantContent}
	if err := global.GVA_DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if skillMessage != nil {
			skillMessage.TenantID = tenantID
			skillMessage.ConversationID = conversation.ID
			if err := tx.Create(skillMessage).Error; err != nil {
				return err
			}
		}
		for index := range toolMessages {
			toolMessages[index].TenantID = tenantID
			toolMessages[index].ConversationID = conversation.ID
			if err := tx.Create(&toolMessages[index]).Error; err != nil {
				return err
			}
		}
		if err := tx.Create(&assistantMessage).Error; err != nil {
			return err
		}
		return tx.Model(&conversation).Update("updated_at", time.Now()).Error
	}); err != nil {
		return response.AIChatSendResult{}, err
	}
	// Long-term memory is derived only after both sides of this turn have been
	// durably committed. Its index is an enhancement, so an embedding outage
	// must not discard an otherwise successful assistant response.
	if err := service.persistAIChatTurnMemory(ctx, tenantID, conversation.ID, userMessage, assistantMessage); err != nil && global.GVA_LOG != nil {
		global.GVA_LOG.Warn("AI 对话回合语义记忆索引失败，仍可使用词法回忆", zap.Error(err))
	}

	result := response.AIChatSendResult{
		UserMessage:      aiChatMessageView(userMessage),
		AssistantMessage: aiChatMessageView(assistantMessage),
		ToolMessages:     aiChatMessageViews(toolMessages),
		Warnings:         compactWarnings(warnings),
	}
	if skillMessage != nil {
		view := aiChatMessageView(*skillMessage)
		result.SkillMessage = &view
	}
	return result, nil
}
