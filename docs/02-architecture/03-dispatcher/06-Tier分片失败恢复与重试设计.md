# Tier 分片失败恢复与重试设计

- 状态：Implemented for Tier 1–4 stage recovery, resource health and observability
- 日期：2026-09-20
- 范围：AI 扫描分片、Tier 调用、同 resource 重试、OpenCode / Codex / Native 调用恢复、失败分类与报告可观测性
- 关联文档：[OpenCode失败会话续跑与输出守卫设计](05-OpenCode失败会话续跑与输出守卫设计.md)、[LLM结构化输出契约与定向Schema Repair设计](../02-scanner-engines/10-LLM结构化输出契约与定向Schema%20Repair设计.md)

## 1. 背景与问题

2026-09-19 的 `change_review` 扫描报告显示，分片失败集中出现在三个场景：

| 失败场景 | 数量 | 典型特征 |
| --- | ---: | --- |
| `idle_timeout` | 36 | 多个小分片同时等待到 10 分钟无活动 |
| `output_missing` / 空输出 | 24 | CLI 退出成功但目标 artifact 缺失，或 final message 为空 |
| JSON / schema 失败 | 11 | 输出不是合法 JSON、schema 版本错误、`unit_ref` 为空或无有效 unit |

大量失败分片的 `attempts` 为 1、`retries` 为 0，说明当前恢复层没有稳定兜住非合同类错误。当前已有部分恢复能力：

- `native` 已支持 endpoint 级重试与 failover。
- `opencode` 已支持同 session continuation。
- Hunter 阶段已有 output missing fresh retry 和超时二分。
- `specialized assessment` 目前主要只对合同错误做一次重试，对空闲超时、输出缺失缺少统一恢复路径。

当前的问题是恢复策略分散在不同 engine 和 invoker 中，缺少以错误类型为中心的统一恢复决策，也缺少一致的 attempt / error class / resource chain 观测。

## 2. 目标

1. 为每个 AI stage 定义统一的恢复状态机，而不是让每个 engine 自行组合 retry、continuation 和 split。
2. 对空闲超时、空输出、输出缺失提供有限重试；普通重试保持在同一 resource 上执行，不切换资源池。
3. 对合同错误走定向 schema repair，不做普通重试。
4. 保留完整的 attempt 历史、错误分类、耗时、队列等待、resource chain 和 artifact 证据。
5. 避免无限重试、重复 finding、重复 token 消耗和掩盖降级状态。

## 3. 非目标

1. 不在失败后合并多次 AI 输出，同一个分片只接受第一个通过校验的 artifact。
2. 不用切换 resource 解决权限、配置错误或内容过滤等本质性失败。
3. 不把 retry 作为 schema 错误的首选修复方式。
4. 不在本文中改变报告合成和缺陷生命周期语义。

## 3.1 决策原则

同一 Tier 内配置的不同 resource 池能力接近，普通重试不应把切换资源池当作默认收益来源。因此：

1. 首选 resource 固定为本次 stage 的执行身份；`idle_timeout`、`output_missing`、超时等恢复动作优先走 continuation、fresh retry 或 bundle split。
2. 重试和 failover 是不同语义。重试指同一个 resource 内的后续 attempt；failover 指当前 resource 已不可用且显式开启候选切换后的降级动作。
3. 候选 resource failover 默认关闭，只允许在可用性类错误上显式配置，不作为提升生成质量的常规手段。

## 4. 总体状态机

```text
单次调用失败
  |
  |--> driver 内恢复
  |      |- opencode session continuation
  |      |- codex fresh attempt
  |
  |--> stage 内恢复
  |      |- fresh session retry
  |      |- bundle split
  |
  |--> 定向 schema repair
  |      仅用于 contract_mismatch / json_invalid
  |
  |--> 候选 resource failover（可选）
  |      仅当当前 resource 不可用且显式开启
  |
  |--> 预算耗尽
         标记 CHUNK_FAILED + MISSING_ASSESSMENT
```

恢复动作必须记录在同一个 stage attempt context 中。每个阶段拥有总预算、单次 attempt 预算和候选切换上限；任何一次成功都立即返回，不再继续尝试。默认配置下，候选切换上限为 0。

## 5. 配置模型

在现有 `TierBindingConfig` 下扩展 recovery 配置：

```yaml
scanner:
  debate:
    tiers:
      tier1_hunter:
        resources:
          - opencode-deepseek
          - opencode-glm
          - native
        attempt_timeout_seconds: 600
        timeout_seconds: 1800
        first_byte_timeout_seconds: 60
        idle_timeout_seconds: 300

        recovery:
          max_total_attempts: 3
          max_attempts_per_resource: 3
          max_candidate_failovers: 0
          retry_backoff_ms: 2000
          max_backoff_seconds: 15
          split_on:
            - idle_timeout
            - timeout
          retry_on:
            - idle_timeout
            - timeout
            - output_missing
            - resource_busy
            - rate_limited
            - network_transient
          candidate_failover_on:
            - resource_unavailable
            - resource_busy
            - rate_limited
            - network_transient
          contract_repair_on:
            - contract_mismatch
            - json_invalid
```

字段语义：

| 字段 | 语义 |
| --- | --- |
| `attempt_timeout_seconds` | 单次 AI 调用上限，包含 invocation 内部 continuation |
| `timeout_seconds` | stage 总预算，包含 retry、failover 和 split |
| `first_byte_timeout_seconds` | 调用开始后等待首个可观测事件的时间 |
| `idle_timeout_seconds` | 相邻 stdout / stderr / SSE 事件之间允许的最大空闲 |
| `max_total_attempts` | stage 内最多发出的逻辑尝试数 |
| `max_attempts_per_resource` | 同一 resource 的最大尝试数，允许普通重试留在首选 resource |
| `max_candidate_failovers` | 最多切换候选 resource 的次数，默认必须为 0 |
| `retry_on` | 允许在同一 resource 上 fresh retry 的错误类型 |
| `candidate_failover_on` | 允许切换候选 resource 的错误类型；仅在显式开启后生效 |

`timeout_seconds` 不应只是简单叠加 attempt timeout。实现时必须在总 context 中统一倒计时，避免“3 次失败 x 10 分钟”变成 30 分钟级故障。

## 6. 错误决策矩阵

| 错误类型 | 第一动作 | 第二动作 | 禁止动作 |
| --- | --- | --- | --- |
| `resource_unavailable` | 结束当前 resource 的恢复链 | 仅显式开启候选 failover 且预算允许时切换 | 默认原地无限等待 |
| `idle_timeout` | fresh session retry 或 continuation | 多文件 bundle 先 split；单文件分片继续同 resource retry | 无限等待或无预算重试 |
| `timeout` | 多文件 bundle split | 不可拆分时同 resource fresh retry | 无退避高频原 bundle 重试 |
| `output_missing` | OpenCode continuation / Codex fresh attempt | 同 resource fresh retry | 把 stdout 片段当作 artifact |
| `resource_busy` | 短退避 | 同 resource fresh retry | 立即切换 resource |
| `rate_limited` | 指数退避 | 同 resource fresh retry | 无预算高频重试 |
| `network_transient` | 指数退避 | 同 resource fresh retry | 无限重试 |
| `json_invalid` | 定向 schema repair | repair 失败后按预算同 resource retry | 普通重试优先于 repair |
| `contract_mismatch` | 定向 schema repair | repair 失败后按预算同 resource retry | 直接重新生成完整结论 |
| `auth` | 记录并告警 | 默认失败 | 盲目重试或切换 resource |
| `content_filtered` | 记录原始样本 | 默认失败 | 修改 prompt 绕过过滤或切换 resource |
| `canceled` | 立即终止 | 无 | 重试 |

`contract_mismatch` 和 `json_invalid` 不属于普通 retry 类错误。它们应首先进入 schema repair 流程；只有 repair 失败且总预算仍有剩余时，才在同一 resource 上重新生成。资源切换只保留给显式配置的可用性异常。

熔断中的 resource 在 stage 内映射为 `resource_unavailable`。它不会自动打开候选切换；只有 `max_candidate_failovers > 0` 且错误进入 `candidate_failover_on` 时才会降级到备选资源。

## 7. Tier Resource 计划

当前 `AcquireTier()` 在调用前选择一个 backend / model。为了让重试保持执行身份稳定，同时保留有限的可用性降级能力，应新增 resource 计划接口：

```go
type TierCandidate struct {
    ResourceID string
    Driver     string
    Model      string
}

type TierPlan struct {
    Tier       string
    Selected   TierCandidate
    Candidates []TierCandidate
}

func AcquireTierResourcePlan(ctx context.Context, tierName string, excluded map[string]struct{}) (TierPlan, error)
```

排序规则：

1. 优先选择当前 task / chunk 已绑定且未被熔断的 resource，保证重试不换池。
2. 只有在当前 resource 熔断、不可达，且 `max_candidate_failovers > 0` 时才选择备选候选。
3. 备选候选按当前容量、队列等待、最近错误率和 P95 耗时排序。
4. 保持 resource 的配置顺序作为稳定 tie-breaker。
5. 滚动降权只影响新 chunk 的首选调度，不改变已绑定 chunk 的普通重试目标。

执行 wrapper 伪代码：

```go
func RunStageWithRecovery(plan TierPlan, req AIRequest, stage Stage) (Artifact, error) {
    candidate := plan.Selected

    for attempt := 0; attempt < stage.Recovery.MaxTotalAttempts; attempt++ {
        if attempt >= stage.Recovery.MaxTotalAttempts {
            break
        }

        if attempt > 0 && candidateFailoverable(stage.LastError(), stage.Recovery.CandidateFailoverOn) {
            next, ok := nextAvailableCandidate(plan, excludedCandidates(stage))
            if !ok || stage.CandidateFailovers >= stage.Recovery.MaxCandidateFailovers {
                break
            }

            candidate = next
            stage.RecordCandidateFailover(candidate)
        }

        req.ModelName = candidate.Model
        artifact, tokens, err := invokeOnce(candidate, req)
        stage.RecordAttempt(candidate, err, tokens)

        if err == nil {
            return artifact, nil
        }

        if isContractMismatch(err) {
            repaired, repairErr := repairSchema(artifact, stage.Contract)
            stage.RecordContractRepair(repairErr)
            if repairErr == nil {
                return repaired, nil
            }
        }

        if isTimeout(err) && stage.Bundle.CanSplit() {
            return splitAndRetry(stage.Bundle)
        }

        if !retryable(err) {
            break
        }

        waitBackoff(stage.Recovery)
    }

    return nil, stage.LastError()
}
```

同一 attempt 内的 OpenCode continuation 仍然可以保留，但它消耗的是该 attempt 的内部预算。continuation 失败后，stage wrapper 默认决定同 resource fresh retry；只有显式开启候选 failover 且错误属于可用性类时，才允许更换 candidate。

## 8. 与分片拆分的关系

超时不应该总是触发普通 retry。恢复顺序如下：

1. 如果 bundle 有多个 primary unit 或多个文件，先 split，降低单次上下文和推理长度。
2. 如果 bundle 已经不可拆分，在同一 resource 上做 fresh retry。
3. 如果预算耗尽后仍失败，标记该分片失败。

对 `change_review` 常见的“一个文件 / 一个 hunk”分片，split 空间通常很小，因此同 resource fresh retry 是关键兜底。

## 9. Artifact 与状态记录

每次尝试都应保留独立 artifact：

```text
chunk-0-xxx-1-assessment.json
chunk-0-xxx-1-assessment.json.attempt-1.json
chunk-0-xxx-1-assessment.json.attempt-1.output.txt
chunk-0-xxx-1-assessment.json.attempt-2.json
chunk-0-xxx-1-assessment.json.attempt-2.output.txt
```

成功 artifact 仍使用主路径，历史 artifact 只用于排障和审计。当前实现状态：OpenCode continuation 已保留 `.attempt-1` 证据；stage 级 fresh retry 仍复用主输出路径并覆盖前一次 artifact，独立 `attempt-N` artifact 是后续增强项。报告 summary 中应新增 attempt 历史：

```json
{
  "attempts": 3,
  "resource_chain": [
    "opencode-deepseek",
    "opencode-deepseek",
    "opencode-deepseek"
  ],
  "error_classes": [
    "idle_timeout",
    "idle_timeout",
    "none"
  ],
  "resource_failovers": 0,
  "driver_failovers": 0,
  "contract_repairs": 0,
  "queue_wait_ms": [1200, 860, 90],
  "duration_seconds": [600.1, 600.1, 34.2]
}
```

数据库和报告必须统一以下口径：

1. `success` 表示 artifact 完整且通过校验。
2. `degraded` 表示有结果但存在失败分片、缺失 assessment 或覆盖率下降。
3. `failed` 表示任务无法产生有效报告或主要阶段失败。
4. `attempts` 包含普通重试和 failover。
5. `resource_failovers` 表示切换候选 resource 的次数；默认普通重试必须为 0。
6. `driver_failovers` 只表示 invocation 内部的 driver / endpoint 切换，不表示跨 resource 恢复。
7. `contract_repairs` 只表示 schema repair 成功或失败的处理次数。

## 10. Resource 健康度与熔断

调度器应为每个 resource 维护滚动窗口指标：

| 指标 | 用途 |
| --- | --- |
| 队列等待 P95 | 判断是否已经拥塞 |
| `idle_timeout` 率 | 判断模型是否无响应 |
| `output_missing` 率 | 判断 CLI / 模型是否稳定产出 artifact |
| contract 失败率 | 判断结构化输出质量 |
| token 成功率 | 判断是否花费大量 token 后仍失败 |
| P95 / P99 耗时 | 动态调整 attempt 预算 |

建议初始策略：

1. 5 分钟窗口内同 resource 连续 3 次失败，新 chunk 调度临时降权。
2. 连续 5 次失败，熔断 60–120 秒。
3. 已绑定 chunk 的普通重试仍在原 resource 上执行；只有显式开启候选 failover 且该 resource 熔断时才允许切换。
4. 后台 heartbeat 探测成功后自动恢复。

## 11. 灰度与验收

灰度顺序：

1. 先在 `change_review` 的 `specialized assessment` 打开恢复。
2. 再扩展到 Hunter / Judge / Synthesis。
3. 通过灰度开关按 task type、report ID 或 repo 百分比放量。
4. 每次放量比较失败率、P95 耗时、token 成本和修复成功率。

验收指标：

| 指标 | 目标 |
| --- | --- |
| `idle_timeout` 失败分片数 | 相对基线下降 70% 以上 |
| `output_missing` 失败分片数 | 相对基线下降 70% 以上 |
| contract 失败被 repair 成功率 | 80% 以上 |
| 单分片 stage 总耗时 P95 | 不超过基线的 1.2 倍 |
| token 总量 | 不超过基线的 1.3 倍 |
| 报告状态一致性 | 100%：`success` 不得存在未展示的失败分片或缺失 assessment |

## 12. 测试要求

至少覆盖以下测试：

1. 首个 resource 空闲超时，同 resource fresh retry 成功，`resource_failovers` 为 0。
2. 首个 resource output missing，OpenCode continuation 成功，不重复普通 attempt。
3. 首个 resource output missing，continuation 失败，同 resource fresh session 成功。
4. 多文件 bundle 超时，split 成功，不触发 resource 切换。
5. 单文件 bundle 超时，split 不可用，同 resource retry 成功。
6. contract mismatch 不做普通 retry，直接进入 schema repair。
7. schema repair 失败后，若预算允许，在同一 resource 上重新生成。
8. 总预算耗尽后立即终止，不启动新的 attempt。
9. 所有失败分片均写入 `scan_scope_entries` 或等价覆盖事实表。
10. 成功任务不存在缺失 assessment 但状态仍显示完整成功的情况。
11. 显式开启候选 failover 后，仅 `resource_unavailable`、`resource_busy`、`rate_limited` 或 `network_transient` 可以触发切换，且次数不超过配置上限。

## 13. 当前实现

`services/dispatcher/recovery.go` 提供统一的 `RunTierInvocationWithRecovery` 状态机和 `TierRecoveryStats`。该状态机先解析一次 `TierPlan`，普通 retry 始终复用 `Plan.Selected.ResourceID`；只有 `resource_unavailable`、`resource_busy`、`rate_limited` 或 `network_transient` 命中显式白名单且切换预算大于 0 时，才允许访问备选 resource。合同错误不进入普通 retry，由调用方进入定向 schema repair。

Hunter、`specialized assessment`、Challenger、Judge 和 Synthesis 共用 stage deadline。`timeout_seconds` 控制 retry、split、contract repair 和显式 failover 的统一倒计时，Synthesis 也不再叠加旧的外层 retry 循环。配置规范化为同 resource retry 预算 2，`candidate_failover_on` 默认为空，因此默认配置不会发生跨 resource 切换。

Resource 健康度由 dispatcher 记录连续失败和 breaker 状态。连续 5 次失败会打开 90 秒熔断；breaker 打开在恢复状态机中分类为 `resource_unavailable`，不会单独触发候选切换。打开期间不启动 invocation；超过 `open_until` 后资源恢复可用。

`ChunkDetails`、Synthesis summary 与 `DiagnosticsDTO.synthesis` 记录 `resource_failovers`、`resource_chain`、`error_classes`、`queue_wait_ms` 和 `attempt_duration_seconds`。`resource_chain` 按 attempt 展开以便审计；`resource_failovers` 只统计候选 resource 切换；`driver_failovers` 保持为 invocation 内部 endpoint / driver failover 的既有口径。
