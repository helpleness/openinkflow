// Package officialdoc 定义公文写作领域的持久化模型。
//
// 该包刻意不依赖小说 Project 等已移除的领域模型。后续的模板、任务、版本、
// 资料、审阅和规则模型都应放在此处，并通过 initialize/gorm.go 统一迁移。
package officialdoc
