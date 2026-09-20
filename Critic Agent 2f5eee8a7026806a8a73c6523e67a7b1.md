# Critic Agent

基于我们之前的深入讨论，这是为您量身定制的 **Critic Agent (全维度逻辑审查官)** 最终落地方案。

这个方案的核心在于：**Critic 不是一个被动的“阅读者”，而是一个主动的“规则执行者”**。它依赖 Go 后端构建的“全息上下文”来捕获那些隐性的逻辑冲突。

---

### 1. Critic Agent 架构概览

- **运行位置**：Go 后端调用的独立 `llama.cpp` 实例（或同一实例的并行 Slot）。
- **模型选择**：建议使用 **DeepSeek-R1-Distill-Qwen-7B** 或 **Qwen-2.5-14B-Instruct**。这类型号逻辑推理能力强，且在本地运行速度快。
- **核心职责**：对比【正文草稿】与【3+1 维度限制】，输出 JSON 判决书。

---

### 2. 第一步：Go 端构建“全息上下文” (The Context Builder)

在调用 Critic AI 之前，Go 程序必须先去各个数据库里“抓药”，组装成一个**不可违背的规则集合**。

### A. 空间维 (Spatial Context) - 必选

- **来源**：`实体百科库` (Location Tags) & `规则库`
- **逻辑**：读取当前大纲所在的 `location_id`。
- **提取内容**：该地点的物理属性、禁制。
    - *例子*：“霍格沃茨大礼堂：天花板同步天气；禁止幻影移形。”

### B. 状态维 (State Context) - 必选

- **来源**：`角色矩阵库` (Character Status)
- **逻辑**：读取当前场景所有参与者的 `ActorID`。
- **提取内容**：HP/MP、Buff/Debuff、持有物品状态。
    - *例子*：“墨离：[左臂骨折]、[灵力枯竭]、[持有：断剑]。”

### C. 全局维 (Global Context) - 必选

- **来源**：`事件/事实库` (Timeline)
- **逻辑**：读取当前虚拟时间 `VirtualTime`。
- **提取内容**：当前时间段的世界级生效法则。
    - *例子*：“当前时间：凛冬之夜（火系魔法失效，冰系增强）。”

### D. 动作维 (Action Semantic) - 动态检索

- **来源**：`规则法则库` (Vector DB)
- **逻辑**：
    1. 对草稿进行简单分词（Go 侧 NLP 或正则），提取动词/名词（飞、杀、救、剑、药）。
    2. 去向量库检索最相关的 Top-3 规则。
    3. *例子*：提取到“飞行”，检索出“御剑术消耗规则”。

---

### 3. 第二步：Critic 的 Prompt 设计 (The Protocol)

这是发送给 Critic Agent 的最终指令。我们需要强制它进行**CoT (思维链)** 推理，但最终只输出 JSON。

Markdown

# 

`# Role
你是一个严格的小说逻辑校验系统。你的唯一任务是找出【正文草稿】中的逻辑漏洞。

# Constraints (绝对真理)
以下规则和状态不可违背：
[1. 空间规则]: {spatial_rules} (如：禁空领域)
[2. 角色状态]: {actor_status} (如：墨离左手已断)
[3. 全局环境]: {global_rules} (如：现在是午夜)
[4. 动作判例]: {action_rules} (如：使用"禁术"会扣除寿命)

# Draft (待审查正文)
"""
{draft_text}
"""

# Task
仔细对比正文与Constraints。如果正文描述的行为违反了上述任何一条，视为逻辑错误。
注意隐性冲突：例如"禁空领域"意味着角色不能"飞起来"，即使正文没提"禁空"二字。

# Output Format
请严格返回以下JSON格式，不要包含Markdown代码块：
{
    "is_valid": boolean, // true=通过, false=有逻辑错误
    "errors": [
        {
            "type": "状态冲突/环境冲突/规则冲突",
            "content": "原文写'他举起左手'，但状态显示'左手已断'。",
            "severity": "high/medium"
        }
    ],
    "suggestion": "一句话建议，如：将举起左手改为举起右手，或改为因断臂无法举起。"
}`

---

### 4. 第三步：Go 后端执行逻辑 (The Controller)

这是在 Go 中实现 Critic 工作流的代码结构：

Go

# 

`type CriticResponse struct {
    IsValid    bool        `json:"is_valid"`
    Errors     []ErrorItem `json:"errors"`
    Suggestion string      `json:"suggestion"`
}

type ErrorItem struct {
    Type     string `json:"type"`
    Content  string `json:"content"`
    Severity string `json:"severity"`
}

func (e *Engine) RunCritic(draft string, chapterInfo Chapter) (*CriticResponse, error) {
    // 1. 【聚合阶段】Go 主动拉取上下文 (3+1维度)
    // 空间维
    locRules := e.VectorDB.GetRulesByLocation(chapterInfo.LocationID)
    // 状态维
    charStatus := e.SQLDB.GetCharactersStatus(chapterInfo.CharacterIDs)
    // 全局维
    globalRules := e.SQLDB.GetGlobalRules(e.CurrentVirtualTime)
    // 动作维 (基于草稿关键词)
    keywords := e.NLP.ExtractKeywords(draft)
    actionRules := e.VectorDB.SearchRules(keywords)

    // 2. 【组装 Prompt】
    prompt := fmt.Sprintf(CriticPromptTemplate, locRules, charStatus, globalRules, actionRules, draft)

    // 3. 【推理阶段】调用 Critic 模型
    // 建议设置 temperature=0.1，保证逻辑严谨
    rawJSON, err := e.LlamaClient.Generate(prompt, ModelParams{Temp: 0.1})
    if err != nil {
        return nil, err
    }

    // 4. 【解析结果】
    var result CriticResponse
    if err := json.Unmarshal([]byte(rawJSON), &result); err != nil {
        // 如果模型没按JSON输出，这里需要重试机制
        return nil, fmt.Errorf("Critic output format error: %v", err)
    }

    return &result, nil
}`

---

### 5. 第四步：反馈闭环 (The Loop)

当 `RunCritic` 返回结果后，Go 需要根据 `is_valid` 字段决定下一步：

- **Case A: Pass (`is_valid: true`)**
    - 流程继续。
    - 调用 **Observer (记录员)** 提取既定事实，存入数据库。
    - 推送正文给前端。
- **Case B: Fail (`is_valid: false`)**
    - **自动修正模式**：
        
        Go 将 `CriticResponse.suggestion` 附加到 **Writer** 的下一轮 Prompt 中：
        
        > System: 上一版草稿因逻辑错误被驳回。错误原因：{error.content}。请根据建议："{suggestion}" 重写本段。
        > 
    - **人工介入模式**（推荐用于 Severity=High）：
        
        前端弹窗提示用户：“检测到严重逻辑冲突（左手已断却在用左手剑），是否强制通过或要求 AI 重写？”
        

---

### 6. 优化策略：如何防止漏检“隐性规则”？

你担心的“漏掉规则”主要通过 **Vector DB 的关联元数据** 来解决。

**建立“触发词映射表” (Trigger Map)**：

在 Go 中维护一个简单的映射字典，用于增强检索：

- 如果大纲/草稿包含：`"攻击"`, `"战斗"`, `"杀"`
    - > 自动强制检索规则：`"和平区域限制"`, `"主角当前战斗力"`
- 如果大纲/草稿包含：`"走"`, `"跑"`, `"飞"`, `"移动"`
    - > 自动强制检索规则：`"地形限制"`, `"腿部状态"`

**这样，只要 AI 写了“走两步”，系统就会强制检查“腿断没断”，而不需要 AI 显式提到“腿”。**

### 总结方案价值

这套 Critic Agent 方案将逻辑检查从“黑盒生成”变成了**“白盒审计”**。
它利用 Go 的数据聚合能力，把**世界观（空间）、时间轴（全局）、人物卡（状态）**强行拍在 AI 面前，迫使 AI 在“戴着镣铐跳舞”的情况下进行创作，从而彻底解决长篇小说逻辑崩坏的问题。