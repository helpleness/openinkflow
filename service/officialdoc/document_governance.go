package officialdoc

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"InkFlow/global"
	model "InkFlow/model/officialdoc"
	request "InkFlow/model/officialdoc/request"
	response "InkFlow/model/officialdoc/response"
	"InkFlow/utils/documentdiff"
	"InkFlow/utils/documentquality"

	"gorm.io/gorm"
)

// DocumentGovernanceService owns review records and deterministic compliance
// checks for immutable writing versions.
type DocumentGovernanceService struct{}

func (service *DocumentGovernanceService) Diff(ctx context.Context, tenantID, taskID, versionID, baseVersionID, userID uint) (response.DocumentVersionDiff, error) {
	task, err := (&WritingTaskService{}).findTaskForMember(ctx, tenantID, taskID, userID)
	if err != nil {
		return response.DocumentVersionDiff{}, err
	}
	target, err := findTaskVersion(ctx, task, versionID)
	if err != nil {
		return response.DocumentVersionDiff{}, err
	}
	if baseVersionID == 0 {
		var previous model.DocumentVersion
		err = global.GVA_DB.WithContext(ctx).Where("task_id = ? AND version < ?", task.ID, target.Version).Order("version DESC").First(&previous).Error
		if err == nil {
			baseVersionID = previous.ID
		} else if err == gorm.ErrRecordNotFound {
			return response.DocumentVersionDiff{BaseVersionID: 0, TargetVersionID: target.ID, Segments: convertDiff(documentdiff.Compare("", target.Content))}, nil
		} else {
			return response.DocumentVersionDiff{}, err
		}
	}
	base, err := findTaskVersion(ctx, task, baseVersionID)
	if err != nil {
		return response.DocumentVersionDiff{}, err
	}
	return response.DocumentVersionDiff{BaseVersionID: base.ID, TargetVersionID: target.ID, Segments: convertDiff(documentdiff.Compare(base.Content, target.Content))}, nil
}

func (service *DocumentGovernanceService) Validate(ctx context.Context, tenantID, taskID, versionID, userID uint) (response.DocumentValidationResult, error) {
	task, err := (&WritingTaskService{}).findTaskForMember(ctx, tenantID, taskID, userID)
	if err != nil {
		return response.DocumentValidationResult{}, err
	}
	version, err := findTaskVersion(ctx, task, versionID)
	if err != nil {
		return response.DocumentValidationResult{}, err
	}
	var template model.DocumentTemplate
	if err := global.GVA_DB.WithContext(ctx).Where("id = ? AND tenant_id = ? AND organization_id = ?", task.TemplateID, tenantID, task.OrganizationID).First(&template).Error; err != nil {
		return response.DocumentValidationResult{}, err
	}
	// Template constraints are intentionally treated as organization-level terms
	// only when prefixed with “敏感词:”, keeping free-form generation guidance
	// out of deterministic sensitive-data matching.
	terms := sensitiveTerms(append(decodeStrings(template.Constraints), decodeStrings(task.Constraints)...))
	findings := documentquality.Validate(version.Content, terms)
	findings = append(findings, documentquality.ValidateTemplateStructure(version.Content, template.Body)...)
	result := response.DocumentValidationResult{VersionID: version.ID, Passed: true, Findings: make([]response.DocumentValidationFinding, 0, len(findings))}
	for _, finding := range findings {
		if finding.Severity == documentquality.SeverityError {
			result.Passed = false
		}
		result.Findings = append(result.Findings, response.DocumentValidationFinding{Rule: finding.Rule, Category: finding.Category, Severity: string(finding.Severity), Message: finding.Message, Line: finding.Line, Excerpt: finding.Excerpt})
	}
	return result, nil
}

func (service *DocumentGovernanceService) ListComments(ctx context.Context, tenantID, taskID, versionID, userID uint) ([]response.DocumentReviewCommentView, error) {
	task, err := (&WritingTaskService{}).findTaskForMember(ctx, tenantID, taskID, userID)
	if err != nil {
		return nil, err
	}
	if _, err := findTaskVersion(ctx, task, versionID); err != nil {
		return nil, err
	}
	var comments []model.DocumentReviewComment
	if err := global.GVA_DB.WithContext(ctx).Where("task_id = ? AND version_id = ? AND tenant_id = ?", task.ID, versionID, tenantID).Order("created_at ASC, id ASC").Find(&comments).Error; err != nil {
		return nil, err
	}
	result := make([]response.DocumentReviewCommentView, 0, len(comments))
	for _, comment := range comments {
		result = append(result, commentView(comment))
	}
	return result, nil
}

func (service *DocumentGovernanceService) CreateComment(ctx context.Context, tenantID, taskID, versionID, userID uint, input request.DocumentReviewCommentCreate) (response.DocumentReviewCommentView, error) {
	task, err := (&WritingTaskService{}).findTaskForMember(ctx, tenantID, taskID, userID)
	if err != nil {
		return response.DocumentReviewCommentView{}, err
	}
	if _, err := findTaskVersion(ctx, task, versionID); err != nil {
		return response.DocumentReviewCommentView{}, err
	}
	content := strings.TrimSpace(input.Content)
	if content == "" || len([]rune(content)) > 4000 {
		return response.DocumentReviewCommentView{}, fmt.Errorf("批注内容必须为 1 到 4000 个字符")
	}
	if input.AnchorStart < 0 || input.AnchorEnd < input.AnchorStart {
		return response.DocumentReviewCommentView{}, fmt.Errorf("批注定位范围无效")
	}
	if len([]rune(input.Quote)) > 1000 {
		return response.DocumentReviewCommentView{}, fmt.Errorf("批注引用片段不能超过 1000 个字符")
	}
	if input.ParentID != 0 {
		var parent model.DocumentReviewComment
		if err := global.GVA_DB.WithContext(ctx).Where("id = ? AND task_id = ? AND version_id = ? AND tenant_id = ?", input.ParentID, task.ID, versionID, tenantID).First(&parent).Error; err != nil {
			return response.DocumentReviewCommentView{}, fmt.Errorf("回复的批注不存在或不属于当前版本")
		}
	}
	comment := model.DocumentReviewComment{TaskID: task.ID, VersionID: versionID, TenantID: tenantID, OrganizationID: task.OrganizationID, CreatedBy: userID, ParentID: input.ParentID, AnchorStart: input.AnchorStart, AnchorEnd: input.AnchorEnd, Quote: strings.TrimSpace(input.Quote), Content: content, Status: "open"}
	if err := global.GVA_DB.WithContext(ctx).Create(&comment).Error; err != nil {
		return response.DocumentReviewCommentView{}, err
	}
	return commentView(comment), nil
}

func (service *DocumentGovernanceService) ResolveComment(ctx context.Context, tenantID, taskID, versionID, commentID, userID uint, input request.DocumentReviewCommentResolve) (response.DocumentReviewCommentView, error) {
	task, err := (&WritingTaskService{}).findTaskForMember(ctx, tenantID, taskID, userID)
	if err != nil {
		return response.DocumentReviewCommentView{}, err
	}
	var comment model.DocumentReviewComment
	if err := global.GVA_DB.WithContext(ctx).Where("id = ? AND task_id = ? AND version_id = ? AND tenant_id = ? AND organization_id = ?", commentID, task.ID, versionID, tenantID, task.OrganizationID).First(&comment).Error; err != nil {
		return response.DocumentReviewCommentView{}, err
	}
	updates := map[string]any{"status": "open", "resolved_by": 0, "resolved_at": nil}
	if input.Resolved {
		now := time.Now()
		updates = map[string]any{"status": "resolved", "resolved_by": userID, "resolved_at": now}
	}
	if err := global.GVA_DB.WithContext(ctx).Model(&comment).Updates(updates).Error; err != nil {
		return response.DocumentReviewCommentView{}, err
	}
	if err := global.GVA_DB.WithContext(ctx).First(&comment, comment.ID).Error; err != nil {
		return response.DocumentReviewCommentView{}, err
	}
	return commentView(comment), nil
}

func findTaskVersion(ctx context.Context, task *model.WritingTask, versionID uint) (*model.DocumentVersion, error) {
	if versionID == 0 {
		return nil, fmt.Errorf("版本 ID 无效")
	}
	var version model.DocumentVersion
	if err := global.GVA_DB.WithContext(ctx).Where("id = ? AND task_id = ? AND tenant_id = ? AND organization_id = ?", versionID, task.ID, task.TenantID, task.OrganizationID).First(&version).Error; err != nil {
		return nil, err
	}
	return &version, nil
}

func convertDiff(items []documentdiff.Segment) []response.DocumentDiffSegment {
	result := make([]response.DocumentDiffSegment, 0, len(items))
	for _, item := range items {
		result = append(result, response.DocumentDiffSegment{Kind: string(item.Kind), Text: item.Text})
	}
	return result
}

func commentView(comment model.DocumentReviewComment) response.DocumentReviewCommentView {
	return response.DocumentReviewCommentView{ID: comment.ID, TaskID: comment.TaskID, VersionID: comment.VersionID, CreatedBy: comment.CreatedBy, ParentID: comment.ParentID, AnchorStart: comment.AnchorStart, AnchorEnd: comment.AnchorEnd, Quote: comment.Quote, Content: comment.Content, Status: comment.Status, ResolvedBy: comment.ResolvedBy, ResolvedAt: comment.ResolvedAt, CreatedAt: comment.CreatedAt}
}

func sensitiveTerms(constraints []string) []string {
	terms := make([]string, 0, len(constraints))
	for _, constraint := range constraints {
		value := strings.TrimSpace(constraint)
		if !strings.HasPrefix(value, "敏感词:") {
			continue
		}
		for _, term := range strings.FieldsFunc(strings.TrimPrefix(value, "敏感词:"), func(r rune) bool { return r == '，' || r == ',' || r == '、' || r == '\n' }) {
			if term = strings.TrimSpace(term); term != "" {
				terms = append(terms, term)
			}
		}
	}
	sort.Strings(terms)
	return terms
}
