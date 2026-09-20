// Package officialdoc 提供公文写作领域的应用服务。
package officialdoc

// ServiceGroup 是公文服务的领域入口。
// 目前只完成架构隔离；后续服务按模板、任务、版本等子域在此聚合。
type ServiceGroup struct {
	KnowledgeDocumentService
	KnowledgeSearchService
	DocumentTemplateService
	WritingTaskService
	DocumentGovernanceService
	WritingRunService
}

// ServiceGroupApp 由公文 API 与后续初始化逻辑使用。
var ServiceGroupApp = new(ServiceGroup)
