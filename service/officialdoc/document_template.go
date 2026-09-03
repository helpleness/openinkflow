package officialdoc

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"InkFlow/global"
	model "InkFlow/model/officialdoc"
	request "InkFlow/model/officialdoc/request"
	response "InkFlow/model/officialdoc/response"
)

// DocumentTemplateService manages templates; generation is deliberately owned by
// WritingTaskService so a template update cannot mutate an existing task history.
type DocumentTemplateService struct{}

func (service *DocumentTemplateService) List(ctx context.Context, tenantID, organizationID, userID uint) ([]response.DocumentTemplateView, error) {
	if err := ensureKnowledgeMember(ctx, tenantID, organizationID, userID); err != nil {
		return nil, err
	}
	db := global.GVA_DB
	var templates []model.DocumentTemplate
	if err := db.WithContext(ctx).Where("tenant_id = ? AND organization_id = ?", tenantID, organizationID).Order("is_enabled DESC, updated_at DESC").Find(&templates).Error; err != nil {
		return nil, err
	}
	views := make([]response.DocumentTemplateView, 0, len(templates))
	for _, template := range templates {
		views = append(views, templateView(template))
	}
	return views, nil
}

func (service *DocumentTemplateService) Create(ctx context.Context, tenantID, userID uint, input request.DocumentTemplateCreate) (response.DocumentTemplateView, error) {
	if err := ensureKnowledgeMember(ctx, tenantID, input.OrganizationID, userID); err != nil {
		return response.DocumentTemplateView{}, err
	}
	name, code, body := strings.TrimSpace(input.Name), strings.TrimSpace(input.Code), strings.TrimSpace(input.Body)
	if name == "" || code == "" || body == "" {
		return response.DocumentTemplateView{}, fmt.Errorf("模板名称、编码和正文不能为空")
	}
	enabled := true
	if input.IsEnabled != nil {
		enabled = *input.IsEnabled
	}
	variables, err := json.Marshal(compactStrings(input.Variables))
	if err != nil {
		return response.DocumentTemplateView{}, err
	}
	constraints, err := json.Marshal(compactStrings(input.Constraints))
	if err != nil {
		return response.DocumentTemplateView{}, err
	}
	template := model.DocumentTemplate{TenantID: tenantID, OrganizationID: input.OrganizationID, CreatedBy: userID, Name: name, Code: code, Description: strings.TrimSpace(input.Description), Category: strings.TrimSpace(input.Category), Body: body, Variables: string(variables), Constraints: string(constraints), IsEnabled: enabled}
	if err := global.GVA_DB.WithContext(ctx).Create(&template).Error; err != nil {
		return response.DocumentTemplateView{}, err
	}
	return templateView(template), nil
}

func (service *DocumentTemplateService) Update(ctx context.Context, tenantID, templateID, userID uint, input request.DocumentTemplateUpdate) (response.DocumentTemplateView, error) {
	db := global.GVA_DB
	var template model.DocumentTemplate
	if err := db.WithContext(ctx).Where("id = ? AND tenant_id = ?", templateID, tenantID).First(&template).Error; err != nil {
		return response.DocumentTemplateView{}, err
	}
	if err := ensureKnowledgeMember(ctx, tenantID, template.OrganizationID, userID); err != nil {
		return response.DocumentTemplateView{}, err
	}
	name, body := strings.TrimSpace(input.Name), strings.TrimSpace(input.Body)
	if name == "" || body == "" {
		return response.DocumentTemplateView{}, fmt.Errorf("模板名称和正文不能为空")
	}
	variables, _ := json.Marshal(compactStrings(input.Variables))
	constraints, _ := json.Marshal(compactStrings(input.Constraints))
	updates := map[string]any{"name": name, "description": strings.TrimSpace(input.Description), "category": strings.TrimSpace(input.Category), "body": body, "variables": string(variables), "constraints": string(constraints), "is_enabled": input.IsEnabled}
	if err := db.WithContext(ctx).Model(&template).Updates(updates).Error; err != nil {
		return response.DocumentTemplateView{}, err
	}
	if err := db.WithContext(ctx).First(&template, template.ID).Error; err != nil {
		return response.DocumentTemplateView{}, err
	}
	return templateView(template), nil
}

func templateView(template model.DocumentTemplate) response.DocumentTemplateView {
	return response.DocumentTemplateView{ID: template.ID, OrganizationID: template.OrganizationID, Name: template.Name, Code: template.Code, Description: template.Description, Category: template.Category, Body: template.Body, Variables: decodeStrings(template.Variables), Constraints: decodeStrings(template.Constraints), IsEnabled: template.IsEnabled, UpdatedAt: template.UpdatedAt}
}

func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func decodeStrings(raw string) []string {
	var values []string
	if json.Unmarshal([]byte(raw), &values) != nil {
		return []string{}
	}
	return values
}
