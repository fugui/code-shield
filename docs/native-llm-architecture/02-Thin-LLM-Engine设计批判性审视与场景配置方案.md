# 02-Thin LLM Engine 设计批判性审视与场景配置方案

## 📋 文档元数据

*   **文档编号**：`CS-NATIVE-02`
*   **文档类型**：架构评审辅助材料 / 批判性审视报告
*   **前置依赖**：[01-CodeShield-原生LLM轻量执行引擎与混合调用架构设计](01-CodeShield-原生LLM轻量执行引擎与混合调用架构设计.md)
*   **编制目的**：从 Challenger（批判者）角度，对 01 号设计文档的**必要性、完备性、准确性**进行严格审视，纠正夸大与遗漏，并补充缺失的"Thin/Thick 场景可配置化路由"设计。

---

## 一、 必要性审视：这件事到底值不值得做？

### 1.1 ✅ 确实成立的痛点

| 痛点 | 审计结论 | 现有代码佐证 |
| :--- | :--- | :--- |
| **RepairJSON 杀鸡用牛刀** | **完全成立且优先级最高**。修 JSON 语法不需要任何工具调用和文件探索，当前为此拉起 CLI 进程纯属浪费。 | [`services/ai_cli.go:RepairJSON`](file:///home/fugui/codes/code-shield/services/ai_cli.go#L99) 直接调用 `GetAIInvoker(backend)` + `invoker.Invoke`，逻辑上完全不依赖 CLI 的 Agent 能力。 |
| **OpenCode SQLite 锁冲突** | **完全成立**。`prepareIsolatedDataDir` 是为了绕过 OpenCode 本地 SQLite 并发锁而专门编写的 Workaround，增加了维护复杂度。 | [`services/opencode_cli.go:47-60`](file:///home/fugui/codes/code-shield/services/opencode_cli.go#L47) 创建临时目录 + 拷贝 `auth.json` 的 hack。 |
| **`askLLMIfSameFinding` 对 CLI 的不必要依赖** | **成立**。这是文档遗漏的第 7 个场景——缺陷指纹语义匹配也是典型的单轮确定性推理。 | [`services/hooks.go:624-710`](file:///home/fugui/codes/code-shield/services/hooks.go#L624) 纯粹构造 Prompt → 调 LLM → 读 JSON 结果，无需任何探索能力。 |

### 1.2 ⚠️ 需要谨慎校正的设计假设

#### 假设一："Judge 和 Challenger 适合用 Thin LLM"——**部分成立，但有关键前提**

文档将 Challenger（辩护人）和 Judge（法官）都归类为"无需工具调用的单轮推理任务"。**从代码事实来看这确实是成立的**——[`runChallengerStage`](file:///home/fugui/codes/code-shield/services/engine_debate.go#L418) 和 [`runJudgeStage`](file:///home/fugui/codes/code-shield/services/engine_debate.go#L440) 都只是 `callAITier(prompt, workDir, ...)` → 读 JSON。

**但必须警惕一个隐性风险：辩论对抗质量对模型推理能力高度敏感。**

*   **Thick Agent CLI 方案的隐性优势**：CLI 的 Agent 运行时（如 `claude` 或 `agy`）在内部可能使用 system prompt 注入、thinking/chain-of-thought 以及更精细的 token 分配策略。直接调用裸 `/v1/chat/completions` 时，如果不显式配置 system prompt、thinking budget 等参数，可能导致推理深度下降。
*   **实际影响**：Judge 的裁决准确率如果因为切换引擎而下降 5%~10%，可能带来大量漏报或误报——这恰恰是这套三方辩论架构最核心的价值所在。

> **结论**：Judge 和 Challenger 切换为 Thin LLM 在技术上完全可行，但**不应作为默认推荐**，应先在保持同等模型（如 `gemini-3.7-pro`）和 system prompt 一致的条件下进行 A/B 对照基准测试，确认裁决质量不降级后再推行。

#### 假设二："初筛可以 50~100 路并发"——**取决于 LLM 服务端，不是客户端能力**

文档反复强调"HTTP 连接池轻松支持 50~100 路并发初筛"，这个描述有**偷换瓶颈归因**的嫌疑：

*   当前并发受限的核心原因是 **LLM 服务端的 GPU 吞吐上限**（无论 vLLM、TGI 还是云端 API 的 RPM/TPM 配额），而非 Go 端 `os/exec` 进程创建能力。
*   如果 LLM 服务本身只能支撑 10 路并发推理请求，那客户端用连接池发 100 个 HTTP 请求只会收到 429 限流或排队等待，**真实吞吐不会提升**。

> **结论**：文档应清晰区分"客户端发送能力"和"服务端处理能力"，避免给架构组造成"切到 HTTP 就能 10x 提速"的误导。真正的收益是**消除 2~10s 的客户端进程启动延迟**与**释放本地 OS 资源**，但端到端吞吐的天花板取决于 LLM 服务算力。

#### 假设三："收益数据 70% 缩减"——**缺乏实测支撑**

第五章对比评估表中给出了"单仓全扫描总耗时从 15 分钟降至 4.5 分钟（缩减 70%）"——**这是估算而非实测数据**。在正式架构评审中，这样的量化结论应标注为"预估值"，并在落地阶段设计对照实验验证。

### 1.3 ❌ 文档遗漏的场景

| 遗漏场景 | 代码位置 | 说明 |
| :--- | :--- | :--- |
| **`askLLMIfSameFinding`（缺陷指纹语义匹配）** | [`services/hooks.go:623-710`](file:///home/fugui/codes/code-shield/services/hooks.go#L623) | 典型的单轮 Prompt → JSON 场景，应纳入 Thin LLM 候选清单。 |
| **Chunked 引擎（非辩论模式）下的分片分析** | [`services/engine_chunked.go`](file:///home/fugui/codes/code-shield/services/engine_chunked.go) | 旧版分片扫描引擎直接调用 `AIInvoker.Invoke`，**但这类分析需要 Agent 自主读取代码文件**——并非纯 Prompt 输入，不适合 Thin LLM。文档未显式排除此场景。 |
| **Single 引擎（`task_runner.go:executeAI`）** | [`services/task_runner.go:413`](file:///home/fugui/codes/code-shield/services/task_runner.go#L413) | 同上，Single 模式下 Agent 需要读 `InputFiles`，并非纯 Prompt。 |

---

## 二、 完备性审视：设计中遗漏了哪些关键维度？

### 2.1 缺失：System Prompt 与高级推理参数传递

当前 `NativeInvoker` 示例代码只构造了 `user` 角色消息：

```go
"messages": []map[string]string{
    {"role": "user", "content": promptPayload},
},
```

但在辩论流水线中，Hunter/Challenger/Judge 都有**系统级角色定义与行为规约**（如 `buildJudgePrompt` 中的 "你是 Code-Shield 漏洞终审法官"）。当前方案将系统指令和用户输入混在同一个 `user` 消息中，丧失了：

1.  **`system` 角色消息的权重优先级**（多数模型对 system 指令的遵循度更高）；
2.  **`max_tokens`、`top_p` 等高级参数调优能力**；
3.  **Thinking/Extended Thinking 模式支持**（Claude 的 `thinking` budget、Gemini 的 `thinkingConfig`）。

> **建议**：`NativeInvoker` 应支持 `SystemPrompt` 字段，并将 `AIRequest` 扩展支持 `MaxTokens`、`TopP`、`ThinkingBudget` 等可选参数。

### 2.2 缺失：Token 用量统计与追踪

当前 [`callAITier`](file:///home/fugui/codes/code-shield/services/engine_debate.go#L598) 返回 `(string, int64, error)` 其中 `int64` 是 Token 用量。但 `NativeInvoker` 示例代码丢弃了 API 返回中的 `usage` 字段。如果不追踪 Token，Tier 级别的用量统计和成本核算将失真。

### 2.3 缺失：重试与指数退避

`config.yaml` 示例中列出了 `max_retries: 3` 和 `retry_backoff_ms: 500`，但 `NativeInvoker` 的实现代码中**完全没有重试逻辑**。在生产环境下 LLM API 的 5xx / 429 重试是刚需。

### 2.4 缺失：流式响应支持

部分场景（如大型报告汇总生成 Tier 3）的输出可能很长。如果 LLM 服务端响应时间较长（>30s），非流式模式下 HTTP 连接可能触发中间代理（Nginx / LB）的超时。应考虑支持 SSE 流式响应模式 (`stream: true`)。

---

## 三、 准确性审视：现有技术方案的细节纠正

### 3.1 `RepairJSON` 改造示例中的逻辑缺陷

```go
// 原文示例
invoker := GetAIInvoker("native")
if invoker == nil {
    invoker = GetAIInvoker(aiBackend)
}
```

`GetAIInvoker` 在找不到注册名时**不会返回 nil**，而是回退到 `claude`：

```go
// services/ai_cli.go:85-97
func GetAIInvoker(name string) AIInvoker {
    var inv AIInvoker
    var ok bool
    if inv, ok = invokerRegistry[name]; !ok {
        log.Printf("[AI] WARNING: AI backend %q is not registered, falling back to claude\n", name)
        inv = invokerRegistry["claude"]
    }
    ...
}
```

因此 `if invoker == nil` 的判断**永远不会为真**——如果 `native` 未注册，会静默回退到 `claude`，而非预期的 `aiBackend`。

### 3.2 `defer os.Remove(fixedPath)` 在 `os.ReadFile` 之后

```go
fixed, err := os.ReadFile(fixedPath)
if err != nil {
    return nil, err
}
defer os.Remove(fixedPath)  // ← 这行在函数返回后才执行，但已经读完了——逻辑上可以，但风格上应提前声明
```

虽然功能正确，但 `defer` 放在 `ReadFile` 之后意味着如果 `ReadFile` 报错，临时文件不会被清理。应在 `Invoke` 成功后立即 `defer`。

---

## 四、 缺失的关键设计：场景级 Thin/Thick 可配置化路由

### 4.1 为什么需要场景级可配置化？

01 号文档将"哪些场景用 Thin、哪些用 Thick"**硬编码在设计假设中**，但在实际生产环境下：

1.  **不同租户的部署环境差异极大**：有的客户只有 Thick Agent CLI（如只部署了 `claude`），没有可用的 OpenAI 兼容 LLM API 端点；
2.  **模型能力对比会持续变化**：今天 Judge 用 Thin LLM 质量不够，明天换了更强的模型可能就足够了；
3.  **灰度验证需求**：上线初期希望只对 RepairJSON 和初筛启用 Thin，辩论仍用 Thick，等验证稳定后再扩展；
4.  **特定任务类型可能有不同偏好**：C++ Coredump 分析的 Hunter 可能必须用重型 Agent 读 `.cc` 文件，但 Go 的代码风格检查用 Thin 初筛即可。

### 4.2 配置化设计方案

在现有 `TierConfig` 的基础上，将 `backend` 字段的语义从"选择哪个 CLI"扩展为"选择哪种执行模式"——`native` 成为一个与 `claude`/`agy`/`opencode`/`codex` 平级的可选后端。**无需新增任何新的配置层级**，现有 Tier 机制天然支持：

```yaml
# config.yaml — 场景级 Thin/Thick 混合编排配置示例

ai:
  backend: "agy"                    # 全局默认后端（兜底用）

  native:                           # Thin LLM Engine 连接配置
    endpoint: "http://192.168.56.18:8000/v1/chat/completions"
    api_key: "sk-xxx"
    default_model: "glm-4-flash"
    temperature: 0.1
    response_format_json: true
    max_retries: 3
    retry_backoff_ms: 500

  tiers:
    # ── 初筛场景 ──
    # Hunter 初筛 Prompt 仅传入文件名列表，Agent 需自主读取 WorkDir 下的源码
    # → 必须使用 Thick Agent（具备文件系统访问能力）
    tier1_fast:
      backend: "agy"                # ← Thick Agent（Hunter 需读取代码文件）
      model: "gemini-3.7-flash"
      concurrent: 5
      timeout_seconds: 300

    # ── 深度分析场景 ──
    # Hunter 需要自主探索代码文件，跨文件追踪依赖 → 必须使用 Thick Agent
    tier2_reasoning:
      backend: "agy"                # ← Thick Agent (保留探索能力)
      model: "gemini-3.7-pro"
      concurrent: 5
      timeout_seconds: 1800

    # ── 报告汇总场景 ──
    # 纯文本聚合生成，无需工具调用 → 使用 Thin LLM
    tier3_synthesis:
      backend: "native"             # ← Thin LLM
      model: "glm-4-plus"
      concurrent: 10
      timeout_seconds: 300

  # ── 内嵌工具场景级引擎配置 (新增) ──
  tool_backends:
    repair_json: "native"           # JSON 修复 → 强制 Thin LLM
    finding_match: "native"         # 缺陷指纹语义匹配 → 强制 Thin LLM
    # 以下为默认值，可显式覆盖：
    # feedback_extraction: "native" # 误报规则提炼 → Thin LLM
```

### 4.3 代码层面的适配方案

在 `AIRequest` 或辅助工具函数中，通过查询 `tool_backends` 配置决定使用哪个后端：

```go
// models/config.go 新增
type ToolBackendsConfig struct {
    RepairJSON        string `yaml:"repair_json" json:"repair_json"`
    FindingMatch      string `yaml:"finding_match" json:"finding_match"`
    FeedbackExtract   string `yaml:"feedback_extraction" json:"feedback_extraction"`
}

// services/ai_cli.go 改造
func getToolBackend(toolName string) string {
    cfg := models.AppConfig.AI.ToolBackends
    switch toolName {
    case "repair_json":
        if cfg.RepairJSON != "" { return cfg.RepairJSON }
    case "finding_match":
        if cfg.FindingMatch != "" { return cfg.FindingMatch }
    case "feedback_extraction":
        if cfg.FeedbackExtract != "" { return cfg.FeedbackExtract }
    }
    return models.AppConfig.AI.Backend // 兜底回退全局默认
}
```

### 4.4 完整的场景-引擎决策矩阵（可配置化版本）

| 场景 | 默认引擎 | 配置键 | 可替换为 | 替换条件与注意事项 |
| :--- | :--- | :--- | :--- | :--- |
| **JSON 语法修复** | `native` (Thin) | `ai.tool_backends.repair_json` | 任意 Thick | 几乎不需要替换，Thin 是最优选择 |
| **缺陷指纹语义匹配** | `native` (Thin) | `ai.tool_backends.finding_match` | 任意 Thick | 同上 |
| **Tier 1 初筛 (Hunter)** | `agy` (Thick) | `ai.tiers.tier1_fast.backend` | — | **必须 Thick**：`buildHunterPrompt` 只传文件名列表，Agent 需自主从 WorkDir 读取源码 |
| **Tier 2 深挖 (Hunter)** | `agy` (Thick) | `ai.tiers.tier2_reasoning.backend` | `native` | ⚠️ 仅限 Prompt 中已内联全部代码的场景 |
| **Tier 2 辩论 (Challenger/Judge)** | `agy` (Thick) | `ai.tiers.tier2_reasoning.backend` | `native` | ⚠️ 需先 A/B 验证裁决质量不降级 |
| **Tier 3 报告汇总** | `native` (Thin) | `ai.tiers.tier3_synthesis.backend` | 任意 Thick | 几乎不需要替换 |
| **误报反馈规则提炼** | `native` (Thin) | `ai.tool_backends.feedback_extraction` | 任意 Thick | 几乎不需要替换 |
| **单仓/分片深度分析** | `agy` (Thick) | `ai.backend` / RunParams 覆盖 | — | **不应使用 Thin**：Agent 需自主读取 InputFiles |

---

## 五、 综合审视结论与修订建议

### 5.1 必要性定性

| 评估维度 | 结论 |
| :--- | :--- |
| **这件事该不该做？** | **应该做**。RepairJSON、指纹语义匹配、报告汇总等场景的收益确定且风险极低。 |
| **做到什么程度？** | 建议**分层推进**：Phase 1 仅覆盖辅助工具（RepairJSON / 指纹匹配 / 反馈提炼），Phase 2 再审慎扩展到辩论流水线。 |
| **核心价值点？** | 不是"10x 并发提速"（受限于 LLM 服务端），而是**消除客户端进程冗余、根治本地状态锁、获得原生结构化 JSON 输出保证**。 |

### 5.2 对 01 号文档的修订建议清单

| 编号 | 修订项 | 优先级 |
| :--- | :--- | :--- |
| R1 | 补充第 7 个场景：`askLLMIfSameFinding`（缺陷指纹语义匹配） | **高** |
| R2 | 将并发收益描述从"客户端 50~100 路"纠正为"取决于 LLM 服务端算力上限" | **高** |
| R3 | Judge/Challenger 切换 Thin LLM 的前提：标注需先完成 A/B 对照基准测试 | **高** |
| R4 | 量化收益表标注"预估值（待实测验证）" | **中** |
| R5 | `NativeInvoker` 补充 system prompt、max_tokens、token usage 追踪 | **高** |
| R6 | `NativeInvoker` 实现重试与指数退避逻辑 | **高** |
| R7 | `RepairJSON` 示例修正 `GetAIInvoker` 永不返回 nil 的逻辑问题 | **中** |
| R8 | 新增第四章：场景级 Thin/Thick 可配置化路由（`ai.tool_backends` + Tier backend） | **高** |
| R9 | 显式排除 Single / Chunked 引擎不适用 Thin LLM 的声明 | **中** |
| R10 | 考虑流式响应模式（`stream: true`）对长时间生成任务的连接超时保护 | **低** |
| R11 | **纠正 01 文档中 Tier 1 初筛可用 Thin LLM 的错误假设**：Hunter 的 Prompt 只传入文件名列表（`buildHunterPrompt` L521-524），Agent 需自主从 WorkDir 读取源码文件，必须保持 Thick Agent | **高** |

---
*批判性审视编制人：Code-Shield 核心架构组*
*编制日期：2026-09-02*
