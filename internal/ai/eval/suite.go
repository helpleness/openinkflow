// Package eval defines a reproducible, public-office evaluation suite for
// InkFlow agents. The suite deliberately contains only synthetic, non-secret
// public-office material so it can be checked into the public repository.
package eval

import (
	"fmt"
	"sort"
)

const SuiteVersion = "public-office-v1"

const (
	KindRetrieval = "retrieval"
	KindSummary   = "summary"
	KindDraft     = "draft"
	KindRewrite   = "rewrite"
	KindToolCall  = "tool_call"
	KindCitation  = "citation"
)

// Task is one independently gradable agent request. RelevantChunkIDs and
// ExpectedTools are labels for scoring; they are not injected into the agent
// prompt. A runner should expose Corpus() to its retrieval tool.
type Task struct {
	ID               string   `json:"id"`
	SuiteVersion     string   `json:"suite_version"`
	Kind             string   `json:"kind"`
	Topic            string   `json:"topic"`
	Prompt           string   `json:"prompt"`
	RelevantChunkIDs []string `json:"relevant_chunk_ids,omitempty"`
	ExpectedTools    []string `json:"expected_tools,omitempty"`
	RequiredTerms    []string `json:"required_terms,omitempty"`
}

// CorpusChunk is intentionally small and synthetic. It gives a public runner
// a stable corpus and stable citation IDs without publishing a user's files.
type CorpusChunk struct {
	ID      string `json:"id"`
	Topic   string `json:"topic"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

type subject struct {
	Slug     string
	Name     string
	Action   string
	Deadline string
}

var subjects = []subject{
	{"archive", "档案移交", "完成目录核对、清点和移交登记", "本周五"},
	{"budget", "预算执行", "汇总项目支出并说明偏差原因", "本月末"},
	{"meeting", "会议组织", "确定议程、参会范围和会务分工", "会前两个工作日"},
	{"procurement", "采购需求", "复核采购清单、预算和验收标准", "本季度内"},
	{"safety", "安全检查", "排查隐患、建立台账并跟踪整改", "三日内"},
	{"emergency", "应急值守", "核验值班安排、报送渠道和联络人", "立即"},
	{"training", "业务培训", "编制课程安排、签到和反馈表", "下周三前"},
	{"complaint", "群众诉求", "登记受理、核实情况并反馈办理进度", "五个工作日内"},
	{"inspection", "督查检查", "梳理问题清单、责任单位和整改时限", "十个工作日内"},
	{"assets", "资产盘点", "核对实物、台账和差异说明", "月底前"},
	{"data", "数据报送", "校验口径、完成汇总并留存来源", "每周一上午"},
	{"duty", "值班安排", "发布排班、交接要求和突发事项流程", "节假日前"},
	{"disclosure", "信息公开", "审核公开范围、脱敏内容和发布流程", "法定期限内"},
	{"project", "项目推进", "更新里程碑、风险和协调事项", "每周例会前"},
	{"petition", "信访办理", "分办事项、核实材料并形成答复要点", "规定时限内"},
	{"document", "公文流转", "登记来文、提出拟办意见并归档", "收文当日"},
	{"audit", "内部审计", "收集凭证、核验流程并形成问题清单", "审计期内"},
	{"service", "政务服务", "核对申请材料、一次性告知和办结记录", "承诺时限内"},
	{"confidential", "保密管理", "核验涉密载体、权限和登记记录", "立即"},
	{"publicity", "政策宣传", "确认解读口径、发布渠道和咨询方式", "发布前"},
}

// BuildSuite returns 120 tasks: 20 topics times six task kinds. Keeping the
// generator in source makes the checked-in JSONL auditable and reproducible.
func BuildSuite() []Task {
	tasks := make([]Task, 0, len(subjects)*6)
	for _, item := range subjects {
		relevant := []string{chunkID(item, 1), chunkID(item, 2)}
		for _, kind := range []string{KindRetrieval, KindSummary, KindDraft, KindRewrite, KindToolCall, KindCitation} {
			task := Task{
				ID:               fmt.Sprintf("%s-%s", kind, item.Slug),
				SuiteVersion:     SuiteVersion,
				Kind:             kind,
				Topic:            item.Name,
				RelevantChunkIDs: append([]string(nil), relevant...),
			}
			switch kind {
			case KindRetrieval:
				task.Prompt = fmt.Sprintf("请检索与“%s”有关的依据，找出办理事项、时限和责任要求，并返回依据片段。", item.Name)
			case KindSummary:
				task.Prompt = fmt.Sprintf("依据知识库材料，概括%s工作的目标、办理动作和时限，控制在120字内。", item.Name)
				task.RequiredTerms = []string{item.Action, item.Deadline}
			case KindDraft:
				task.Prompt = fmt.Sprintf("依据知识库材料，起草一则关于%s的工作通知，包含事项、责任分工和完成时限。", item.Name)
				task.RequiredTerms = []string{item.Name, item.Deadline}
			case KindRewrite:
				task.Prompt = fmt.Sprintf("将“请大家尽快处理%s相关事情”改写为正式、明确的公文表述，写清办理动作和时限。", item.Name)
				task.RequiredTerms = []string{item.Deadline}
			case KindToolCall:
				task.Prompt = fmt.Sprintf("先查询知识库，再回答%s的办理要求；回答前必须调用知识检索工具。", item.Name)
				task.ExpectedTools = []string{"knowledge.search"}
			case KindCitation:
				task.Prompt = fmt.Sprintf("依据知识库回答%s应如何办理，并在每项事实后标注所依据的片段编号。", item.Name)
			}
			tasks = append(tasks, task)
		}
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks
}

// Corpus returns two synthetic evidence chunks for every topic. IDs are the
// only allowed citations in the suite results.
func Corpus() []CorpusChunk {
	chunks := make([]CorpusChunk, 0, len(subjects)*2)
	for _, item := range subjects {
		chunks = append(chunks,
			CorpusChunk{ID: chunkID(item, 1), Topic: item.Name, Title: item.Name + "办理要求", Content: fmt.Sprintf("%s工作应当%s，明确承办责任并在%s前完成。", item.Name, item.Action, item.Deadline)},
			CorpusChunk{ID: chunkID(item, 2), Topic: item.Name, Title: item.Name + "留痕要求", Content: fmt.Sprintf("办理%s时，应核验材料来源，形成办理记录；遇到问题及时报告并按程序协调。", item.Name)},
		)
	}
	sort.Slice(chunks, func(i, j int) bool { return chunks[i].ID < chunks[j].ID })
	return chunks
}

func chunkID(item subject, sequence int) string {
	return fmt.Sprintf("PO-%s-%02d", item.Slug, sequence)
}
