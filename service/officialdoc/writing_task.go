package officialdoc

import (
	"context"
	"fmt"
	"strings"

	"InkFlow/global"
	model "InkFlow/model/officialdoc"
	request "InkFlow/model/officialdoc/request"
	response "InkFlow/model/officialdoc/response"

	"gorm.io/gorm"
)

// WritingTaskService implements the explicit outline → draft workflow and keeps
// every generated result immutable in DocumentVersion.
type WritingTaskService struct{}

func (service *WritingTaskService) List(ctx context.Context, tenantID, organizationID, userID uint) ([]response.WritingTaskView, error) {
	if err := ensureKnowledgeMember(ctx, tenantID, organizationID, userID); err != nil {
		return nil, err
	}
	var tasks []model.WritingTask
	if err := global.GVA_DB.WithContext(ctx).Where("tenant_id = ? AND organization_id = ?", tenantID, organizationID).Order("updated_at DESC").Find(&tasks).Error; err != nil {
		return nil, err
	}
	items := make([]response.WritingTaskView, 0, len(tasks))
	for _, task := range tasks {
		items = append(items, taskView(task))
	}
	return items, nil
}

func (service *WritingTaskService) Get(ctx context.Context, tenantID, taskID, userID uint) (response.WritingTaskView, error) {
	db := global.GVA_DB
	var task model.WritingTask
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ?", taskID, tenantID).First(&task).Error; err != nil {
		return response.WritingTaskView{}, err
	}
	if err := ensureKnowledgeMember(ctx, tenantID, task.OrganizationID, userID); err != nil {
		return response.WritingTaskView{}, err
	}
	return service.withVersions(ctx, task)
}

func (service *WritingTaskService) Create(ctx context.Context, tenantID, userID uint, input request.WritingTaskCreate) (response.WritingTaskView, error) {
	if err := ensureKnowledgeMember(ctx, tenantID, input.OrganizationID, userID); err != nil {
		return response.WritingTaskView{}, err
	}
	db := global.GVA_DB
	var template model.DocumentTemplate
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ? AND organization_id = ? AND is_enabled = ?", input.TemplateID, tenantID, input.OrganizationID, true).First(&template).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return response.WritingTaskView{}, fmt.Errorf("模板不存在、已停用或不属于当前组织")
		}
		return response.WritingTaskView{}, err
	}
	title, requirement := strings.TrimSpace(input.Title), strings.TrimSpace(input.Requirement)
	if title == "" || requirement == "" {
		return response.WritingTaskView{}, fmt.Errorf("任务标题和写作要求不能为空")
	}
	task := model.WritingTask{TenantID: tenantID, OrganizationID: input.OrganizationID, TemplateID: template.ID, CreatedBy: userID, Title: title, Requirement: requirement, Constraints: marshalStrings(compactStrings(input.Constraints)), Status: "draft"}
	if err := db.WithContext(ctx).Create(&task).Error; err != nil {
		return response.WritingTaskView{}, err
	}
	return taskView(task), nil
}

func (service *WritingTaskService) SaveVersion(ctx context.Context, tenantID, taskID, userID uint, input request.DocumentVersionCreate) (response.WritingTaskView, error) {
	task, err := service.findTaskForMember(ctx, tenantID, taskID, userID)
	if err != nil {
		return response.WritingTaskView{}, err
	}
	content := strings.TrimSpace(input.Content)
	if content == "" {
		return response.WritingTaskView{}, fmt.Errorf("版本正文不能为空")
	}
	stage := strings.TrimSpace(input.Stage)
	if stage == "" {
		stage = "manual"
	}
	version, err := service.persistVersion(ctx, task, userID, stage, content, "", "", nil)
	if err != nil {
		return response.WritingTaskView{}, err
	}
	task.CurrentVersionID = version.ID
	return service.Get(ctx, tenantID, taskID, userID)
}

func (service *WritingTaskService) findTaskForMember(ctx context.Context, tenantID, taskID, userID uint) (*model.WritingTask, error) {
	var task model.WritingTask
	if err := global.GVA_DB.WithContext(ctx).Where("id = ? AND tenant_id = ?", taskID, tenantID).First(&task).Error; err != nil {
		return nil, err
	}
	if err := ensureKnowledgeMember(ctx, tenantID, task.OrganizationID, userID); err != nil {
		return nil, err
	}
	return &task, nil
}

func (service *WritingTaskService) persistVersion(ctx context.Context, task *model.WritingTask, userID uint, stage, content, prompt, modelName string, evidence []response.KnowledgeEvidence) (*model.DocumentVersion, error) {
	db := global.GVA_DB
	version := &model.DocumentVersion{}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var last model.DocumentVersion
		next := 1
		if err := tx.Where("task_id = ?", task.ID).Order("version DESC").First(&last).Error; err == nil {
			next = last.Version + 1
		} else if err != nil && err != gorm.ErrRecordNotFound {
			return err
		}
		*version = model.DocumentVersion{TaskID: task.ID, TenantID: task.TenantID, OrganizationID: task.OrganizationID, CreatedBy: userID, Version: next, Stage: stage, Content: content, Prompt: prompt, ModelName: modelName}
		if err := tx.Create(version).Error; err != nil {
			return err
		}
		if len(evidence) > 0 {
			records := make([]model.WritingEvidence, 0, len(evidence))
			for rank, item := range evidence {
				records = append(records, model.WritingEvidence{TaskID: task.ID, VersionID: version.ID, DocumentID: item.DocumentID, ChunkID: item.ChunkID, Rank: rank + 1, Score: item.Score, DocumentName: item.DocumentName, ChunkTitle: item.Title, ContentSnapshot: item.Content})
			}
			if err := tx.Create(&records).Error; err != nil {
				return err
			}
		}
		status := "drafted"
		if stage == "outline" {
			status = "outlined"
		}
		return tx.Model(task).Updates(map[string]any{"status": status, "current_version_id": version.ID}).Error
	})
	if err != nil {
		return nil, err
	}
	return version, nil
}

func (service *WritingTaskService) withVersions(ctx context.Context, task model.WritingTask) (response.WritingTaskView, error) {
	view := taskView(task)
	var versions []model.DocumentVersion
	if err := global.GVA_DB.WithContext(ctx).Where("task_id = ?", task.ID).Order("version DESC").Find(&versions).Error; err != nil {
		return response.WritingTaskView{}, err
	}
	view.Versions = make([]response.DocumentVersionView, 0, len(versions))
	for _, version := range versions {
		item := response.DocumentVersionView{ID: version.ID, Version: version.Version, Stage: version.Stage, Content: version.Content, Model: version.ModelName, CreatedAt: version.CreatedAt}
		var evidence []model.WritingEvidence
		if err := global.GVA_DB.WithContext(ctx).Where("version_id = ?", version.ID).Order("rank").Find(&evidence).Error; err != nil {
			return response.WritingTaskView{}, err
		}
		item.Evidence = make([]response.KnowledgeEvidence, 0, len(evidence))
		for _, source := range evidence {
			item.Evidence = append(item.Evidence, response.KnowledgeEvidence{DocumentID: source.DocumentID, DocumentName: source.DocumentName, ChunkID: source.ChunkID, Title: source.ChunkTitle, Content: source.ContentSnapshot, Score: source.Score})
		}
		view.Versions = append(view.Versions, item)
	}
	return view, nil
}

func controlledWritingPrompt(stage string, task *model.WritingTask, template model.DocumentTemplate, evidence []response.KnowledgeEvidence) (string, string) {
	systemPrompt := "你是严谨的中文公文写作助手。只能使用提供的证据中的事实、数值、时间和结论；不确定时明确说明信息不足。不得编造来源。每一处具体事实都要以 [E编号] 紧随标注。输出中文 Markdown，不输出推理过程。"
	var builder strings.Builder
	builder.WriteString("任务标题：" + task.Title + "\n写作要求：" + task.Requirement + "\n")
	builder.WriteString("模板名称：" + template.Name + "\n模板骨架：\n" + template.Body + "\n")
	constraints := append(decodeStrings(template.Constraints), decodeStrings(task.Constraints)...)
	if len(constraints) > 0 {
		builder.WriteString("必须遵守的约束：\n- " + strings.Join(constraints, "\n- ") + "\n")
	}
	if stage == "outline" {
		builder.WriteString("当前阶段：仅生成可执行的文章大纲。每个章节列出写作要点及可支撑它的 [E编号]，不要写成完整正文。\n")
	} else {
		builder.WriteString("当前阶段：生成完整正文。沿用模板的章节逻辑；没有证据支持的段落请删除或标记“待补充”。\n")
	}
	builder.WriteString("可用证据：\n")
	for index, item := range evidence {
		fmt.Fprintf(&builder, "[E%d] 来源《%s》/%s\n%s\n\n", index+1, item.DocumentName, item.Title, item.Content)
	}
	return systemPrompt, builder.String()
}

func taskView(task model.WritingTask) response.WritingTaskView {
	return response.WritingTaskView{ID: task.ID, OrganizationID: task.OrganizationID, TemplateID: task.TemplateID, Title: task.Title, Requirement: task.Requirement, Constraints: decodeStrings(task.Constraints), Status: task.Status, CurrentVersionID: task.CurrentVersionID, CreatedAt: task.CreatedAt, UpdatedAt: task.UpdatedAt}
}

func marshalStrings(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	result := "["
	for index, value := range values {
		if index > 0 {
			result += ","
		}
		result += fmt.Sprintf("%q", value)
	}
	return result + "]"
}
