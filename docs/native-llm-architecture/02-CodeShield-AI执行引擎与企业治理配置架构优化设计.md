# 02-CodeShield-AI执行引擎与企业治理配置架构优化设计

## 📋 方案元数据与导读

*   **文档编号**：`CS-NATIVE-02`
*   **文档类型**：系统配置架构与算力治理专项设计
*   **当前状态**：`PROPOSED`（架构方案设计完成，待实施评审）
*   **涉及模块**：`models/config.go`、`config.yaml`、`services/dispatcher.go`、`services/governance`、`handlers/admin`
*   **核心目标**：针对 Code-Shield 随架构演进在 `ai`（AI 调度引擎）与 `governance`（企业治理闭环）模块中暴露出的“冷热配置混杂”、“算力供给与业务流水线角色耦合”、“治理策略缺乏多级继承覆盖”、“配置修改需重启服务打断扫描”等痛点，提出以 **“算力资源池（Resources Pool）+ 业务流水线（Pipeline Tiers）+ 微工具路由（Tools）+ 治理生命周期（Governance）”** 为核心的模块化配置重构方案，完整保留并增强多模型独立并发、跨后端映射、Least-Loaded 负载均衡与工作时间自动避峰等核心能力。

---

## 一、 背景与现状剖析：为什么需要优化配置组织？

### 1.1 系统配置的“冷热属性”分界

随着 Code-Shield 从早期单纯依赖 CLI 进程的单一分析工具，演进为 **“重型探索 Agent (Thick Agent) + 轻量原生 LLM (Thin LLM)” 动静分离混合调用架构** 以及 **“跨扫描周期企业级多任务治理闭环”**，系统配置在生命周期与变更频率上呈现出明显的“冷热两极化”：

```mermaid
graph TD
    subgraph ColdConfig ["低频冷配置 (基础设施层 - 启动加载/极少变更)"]
        Server["server: HTTP 端口 / 网络超时 / 数据目录"]
        DB["database: PostgreSQL 驱动 / 连接池 / 凭据"]
        Auth["auth: JWT 密钥 / SSO OAuth2 端点 / 字段映射"]
        Notify["notification: Webhook 回调地址"]
    end

    subgraph HotConfig ["高频易变热配置 (算力供给与业务策略层 - 需敏捷调优/热加载)"]
        AI_Compute["ai.resources: GPU 节点拓扑 / 模型并发槽位 / API Key 凭证"]
        AI_Pipeline["ai.pipeline: Hunter/Judge 阶梯角色绑定 / 辩论流控 / 阶段超时"]
        AI_Throttling["ai.throttling: 工作时间避峰限流 / 手动全局缩放"]
        AI_Tools["ai.tools: JSON修复 / 指纹比对 / 知识提炼微任务路由"]
        Gov_Policy["governance: 缺陷指纹 / 范围守卫 / 负样本注入 / PR 门禁"]
    end
```

### 1.2 现行配置 (`config.yaml`) 的结构痛点

1. **算力供给端 (Compute Provider) 与 业务消费端 (Pipeline Tiers) 概念混杂**：
   * 现存配置中，`ai.backend`（全局 CLI）、`ai.native`（原生端点）、`ai.models`（CLI 多模型并发池）、`ai.tiers`（初筛/辩论/汇总业务角色）与 `ai.tool_backends`（场景工具）平铺在 `ai:` 根节点下。
   * 运维人员在新增物理算力节点或调整某阶段模型时，需跨越 `native`、`models`、`tiers` 等多处字段修改，概念割裂。
2. **多模型独立并发调度能力表达不够直观**：
   * 在底层 [`services/dispatcher.go`](file:///home/fugui/codes/code-shield/services/dispatcher.go) 中，系统原生支持“单台服务器映射多种 CLI 模型”、“不同模型具有独立并发配额”、“Least-Loaded 负载均衡打散”；但当前配置形式（`ai.models` 与 `ai.native.endpoints` 割裂）未能统一抽象“算力节点”实体。
3. **`ai.debate` 与 `ai.tiers` 的从属关系倒置**：
   * `debate`（辩论流控与背压）与 `tiers`（阶梯资源）是平级兄弟字段，但实际上 `tier1_fast`、`tier2_reasoning` 正是对抗辩论流水线的执行阶梯，两者应收拢于统一的流水线编排中。
4. **企业治理策略缺乏多级继承与容量保护**：
   * `governance` 仅提供了 5 个平铺布尔开关，无法区分“全局默认基线”与“任务/项目级定制覆盖”；
   * 研发负样本反馈注入（`feedback_injection`）缺乏最大规则注入上限（Token 预算控制），在历史知识库庞大时存在 Prompt 溢出风险。
5. **运维安全与热加载诉求**：
   * 敏感 API Key 需支持 `${ENV:-default}` 动态占位符；
   * 算力与治理热配置的微调应当支持动态热生效（Hot Reloading），避免重启 `shield-server` 打断长耗时扫描。

---

## 二、 核心重构设计：四层解耦模型

优化后的配置体系遵循 **“职责单一、动静分离、能力不减、平滑演进”** 的原则，将易变部分划分为清晰的 4 个子领域：

```mermaid
graph TD
    subgraph Layer1 ["1. 算力供给资源池 (ai.resources)"]
        R1["算力节点 1: local-gpu-glm5<br/>(opencode/claude, concurrent: 10)"]
        R2["算力节点 2: local-gpu-qwen<br/>(opencode/agy, concurrent: 3)"]
        R3["算力节点 3: vllm-native-primary<br/>(native HTTP, concurrent: 30, weight: 80)"]
        R4["算力节点 4: cloud-deepseek-backup<br/>(native HTTP, concurrent: 5, weight: 20)"]
    end

    subgraph Layer2 ["2. 全局流控与避峰 (ai.throttling)"]
        Throttle["工作时间避峰 (work_hours) & 手动缩放 (manual_scale)<br/>联动 calculateLimit 实时缩放各节点活跃槽位"]
    end

    subgraph Layer3 ["3. 业务流水线与工具路由 (ai.pipeline & ai.tools)"]
        Hunter["Tier 1 Hunter (初筛)<br/>绑定: Thick Agent (agy)"]
        Judge["Tier 2 Reasoning (辩论仲裁)<br/>绑定: Thin LLM (native)"]
        Synthesis["Tier 3 Synthesis (态势汇总)<br/>绑定: Thin LLM (native)"]
        Tools["Utility Tools (微任务)<br/>RepairJSON / FindingMatch -> 默认 native"]
    end

    subgraph Layer4 ["4. 企业质量与记忆闭环 (governance)"]
        FP["指纹防抖 (fingerprint)"]
        LC["生命周期守卫 (lifecycle)"]
        FM["误报记忆注入 (feedback_memory)"]
    end

    Layer1 --> Throttle
    Throttle --> Layer3
    Layer3 --> Layer4
```

### 2.1 算力供给资源池 (`ai.resources`)
统一建模物理/逻辑算力节点实体（Compute Resource Entity）：
* **实体字段**：包含节点唯一名称（`name`）、物理并发配额（`concurrent`）、分流权重（`weight`）、各后端驱动的模型映射（`opencode` / `claude` / `codex` / `agy` / `native`）、以及原生 REST 节点的连接端点（`base_url`、`api_key`）。
* **调度机制**：[`ModelDispatcher`](file:///home/fugui/codes/code-shield/services/dispatcher.go) 依据各节点的 `Active / Limit` 比例执行 **Least-Loaded 负载均衡**，自动保障高并发下的槽位隔离与请求打散。

### 2.2 全局流控与避峰策略 (`ai.throttling`)
* **工作时间自动避峰**：在设定的工作日与时段内自动缩放并发（如 `scale: 0.10` 代表 10% 算力，`scale: 0.0` 代表完全暂停），非工作时间/夜间自动拉满 100%。
* **动态等比缩放**：采用 `calculateLimit(res.Concurrent, scale)` 计算每个算力节点的即时上限，配合后台守护心跳（`startHeartbeat`）实现秒级自愈与唤醒。

### 2.3 业务流水线与阶梯编排 (`ai.pipeline`)
* **流水线总控**：指定流水线模式（`debate`、`single`、`chunked`）及辩论流控（0 候选快速放行 `fast_pass`、阶段硬超时 `stage_timeout_seconds`、跨阶梯背压保护 `backpressure_threshold`）。
* **阶梯角色绑定 (`tiers`)**：
  * **Tier 1 (Hunter 初筛)**：必须使用具备工作区自主探索能力的 **Thick Agent**（如 `agy` / `claude`）；
  * **Tier 2 (Challenger & Judge 辩护与终审)**：推荐使用毫秒级连接复用的 **Thin LLM**（`native`）或深度推理 Agent；
  * **Tier 3 (Synthesis 报告排版汇总)**：推荐使用直传直出的 **Thin LLM**（`native`）。

### 2.4 内置场景微任务路由 (`ai.tools`)
* 专为确定性、单轮无上下文探索的内嵌辅助工具指定路由（如 `repair_json`、`finding_match`、`feedback_extraction`），默认统一切换至 `native` 算力，实现 $<800\text{ms}$ 极速响应。

### 2.5 企业多任务治理与历史记忆闭环 (`governance`)
* **指纹防抖 (`fingerprint`)**：启用跨扫描周期的语义/AST 抗行号抖动指纹，配置相似度比对门限；
* **生命周期守卫 (`lifecycle`)**：扫描范围守卫（`scope_guard_enabled`）杜绝局部假修复，未出现缺陷自动归档（`auto_resolve_missing`），PR 门禁严格增量模式（`diff_gate_strict`）；
* **研发误报记忆 (`feedback_memory`)**：支持负样本规则自动注入 Prompt，并引入 `max_rules_injected`（单次最大注入规则数）防止 Prompt 上下文溢出。

---

## 三、 完整规范模版 (YAML Specification)

```yaml
# ==============================================================================
# Code-Shield 🛡️ 服务端配置规范 (重构推荐标准)
# ==============================================================================

# ── 1. HTTP 服务与数据库基础层 (低频冷配置) ──
server:
  port: ":8080"                         # HTTP 服务监听端口
  data_dir: "./data"                    # 运行时数据根目录 (自动存放 codes/ 与 reports/)
  # external_url: "http://192.168.56.18:8080" # 外部访问基准 URL (邮件与外链跳转)
  # worker_count: 5                     # 全局任务并发数 (缺省时自动按算力池总并发折中推导)
  # max_queue_size: 2000                # 待处理任务排队上限 (-1 为无上限)
  read_timeout: 120s                    # 读取请求超时
  write_timeout: 120s                   # 写入响应超时
  idle_timeout: 180s                    # 空闲连接保持超时

database:
  driver: "postgres"
  host: "127.0.0.1"
  port: 5432
  user: "code_shield"
  password: "${DB_PASSWORD:-code_shield_password}" # 支持环境变量动态注入
  dbname: "code_shield"
  sslmode: "disable"
  timezone: "Asia/Shanghai"
  max_open_conns: 50
  max_idle_conns: 10

# ==============================================================================
# 2. AI 执行引擎：算力供给、流控与流水线编排 (高频易变热配置)
# ==============================================================================
ai:
  default_backend: "agy"               # 全局兜底 Backend (agy / claude / opencode / codex / native)
  debug_logs: false                    # 是否输出底层调试交互日志
  mock_on_missing_cli: false           # CLI 未就绪时是否阻断或模拟 (生产建议 false)

  # ────────────────────────────────────────────────────────────────────────────
  # 2.1 算力供给资源池 (Compute Resources Pool)
  # 纳管所有物理/逻辑 LLM 算力节点。每个节点具备独立的模型名称与并发配额。
  # 调度器自动基于 Least-Loaded (最小活跃数) 做节点级负载均衡与槽位隔离。
  # ────────────────────────────────────────────────────────────────────────────
  resources:
    # 算力节点 1: 内网主 GPU 节点 (提供 glm5.1，多个 CLI 共享 10 并发)
    - name: "local-gpu-glm5"
      concurrent: 10                   # 该节点允许的最大物理并发
      opencode: "models/glm5.1"        # opencode CLI 对应的模型标识
      claude: "glm5.1"                 # claude CLI 对应的模型标识
      agy: "glm5.1"                    # agy 对应的模型标识

    # 算力节点 2: 内网轻量节点 (小模型，单独限制 3 并发)
    - name: "local-gpu-qwen"
      concurrent: 3
      opencode: "models/qwen3.5"
      agy: "qwen-3.5-coder"

    # 算力节点 3: 原生 Thin LLM REST API 集群 (HTTP/2 直连，大并发)
    - name: "vllm-native-primary"
      backend: "native"
      native: "glm-4-flash"            # 绑定的 Native 模型名称
      base_url: "http://192.168.56.18:8000/v1/chat/completions"
      api_key: "${NATIVE_PRIMARY_KEY:-sk-internal}"
      concurrent: 30                   # 专有 30 高并发槽位
      weight: 80                       # 多 Native 节点分流权重
      response_format_json: true       # 强制 JSON Mode 约束
      max_retries: 3                   # 最大重试次数
      retry_backoff_ms: 500            # 指数退避基准时长 (毫秒)

    # 算力节点 4: 云端兜底推理大模型 (冷备节点)
    - name: "cloud-deepseek-backup"
      backend: "native"
      native: "deepseek-chat"
      base_url: "https://api.deepseek.com/v1/chat/completions"
      api_key: "${DEEPSEEK_API_KEY}"
      concurrent: 5
      weight: 20

  # ────────────────────────────────────────────────────────────────────────────
  # 2.2 全局流控与避峰策略 (Throttling & Work Hours)
  # 联动所有 resources，按比例实时动态缩放各算力节点的 active 限制 (calculateLimit)
  # ────────────────────────────────────────────────────────────────────────────
  throttling:
    work_hours:
      enabled: true                    # 开启工作时间自动限流
      workdays: [1, 2, 3, 4, 5]        # 生效星期 (周一至周五)
      start_time: "09:00"              # 限流开始时刻
      end_time: "22:00"                # 限流结束时刻
      scale: 0.10                      # 工作时间算力比例 (0.10 代表 10%，0.0 代表暂停)

  # ────────────────────────────────────────────────────────────────────────────
  # 2.3 业务流水线与阶段编排 (Debate Pipeline & Workflow Tiers)
  # 动静分离：将业务角色（Hunter/Judge/Synthesis）与算力模型进行解耦绑定
  # ────────────────────────────────────────────────────────────────────────────
  pipeline:
    mode: "debate"                     # 流水线模式：debate (对抗辩论) / single / chunked
    debate:
      fast_pass_enabled: true          # 0 候选快速放行 (节省 80%+ 辩论算力)
      max_candidates_per_chunk: 20     # 单分片最大仲裁候选数
      stage_timeout_seconds: 1800      # 单阶段全局硬超时兜底 (秒)
      backpressure_threshold: 30       # 跨 Tier 背压触发积压阈值
      backpressure_timeout_seconds: 120 # 背压超时兜底 (秒)
      log_retention_days: 30           # 辩论轨迹日志保留天数

    # 各阶段阶梯资源与模型绑定
    tiers:
      tier1_hunter:                    # Tier 1 快模型初筛 (必须 Thick Agent，自主遍历源码)
        backend: "agy"
        model: "gemini-3.7-flash"      # 可指定特定模型；未指定则在 resources 中 Least-Loaded 匹配
        concurrent: 5
        timeout_seconds: 1200          # 初筛单片超时 (秒)

      tier2_reasoning:                 # Tier 2 辩护与终审 (Challenger & Judge，深度逻辑推理)
        backend: "native"              # 可选 native (推荐 Thin LLM) 或 agy (Thick Agent)
        model: "deepseek-chat"
        concurrent: 10
        timeout_seconds: 600           # 辩论仲裁单片超时 (秒)

      tier3_synthesis:                 # Tier 3 全仓报告汇总与态势总结 (纯文本聚合)
        backend: "native"              # 推荐 native 直传直出
        model: "glm-4-flash"
        concurrent: 5
        timeout_seconds: 300           # 报告汇总超时 (秒)

  # ────────────────────────────────────────────────────────────────────────────
  # 2.4 内置场景微任务路由 (Utility Tools Routing)
  # 确定性单轮无文件探索任务指定执行方式 (未指定时统一 fallback 到 default_backend)
  # ────────────────────────────────────────────────────────────────────────────
  tools:
    default_backend: "native"
    repair_json: "native"              # JSON 语法修复
    finding_match: "native"            # 跨周期缺陷指纹语义比对
    feedback_extraction: "native"      # 研发标记误报规则提炼

# ==============================================================================
# 3. 企业多任务治理与历史记忆闭环 (Enterprise Governance & SSOT Memory)
# ==============================================================================
governance:
  # 3.1 跨扫描缺陷抗抖动指纹
  fingerprint:
    enabled: true                      # 启用抗行号抖动的语义缺陷指纹
    similarity_threshold: 0.85         # 跨版本模糊匹配相似度门限

  # 3.2 缺陷生命周期与门禁状态
  lifecycle:
    scope_guard_enabled: true          # 扫描范围守卫 (杜绝局部扫描产生的假修复)
    auto_resolve_missing: true         # 守卫范围内消失的缺陷自动标记为 RESOLVED
    diff_gate_strict: true             # PR/MR 门禁流水线仅阻断 [NEW] 增量缺陷

  # 3.3 研发误报记忆与反馈注入
  feedback_memory:
    injection_enabled: true            # 自动将研发标记的误报规则注入 Prompt
    max_rules_injected: 10             # 单次扫描最大注入负样本规则数 (防 Prompt 溢出)

# ==============================================================================
# 4. 通知与认证服务 (Notification & Enterprise SSO)
# ==============================================================================
notification:
  webhook: "http://127.0.0.1:8081/api/notify/email"

auth:
  standalone_mode: false               # 是否以独立系统模式运行
  jwt_secret: "${JWT_SECRET}"          # JWT 签名密钥
  password_login_enabled: true         # 是否启用密码登录
  oauth2:
    enabled: true                      # 企业 SSO 单点登录
    client_id: "code-shield"
    client_secret: "${OAUTH_SECRET}"
    auth_url: "https://sso.yourcompany.com/realms/main/protocol/openid-connect/auth"
    token_url: "https://sso.yourcompany.com/realms/main/protocol/openid-connect/token"
    userinfo_url: "https://sso.yourcompany.com/realms/main/protocol/openid-connect/userinfo"
    redirect_url: ""                   # 留空自动推导
    scopes: ["openid", "profile", "email"]
    admin_list: ["admin@company.com"]
    field_mapping:
      username: "preferred_username"
      email: "email"
      name: "name"
      employee_id: "employee_id"
      unique_id: "unique_id"
      employee_type: "employee_type"
```

---

## 四、 Go 结构体与向后兼容设计 (`models/config.go`)

为了确保平滑过渡，Go 数据结构定义采用“新版标准结构 + 旧版字段平滑兼容（UnmarshalYAML Fallback）”设计：

```go
package models

// ComputeResourceConfig 单个算力节点实体定义
type ComputeResourceConfig struct {
	Name               string  `yaml:"name" json:"name"`
	Concurrent         int     `yaml:"concurrent" json:"concurrent"`
	Weight             int     `yaml:"weight" json:"weight"`
	Backend            string  `yaml:"backend" json:"backend"` // 用于 native 节点
	OpenCode           string  `yaml:"opencode" json:"opencode"`
	Claude             string  `yaml:"claude" json:"claude"`
	Codex              string  `yaml:"codex" json:"codex"`
	Agy                string  `yaml:"agy" json:"agy"`
	Native             string  `yaml:"native" json:"native"`
	BaseURL            string  `yaml:"base_url" json:"base_url"`
	APIKey             string  `yaml:"api_key" json:"api_key"`
	ResponseFormatJSON bool    `yaml:"response_format_json" json:"response_format_json"`
	MaxRetries         int     `yaml:"max_retries" json:"max_retries"`
	RetryBackoffMs     int     `yaml:"retry_backoff_ms" json:"retry_backoff_ms"`
}

// ThrottlingConfig 流控策略配置
type ThrottlingConfig struct {
	WorkHours WorkHoursThrottleConfig `yaml:"work_hours" json:"work_hours"`
}

// PipelineTiersConfig 阶梯资源配置
type PipelineTiersConfig struct {
	Tier1Hunter    TierConfig `yaml:"tier1_hunter" json:"tier1_hunter"`
	Tier2Reasoning TierConfig `yaml:"tier2_reasoning" json:"tier2_reasoning"`
	Tier3Synthesis TierConfig `yaml:"tier3_synthesis" json:"tier3_synthesis"`
}

// PipelineConfig 业务流水线编排
type PipelineConfig struct {
	Mode   string              `yaml:"mode" json:"mode"`
	Debate DebateFlowConfig    `yaml:"debate" json:"debate"`
	Tiers  PipelineTiersConfig `yaml:"tiers" json:"tiers"`
}

// ToolsConfig 微任务工具路由
type ToolsConfig struct {
	DefaultBackend     string `yaml:"default_backend" json:"default_backend"`
	RepairJSON         string `yaml:"repair_json" json:"repair_json"`
	FindingMatch       string `yaml:"finding_match" json:"finding_match"`
	FeedbackExtraction string `yaml:"feedback_extraction" json:"feedback_extraction"`
}

// GovernancePolicyConfig 企业治理策略定义
type GovernancePolicyConfig struct {
	Fingerprint struct {
		Enabled             bool    `yaml:"enabled" json:"enabled"`
		SimilarityThreshold float64 `yaml:"similarity_threshold" json:"similarity_threshold"`
	} `yaml:"fingerprint" json:"fingerprint"`

	Lifecycle struct {
		ScopeGuardEnabled  bool `yaml:"scope_guard_enabled" json:"scope_guard_enabled"`
		AutoResolveMissing bool `yaml:"auto_resolve_missing" json:"auto_resolve_missing"`
		DiffGateStrict     bool `yaml:"diff_gate_strict" json:"diff_gate_strict"`
	} `yaml:"lifecycle" json:"lifecycle"`

	FeedbackMemory struct {
		InjectionEnabled bool `yaml:"injection_enabled" json:"injection_enabled"`
		MaxRulesInjected int  `yaml:"max_rules_injected" json:"max_rules_injected"`
	} `yaml:"feedback_memory" json:"feedback_memory"`
}
```

### 4.1 向后兼容处理（Backward Compatibility）
在 `LoadConfig` 中自动执行平滑映射：
1. 若配置文件包含旧版 `ai.models`，自动转换为 `ai.resources`；
2. 若配置文件包含旧版 `ai.tiers.tier1_fast`，自动映射到 `ai.pipeline.tiers.tier1_hunter`；
3. 若配置文件包含旧版 `ai.tool_backends`，自动同步到 `ai.tools`；
4. 若配置文件包含旧版平铺 `governance` 布尔值，自动映射到 `governance.fingerprint`、`governance.lifecycle` 与 `governance.feedback_memory`。

---

## 五、 高级演进：动态热重载机制 (Dynamic Hot Reloading)

为了解决“调算力、切模型需重启服务”的痛点，系统可引入轻量级配置热重载架构：

```mermaid
sequenceDiagram
    autonumber
    actor Admin as 运维人员 / 管理端
    participant Watcher as fsnotify 配置文件监听器
    participant AdminAPI as /api/admin/config/reload API
    participant Loader as LoadConfig() 解析器
    participant Dispatcher as ModelDispatcher 调度器
    participant Workers as 正在运行的分片扫描 Goroutines

    alt 方式 A: 文件修改自动触发
        Admin->>Watcher: 保存修改后的 config.yaml
        Watcher->>Loader: 触发重载信号
    else 方式 B: REST API 手动刷新
        Admin->>AdminAPI: POST /api/admin/config/reload
        AdminAPI->>Loader: 触发重载信号
    end

    Loader->>Loader: 解析并校验新配置 (含 ENV 占位符)
    Loader->>Dispatcher: 调用 Dispatcher.ReloadResources(newResources)
    Dispatcher->>Dispatcher: 动态更新 resources 槽位上限与模型映射
    Dispatcher->>Dispatcher: cond.Broadcast() 唤醒等待协程
    Workers-->>Dispatcher: 按最新算力配额继续执行 (零停机/零中断)
```

1. **热加载安全范围**：
   * **允许热重载**：`ai.resources`（算力节点增删/并发调整）、`ai.throttling`（限流时段与比例）、`ai.pipeline.debate`（超时与背压门限）、`governance`（规则阈值）。
   * **需重启生效**：`server.port`、`database.*`。
2. **零中断保障**：`ReloadResources` 仅更新槽位定义与 `Limit`，已在执行中的子进程/HTTP 请求保持执行直到完成，新到请求立即使用最新算力池。

---

## 六、 方案收益总结对比

| 评估维度 | 现行配置架构 (`Current`) | 重构后配置架构 (`Proposed`) | 核心收益 |
| :--- | :--- | :--- | :--- |
| **算力资源管理** | `models` / `native.endpoints` 割裂，概念不清 | 统一收拢为 `ai.resources` 实体算力池 | 统一管理多 CLI 与 Native 节点，清晰支持独立并发与 Least-Loaded 负载均衡。 |
| **流水线编排** | `debate` 与 `tiers` 平级，缺少上下文聚合 | 收拢为 `ai.pipeline` 流水线编排 | 业务角色（Hunter/Judge/Synthesis）与算力解耦，流控策略一目了然。 |
| **微工具扩展** | 写死 3 个孤立场景 | 统一在 `ai.tools` 下规范化命名 | 具备统一的 `default_backend` 兜底，便于未来无缝新增内嵌微任务。 |
| **企业治理策略** | 仅 5 个平铺布尔开关 | 分层为 `fingerprint`、`lifecycle`、`feedback_memory` | 增加 `max_rules_injected` 等保护参数，防止 Prompt 爆炸，支持项目级覆盖。 |
| **运维与安全性** | 敏感 Key 易明文泄露，改配置需重启 | 支持 `${ENV}` 占位符 + 动态配置热重载（Hot Reload） | 提高安全性，修改算力与阈值无需重启服务，扫描任务零中断。 |
