package officialdoc

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"InkFlow/global"
	model "InkFlow/model/officialdoc"
	request "InkFlow/model/officialdoc/request"
	response "InkFlow/model/officialdoc/response"
	systemService "InkFlow/service/system"
	llmutil "InkFlow/utils/llm"
	"InkFlow/utils/taskrun"
	"InkFlow/utils/toolchain"

	"gorm.io/gorm"
)

const (
	writingStepRetrieveEvidence = "retrieve_evidence"
	writingStepComposeDocument  = "compose_document"
	writingStepCommitVersion    = "commit_version"
	writingStepCompleted        = "completed"
)

// writingRunController only tracks goroutines in this process. The durable
// checkpoint belongs to WritingRun, so a restarted server can resume it.
var writingRunController = taskrun.NewController()

// WritingRunService starts, pauses and resumes durable MCP writing workflows.
// The actual writing operations are exposed as MCP tools below; this service is
// only the lifecycle/state-machine owner and never exposes a delete tool.
type WritingRunService struct{}

func (service *WritingRunService) Start(ctx context.Context, tenantID, taskID, userID uint, input request.WritingRunCreate) (response.WritingRunView, error) {
	task, err := ServiceGroupApp.WritingTaskService.findTaskForMember(ctx, tenantID, taskID, userID)
	if err != nil {
		return response.WritingRunView{}, err
	}
	stage := strings.ToLower(strings.TrimSpace(input.Stage))
	if stage != "outline" && stage != "draft" {
		return response.WritingRunView{}, fmt.Errorf("生成阶段只能是 outline 或 draft")
	}
	limit := input.EvidenceLimit
	if limit <= 0 || limit > 8 {
		limit = 6
	}
	query := strings.TrimSpace(input.EvidenceQuery)
	if query == "" {
		query = task.Requirement
	}
	now := time.Now()
	run := model.WritingRun{
		TaskID: task.ID, TenantID: tenantID, OrganizationID: task.OrganizationID, StartedBy: userID,
		Stage: stage, EvidenceQuery: query, EvidenceLimit: limit, Status: "queued", CurrentStep: writingStepRetrieveEvidence,
		StartedAt: &now,
	}
	if err := global.GVA_DB.WithContext(ctx).Create(&run).Error; err != nil {
		return response.WritingRunView{}, err
	}
	if err := service.appendMessage(ctx, run.ID, 0, "user", "", fmt.Sprintf("请求使用 MCP 受控流程生成%s：%s", stageLabel(stage), task.Requirement)); err != nil {
		return response.WritingRunView{}, err
	}
	if err := global.GVA_DB.WithContext(ctx).Model(task).Update("status", "queued").Error; err != nil {
		return response.WritingRunView{}, err
	}
	service.launch(run.ID)
	return service.view(ctx, run.ID, tenantID, userID, true)
}

func (service *WritingRunService) List(ctx context.Context, tenantID, taskID, userID uint) ([]response.WritingRunView, error) {
	if _, err := ServiceGroupApp.WritingTaskService.findTaskForMember(ctx, tenantID, taskID, userID); err != nil {
		return nil, err
	}
	var runs []model.WritingRun
	if err := global.GVA_DB.WithContext(ctx).Where("task_id = ? AND tenant_id = ?", taskID, tenantID).Order("created_at DESC").Find(&runs).Error; err != nil {
		return nil, err
	}
	items := make([]response.WritingRunView, 0, len(runs))
	for index := range runs {
		if err := service.pauseOrphanedRun(ctx, &runs[index]); err != nil {
			return nil, err
		}
		run := runs[index]
		items = append(items, writingRunView(run))
	}
	return items, nil
}

func (service *WritingRunService) Get(ctx context.Context, tenantID, runID, userID uint) (response.WritingRunView, error) {
	return service.view(ctx, runID, tenantID, userID, true)
}

func (service *WritingRunService) Pause(ctx context.Context, tenantID, runID, userID uint) (response.WritingRunView, error) {
	run, err := service.findRunForMember(ctx, tenantID, runID, userID)
	if err != nil {
		return response.WritingRunView{}, err
	}
	switch run.Status {
	case "completed", "canceled":
		return response.WritingRunView{}, fmt.Errorf("当前运行已结束，不能暂停")
	case "paused":
		return service.view(ctx, runID, tenantID, userID, true)
	}
	if err := global.GVA_DB.WithContext(ctx).Model(run).Updates(map[string]any{"status": "pause_requested"}).Error; err != nil {
		return response.WritingRunView{}, err
	}
	if !writingRunController.Cancel(run.ID) {
		now := time.Now()
		if err := global.GVA_DB.WithContext(ctx).Model(run).Updates(map[string]any{"status": "paused", "paused_at": now}).Error; err != nil {
			return response.WritingRunView{}, err
		}
	}
	_ = service.appendMessage(ctx, run.ID, 0, "system", "", "已请求暂停 MCP 工作流；当前工具完成或取消后会保留检查点。")
	return service.view(ctx, runID, tenantID, userID, true)
}

func (service *WritingRunService) Resume(ctx context.Context, tenantID, runID, userID uint) (response.WritingRunView, error) {
	run, err := service.findRunForMember(ctx, tenantID, runID, userID)
	if err != nil {
		return response.WritingRunView{}, err
	}
	if run.Status == "completed" || run.Status == "canceled" {
		return response.WritingRunView{}, fmt.Errorf("当前运行已结束，不能恢复")
	}
	if run.Status == "running" && writingRunController.Active(run.ID) {
		return response.WritingRunView{}, fmt.Errorf("当前运行仍在执行，无需恢复")
	}
	if err := global.GVA_DB.WithContext(ctx).Model(run).Updates(map[string]any{"status": "queued", "failure_reason": "", "paused_at": nil, "resume_count": run.ResumeCount + 1}).Error; err != nil {
		return response.WritingRunView{}, err
	}
	_ = service.appendMessage(ctx, run.ID, 0, "user", "", "恢复已暂停或中断的 MCP 工作流，继续未完成步骤。")
	service.launch(run.ID)
	return service.view(ctx, runID, tenantID, userID, true)
}

func (service *WritingRunService) launch(runID uint) {
	ctx, cancel := context.WithCancel(context.Background())
	writingRunController.Set(runID, cancel)
	go func() {
		defer writingRunController.Clear(runID)
		defer cancel()
		service.execute(ctx, runID)
	}()
}

func (service *WritingRunService) execute(ctx context.Context, runID uint) {
	for {
		var run model.WritingRun
		if err := global.GVA_DB.WithContext(ctx).First(&run, runID).Error; err != nil {
			return
		}
		if run.Status == "pause_requested" || ctx.Err() != nil {
			service.markPaused(context.Background(), &run)
			return
		}
		if run.Status == "completed" || run.Status == "canceled" {
			return
		}
		if err := global.GVA_DB.WithContext(ctx).Model(&run).Updates(map[string]any{"status": "running", "failure_reason": ""}).Error; err != nil {
			return
		}
		if run.CurrentStep == writingStepCompleted {
			service.markCompleted(context.Background(), &run)
			return
		}
		if err := service.runMCPStage(ctx, &run); err != nil {
			if ctx.Err() != nil || service.pauseRequested(context.Background(), run.ID) {
				service.markPaused(context.Background(), &run)
				return
			}
			service.markFailed(context.Background(), &run, err)
			return
		}
	}
}

func (service *WritingRunService) runMCPStage(ctx context.Context, run *model.WritingRun) error {
	registry, toolName, err := service.registryForStep(run)
	if err != nil {
		return err
	}
	dispatcher, err := toolchain.NewDispatcher(registry, toolchain.DispatchOptions{
		Queue: global.GVA_FairQueue, Pool: global.AntPoolWare, Limiter: global.GVALimiter,
		Cache: global.GVARequestCache, Singleflight: global.GVA_Concurrency_Control,
	})
	if err != nil {
		return err
	}
	llmConfig, err := systemService.ServiceGroupApp.SysModelSettingService.ResolvePrimaryLLM(ctx, run.TenantID, run.StartedBy)
	if err != nil {
		return fmt.Errorf("读取 MCP 编排模型配置失败: %w", err)
	}
	if strings.TrimSpace(llmConfig.BaseUrl) == "" || strings.TrimSpace(llmConfig.ModelDefault) == "" {
		return fmt.Errorf("请先在模型配置中填写 OpenAI 兼容主模型地址和默认模型")
	}
	round := service.nextRound(ctx, run.ID)
	stepDescription := writingStepDescription(run.CurrentStep)
	_ = service.appendMessage(ctx, run.ID, round, "assistant", "", "第 "+strconv.Itoa(round)+" 轮：编排器准备调用 MCP 工具 "+toolName+"，"+stepDescription)
	result, err := toolchain.RunWithTools(ctx, []llmutil.Message{
		{Role: "system", Content: "你是 InkFlow 受控写作工作流的 MCP 编排助手。当前只允许完成一个服务端指定步骤。不得直接撰写正文，不得调用删除、权限或任何未提供的工具；必须立即调用提供的 MCP 工具，参数使用空对象 {}。"},
		{Role: "user", Content: "写作任务正在执行“" + stepDescription + "”。请调用 " + toolName + " 完成此步骤。"},
	}, registry, toolchain.RunOptions{
		UserName: strconv.FormatUint(uint64(run.StartedBy), 10),
		// Dispatcher is the only local execution path. External MCP servers are
		// already adapted into the same Registry by RegisterMCPStdioServer, so
		// local tools do not need a second in-process MCP server wrapper.
		Executor:             dispatcher,
		MaxToolCalls:         1,
		MaxLLMToolCalls:      1,
		MaxMutationToolCalls: 2,
		RequiredTool:         toolName,
		ReturnAfterToolCalls: false,
		SynthesizeAfterTools: false,
		LLM:                  &llmutil.GenerateOptions{Context: ctx, LLM: &llmConfig, Model: llmConfig.ModelDefault, Temperature: 0, MaxTokens: 512},
		OnEvent: func(event string, payload any) {
			switch event {
			case "tool_done":
				if trace, ok := payload.(toolchain.Trace); ok {
					_ = service.appendTrace(context.Background(), run.ID, round, trace)
				}
			case "tool_error":
				if data, ok := payload.(map[string]any); ok {
					trace := toolchain.Trace{ToolName: fmt.Sprint(data["tool_name"]), Kind: toolchain.KindQuery, Status: "error", Error: fmt.Sprint(data["error"]), CreatedAt: time.Now()}
					_ = service.appendTrace(context.Background(), run.ID, round, trace)
				}
			}
		},
	})
	if err == nil && result != nil && strings.TrimSpace(result.Message) != "" {
		_ = service.appendMessage(context.Background(), run.ID, round, "assistant", toolName, result.Message)
	}
	return err
}

func (service *WritingRunService) registryForStep(run *model.WritingRun) (*toolchain.Registry, string, error) {
	registry := toolchain.NewRegistry()
	register := func(name string, kind toolchain.Kind, description string, handler toolchain.Handler) error {
		return registry.Register(toolchain.Tool{
			Name: name, Kind: kind, Description: description,
			Parameters: map[string]any{"type": "object", "properties": map[string]any{}, "additionalProperties": false},
			Handler:    handler, MaxCallsPerRun: 1, TerminalOnSuccess: true, StopOnError: false,
		})
	}
	switch run.CurrentStep {
	case writingStepRetrieveEvidence:
		name := "writing.retrieve_evidence"
		return registry, name, register(name, toolchain.KindMutation, "从当前任务所属组织的知识库检索并冻结写作证据。无需参数；绝不删除文档或索引。", func(ctx context.Context, _ json.RawMessage) (any, error) {
			return service.retrieveEvidence(ctx, run.ID)
		})
	case writingStepComposeDocument:
		name := "writing.compose_document"
		return registry, name, register(name, toolchain.KindLLM, "仅基于已冻结的证据生成当前阶段的中文 Markdown。无需参数；不会提交或覆盖版本。", func(ctx context.Context, _ json.RawMessage) (any, error) {
			return service.composeDocument(ctx, run.ID)
		})
	case writingStepCommitVersion:
		name := "writing.commit_version"
		return registry, name, register(name, toolchain.KindMutation, "将已生成正文和已冻结证据固化为新的不可变版本。无需参数；不会删除任何版本、任务或文档。", func(ctx context.Context, _ json.RawMessage) (any, error) {
			return service.commitVersion(ctx, run.ID)
		})
	default:
		return nil, "", fmt.Errorf("未知 MCP 工作流步骤：%s", run.CurrentStep)
	}
}

func (service *WritingRunService) retrieveEvidence(ctx context.Context, runID uint) (any, error) {
	run, err := service.findRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.CurrentStep != writingStepRetrieveEvidence {
		return nil, fmt.Errorf("当前运行不在证据检索步骤")
	}
	result, err := ServiceGroupApp.KnowledgeSearchService.Search(ctx, run.TenantID, run.OrganizationID, run.StartedBy, run.EvidenceQuery, run.EvidenceLimit)
	if err != nil {
		return nil, fmt.Errorf("检索写作证据失败: %w", err)
	}
	if len(result.Items) == 0 {
		return nil, fmt.Errorf("没有找到可用于受控生成的知识库证据；请先导入并完成索引")
	}
	return result.Items, global.GVA_DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		evidence := make([]model.WritingRunEvidence, 0, len(result.Items))
		for index, item := range result.Items {
			evidence = append(evidence, model.WritingRunEvidence{RunID: run.ID, DocumentID: item.DocumentID, ChunkID: item.ChunkID, Rank: index + 1, Score: item.Score, DocumentName: item.DocumentName, ChunkTitle: item.Title, ContentSnapshot: item.Content})
		}
		if err := tx.Create(&evidence).Error; err != nil {
			return err
		}
		return tx.Model(&model.WritingRun{}).Where("id = ?", run.ID).Update("current_step", writingStepComposeDocument).Error
	})
}

func (service *WritingRunService) composeDocument(ctx context.Context, runID uint) (any, error) {
	run, err := service.findRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.CurrentStep != writingStepComposeDocument {
		return nil, fmt.Errorf("当前运行不在文稿生成步骤")
	}
	task, err := ServiceGroupApp.WritingTaskService.findTaskForMember(ctx, run.TenantID, run.TaskID, run.StartedBy)
	if err != nil {
		return nil, err
	}
	var template model.DocumentTemplate
	if err := global.GVA_DB.WithContext(ctx).Where("id = ? AND tenant_id = ? AND organization_id = ?", task.TemplateID, run.TenantID, run.OrganizationID).First(&template).Error; err != nil {
		return nil, fmt.Errorf("读取任务模板失败: %w", err)
	}
	evidence, err := service.runEvidence(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	if len(evidence) == 0 {
		return nil, fmt.Errorf("当前运行没有已冻结的证据，不能生成文稿")
	}
	llmConfig, err := systemService.ServiceGroupApp.SysModelSettingService.ResolvePrimaryLLM(ctx, run.TenantID, run.StartedBy)
	if err != nil {
		return nil, fmt.Errorf("读取主模型配置失败: %w", err)
	}
	if strings.TrimSpace(llmConfig.BaseUrl) == "" || strings.TrimSpace(llmConfig.ModelDefault) == "" {
		return nil, fmt.Errorf("请先在模型配置中填写 OpenAI 兼容主模型地址和默认模型")
	}
	systemPrompt, userPrompt := controlledWritingPrompt(run.Stage, task, template, evidence)
	content, err := llmutil.GenerateMessages([]llmutil.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: userPrompt},
	}, llmutil.GenerateOptions{
		Context:     ctx,
		LLM:         &llmConfig,
		Model:       llmConfig.ModelDefault,
		Temperature: llmConfig.Temperature,
		MaxTokens:   8192,
	})
	if err != nil {
		return nil, fmt.Errorf("写作模型请求失败: %w", err)
	}
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf("写作模型未返回正文")
	}
	if err := global.GVA_DB.WithContext(ctx).Model(&model.WritingRun{}).Where("id = ?", run.ID).Updates(map[string]any{"generated_body": content, "model_name": llmConfig.ModelDefault, "current_step": writingStepCommitVersion}).Error; err != nil {
		return nil, err
	}
	return map[string]any{"stage": run.Stage, "model": llmConfig.ModelDefault, "content_length": len([]rune(content)), "next_step": writingStepCommitVersion}, nil
}

func (service *WritingRunService) commitVersion(ctx context.Context, runID uint) (any, error) {
	run, err := service.findRun(ctx, runID)
	if err != nil {
		return nil, err
	}
	if run.CurrentStep != writingStepCommitVersion {
		return nil, fmt.Errorf("当前运行不在版本固化步骤")
	}
	if strings.TrimSpace(run.GeneratedBody) == "" {
		return nil, fmt.Errorf("当前运行尚未生成可固化的正文")
	}
	task, err := ServiceGroupApp.WritingTaskService.findTaskForMember(ctx, run.TenantID, run.TaskID, run.StartedBy)
	if err != nil {
		return nil, err
	}
	evidence, err := service.runEvidence(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	version, err := ServiceGroupApp.WritingTaskService.persistVersion(ctx, task, run.StartedBy, run.Stage, run.GeneratedBody, "由 MCP 受控工作流生成", run.ModelName, evidence)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	if err := global.GVA_DB.WithContext(ctx).Model(&model.WritingRun{}).Where("id = ?", run.ID).Updates(map[string]any{"status": "completed", "current_step": writingStepCompleted, "version_id": version.ID, "completed_at": now}).Error; err != nil {
		return nil, err
	}
	_ = service.appendMessage(context.Background(), run.ID, 0, "system", "", "MCP 工作流已完成，版本和证据快照已固化。")
	return map[string]any{"version_id": version.ID, "version": version.Version, "stage": version.Stage, "status": "completed"}, nil
}

func (service *WritingRunService) findRun(ctx context.Context, runID uint) (*model.WritingRun, error) {
	var run model.WritingRun
	if err := global.GVA_DB.WithContext(ctx).First(&run, runID).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (service *WritingRunService) findRunForMember(ctx context.Context, tenantID, runID, userID uint) (*model.WritingRun, error) {
	var run model.WritingRun
	if err := global.GVA_DB.WithContext(ctx).Where("id = ? AND tenant_id = ?", runID, tenantID).First(&run).Error; err != nil {
		return nil, err
	}
	if err := ensureKnowledgeMember(ctx, tenantID, run.OrganizationID, userID); err != nil {
		return nil, err
	}
	return &run, nil
}

func (service *WritingRunService) view(ctx context.Context, runID, tenantID, userID uint, detail bool) (response.WritingRunView, error) {
	run, err := service.findRunForMember(ctx, tenantID, runID, userID)
	if err != nil {
		return response.WritingRunView{}, err
	}
	if err := service.pauseOrphanedRun(ctx, run); err != nil {
		return response.WritingRunView{}, err
	}
	view := writingRunView(*run)
	if !detail {
		return view, nil
	}
	var messages []model.WritingRunMessage
	if err := global.GVA_DB.WithContext(ctx).Where("run_id = ?", run.ID).Order("round, id").Find(&messages).Error; err != nil {
		return response.WritingRunView{}, err
	}
	view.Messages = make([]response.WritingRunMessageView, 0, len(messages))
	for _, message := range messages {
		view.Messages = append(view.Messages, response.WritingRunMessageView{ID: message.ID, Round: message.Round, Role: message.Role, ToolName: message.ToolName, Content: message.Content, CreatedAt: message.CreatedAt})
	}
	var traces []model.WritingRunToolTrace
	if err := global.GVA_DB.WithContext(ctx).Where("run_id = ?", run.ID).Order("round, id").Find(&traces).Error; err != nil {
		return response.WritingRunView{}, err
	}
	view.Traces = make([]response.WritingRunTraceView, 0, len(traces))
	for _, trace := range traces {
		view.Traces = append(view.Traces, response.WritingRunTraceView{ID: trace.ID, Round: trace.Round, ToolName: trace.ToolName, Kind: trace.Kind, Input: trace.Input, OutputSummary: trace.OutputSummary, Status: trace.Status, Error: trace.Error, ElapsedMS: trace.ElapsedMS, CreatedAt: trace.CreatedAt})
	}
	view.Evidence, err = service.runEvidence(ctx, run.ID)
	return view, err
}

// pauseOrphanedRun converts a process-local queued/running run left behind by
// a service restart into a durable paused checkpoint. The next user-triggered
// resume starts at CurrentStep and never replays a completed tool.
func (service *WritingRunService) pauseOrphanedRun(ctx context.Context, run *model.WritingRun) error {
	if run == nil || (run.Status != "queued" && run.Status != "running" && run.Status != "pause_requested") || writingRunController.Active(run.ID) {
		return nil
	}
	now := time.Now()
	if err := global.GVA_DB.WithContext(ctx).Model(run).Updates(map[string]any{"status": "paused", "paused_at": now, "failure_reason": "服务重启或执行器中断；可从检查点恢复"}).Error; err != nil {
		return err
	}
	run.Status = "paused"
	run.PausedAt = &now
	run.FailureReason = "服务重启或执行器中断；可从检查点恢复"
	return service.appendMessage(ctx, run.ID, 0, "system", "", "检测到执行器已中断；MCP 工作流已保存为暂停状态，可从当前检查点恢复。")
}

func (service *WritingRunService) runEvidence(ctx context.Context, runID uint) ([]response.KnowledgeEvidence, error) {
	var records []model.WritingRunEvidence
	if err := global.GVA_DB.WithContext(ctx).Where("run_id = ?", runID).Order("rank").Find(&records).Error; err != nil {
		return nil, err
	}
	items := make([]response.KnowledgeEvidence, 0, len(records))
	for _, record := range records {
		items = append(items, response.KnowledgeEvidence{DocumentID: record.DocumentID, DocumentName: record.DocumentName, ChunkID: record.ChunkID, Title: record.ChunkTitle, Content: record.ContentSnapshot, Score: record.Score})
	}
	return items, nil
}

func (service *WritingRunService) appendMessage(ctx context.Context, runID uint, round int, role, toolName, content string) error {
	return global.GVA_DB.WithContext(ctx).Create(&model.WritingRunMessage{RunID: runID, Round: round, Role: role, ToolName: toolName, Content: strings.TrimSpace(content)}).Error
}

func (service *WritingRunService) appendTrace(ctx context.Context, runID uint, round int, trace toolchain.Trace) error {
	input := strings.TrimSpace(string(trace.Input))
	record := model.WritingRunToolTrace{RunID: runID, Round: round, ToolName: trace.ToolName, Kind: string(trace.Kind), Input: input, OutputSummary: trace.OutputSummary, Status: trace.Status, Error: trace.Error, ElapsedMS: trace.ElapsedMS}
	if err := global.GVA_DB.WithContext(ctx).Create(&record).Error; err != nil {
		return err
	}
	content := trace.OutputSummary
	if trace.Error != "" {
		content = trace.Error
	}
	return service.appendMessage(ctx, runID, round, "tool", trace.ToolName, content)
}

func (service *WritingRunService) nextRound(ctx context.Context, runID uint) int {
	var count int64
	if err := global.GVA_DB.WithContext(ctx).Model(&model.WritingRunMessage{}).Where("run_id = ? AND role = ?", runID, "assistant").Count(&count).Error; err != nil {
		return 1
	}
	return int(count) + 1
}

func (service *WritingRunService) pauseRequested(ctx context.Context, runID uint) bool {
	var run model.WritingRun
	if err := global.GVA_DB.WithContext(ctx).Select("status").First(&run, runID).Error; err != nil {
		return false
	}
	return run.Status == "pause_requested"
}

func (service *WritingRunService) markPaused(ctx context.Context, run *model.WritingRun) {
	now := time.Now()
	_ = global.GVA_DB.WithContext(ctx).Model(&model.WritingRun{}).Where("id = ?", run.ID).Updates(map[string]any{"status": "paused", "paused_at": now}).Error
	_ = service.appendMessage(ctx, run.ID, 0, "system", "", "MCP 工作流已暂停；恢复后会从“"+writingStepDescription(run.CurrentStep)+"”继续。")
}

func (service *WritingRunService) markFailed(ctx context.Context, run *model.WritingRun, cause error) {
	_ = global.GVA_DB.WithContext(ctx).Model(&model.WritingRun{}).Where("id = ?", run.ID).Updates(map[string]any{"status": "failed", "failure_reason": cause.Error()}).Error
	_ = service.appendMessage(ctx, run.ID, 0, "system", "", "MCP 工作流失败："+cause.Error()+"。可在修复配置后恢复。")
}

func (service *WritingRunService) markCompleted(ctx context.Context, run *model.WritingRun) {
	now := time.Now()
	_ = global.GVA_DB.WithContext(ctx).Model(&model.WritingRun{}).Where("id = ?", run.ID).Updates(map[string]any{"status": "completed", "current_step": writingStepCompleted, "completed_at": now}).Error
	_ = service.appendMessage(ctx, run.ID, 0, "system", "", "MCP 工作流已完成，版本和证据快照已固化。")
}

func writingRunView(run model.WritingRun) response.WritingRunView {
	return response.WritingRunView{ID: run.ID, TaskID: run.TaskID, Stage: run.Stage, EvidenceQuery: run.EvidenceQuery, EvidenceLimit: run.EvidenceLimit, Status: run.Status, CurrentStep: run.CurrentStep, FailureReason: run.FailureReason, ModelName: run.ModelName, VersionID: run.VersionID, ResumeCount: run.ResumeCount, StartedAt: run.StartedAt, PausedAt: run.PausedAt, CompletedAt: run.CompletedAt, CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt}
}

func writingStepDescription(step string) string {
	switch step {
	case writingStepRetrieveEvidence:
		return "检索并冻结组织知识证据"
	case writingStepComposeDocument:
		return "基于冻结证据生成受控文稿"
	case writingStepCommitVersion:
		return "固化不可变版本和证据快照"
	default:
		return step
	}
}

func stageLabel(stage string) string {
	if stage == "outline" {
		return "大纲"
	}
	if stage == "draft" {
		return "草稿"
	}
	return stage
}
