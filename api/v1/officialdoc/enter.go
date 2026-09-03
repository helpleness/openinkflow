// Package officialdoc 提供公文写作领域的 HTTP API。
package officialdoc

// ApiGroup 是公文 API 的领域入口。
// 后续每个公文子域（模板、任务、版本、资料、审阅、规则）均在此聚合，
// 不能回写到 system 或已移除的小说领域。
type ApiGroup struct {
	KnowledgeDocumentApi
	KnowledgeSearchApi
	DocumentTemplateApi
	WritingTaskApi
	DocumentGovernanceApi
	WritingRunApi
}

// ApiGroupApp 由公文路由层使用。
var ApiGroupApp = new(ApiGroup)
