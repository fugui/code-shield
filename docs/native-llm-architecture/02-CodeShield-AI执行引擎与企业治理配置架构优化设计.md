# 02-CodeShield-AI执行引擎与企业治理配置架构优化设计

## 📋 方案元数据与导读

*   **文档编号**：`CS-NATIVE-02`
*   **文档类型**：系统配置架构与算力治理专项设计
*   **当前状态**：`PROPOSED`（架构方案设计完成，待实施评审）
*   **涉及模块**：`models/config.go`、`config.yaml`、`services/dispatcher.go`、`services/queue.go`、`services/native_cli.go`、`services/governance`
*   **核心目标**：针对 Code-Shield 随架构演进在 `ai`（AI 调度引擎）与 `governance`（企业治理闭环）模块中暴露出的“冷热配置混杂”、“任务扫描并发混在 HTTP Server 中”、“算力供给与业务消费角色耦合”、“治理策略缺乏多级继承覆盖”、“配置修改需重启服务打断扫描”等痛点，提出以 **“任务调度与精准算力池（Task Workers & Native Endpoints）+ 对抗辩论流水线（Debate Pipeline）+ 微任务工具（Tools）+ 治理生命周期（Governance）”** 为核心的扁平模块化重构方案，消除过度抽象，实现结构清晰、零歧义精准绑定，并完整保留多模型独立并发、Native 算力集群负载分流与工作时间自动避峰等核心能力。

---

## 一、 背景与现状剖析：为什么需要优化配置组织？

### 1.1 系统配置的“冷热属性”分界

随着 Code-Shield 从早期单纯依赖 CLI 进程的单一分析工具，演进为 **“重型探索 Agent (Thick Agent) + 轻量原生 LLM (Thin LLM)” 动静分离混合调用架构** 以及 **“跨扫描周期企业级多任务治理闭环”**，系统配置在生命周期与变更频率上呈现出明显的“冷热两极化”：

```mermaid
graph TD
    subgraph ColdConfig ["低频冷配置 (基础设施层 - 启动加载/极少变更)"]
        Server["server: HTTP 端口 / 网络读写超时 / 数据目录"]
        DB["database: PostgreSQL 驱动 / 连接池 / 凭据"]
        Auth["auth: JWT 密钥 / SSO OAuth2 端点 / 字段映射"]
        Notify["notification: Webhook 回调地址"]
    end

    subgraph HotConfig ["高频易变热配置 (算力供给与业务策略层 - 需敏捷调优/热加载)"]
        AI_Tasks["ai.worker_count / max_queue_size: 扫描任务并发与排队队列"]
        AI_Compute["ai.resources: Thick Agent 节点 / Native 算力集群 endpoints / 独立并发配额"]
        AI_Debate["ai.debate: Hunter/Judge 阶梯角色绑定 / 辩论流控 / 阶段超时"]
        AI_Throttling["ai.throttling: 工作时间避峰限流 / 动态等比缩放"]
        AI_Tools["ai.tools: JSON修复 / 指纹比对 / 知识提炼微任务直连 native"]
        Gov_Policy["governance: 缺陷指纹 / 范围守卫 / 负样本注入 / PR 门禁"]
    end
```

### 1.2 现行配置 (`config.yaml`) 的结构痛点

1. **扫描任务并发混在 HTTP Server 配置中**：
   * 现存配置中，`server.worker_count`（扫描任务并发数）与 `server.max_queue_size`（排队上限）放在 `server:` 块中，容易误导为 HTTP 请求处理并发；
   * 实际上 `worker_count` 本质是 **AI 扫描任务工作池（Task Workers）**，其默认值甚至根据 LLM 算力池总并发自动折中推导 `(sum_concurrent + 1) / 2`，理应归属于 `ai:` 统一调度。
2. **算力供给端 (Compute Provider) 与 业务消费端 (Debate Tiers) 概念混杂**：
   * 现存配置中，`ai.backend`（全局 CLI）、`ai.native`（原生端点）、`ai.models`（CLI 多模型并发池）、`ai.tiers`（初筛/辩论/汇总业务角色）与 `ai.tool_backends`（场景工具）平铺在 `ai:` 根节点下，概念割裂。
3. **Native 引擎的集群能力与消费绑定关系不够清晰**：
   * 原生 Thin LLM 引擎（`NativeInvoker`）在底层支持多 `endpoints` 权重分流与 Failover，但在顶层配置中与消费端（`tiers` / `tool_backends`）的对应关系不够直观；
   * 消费端期望直接以确定性的 `resource: "native"` 进行显式引用，由 Native 引擎自身内部闭环管理其多个算力节点。
4. **流水线流控与执行阶梯割裂**：
   * `debate`（辩论流控与背压）与 `tiers`（阶梯资源）过去作为同级字段分散存在，实际上 `tier1_hunter`、`tier2_reasoning` 正是对抗辩论流水线的执行阶梯，两者应扁平收拢在 `ai.debate` 中。
5. **企业治理策略缺乏多级继承与容量保护**：
   * `governance` 仅提供了 5 个平铺布尔开关，无法区分“全局默认基线”与“任务/项目级定制覆盖”；
   * 研发负样本反馈注入（`feedback_injection`）缺乏最大规则注入上限（Token 预算控制），在历史知识库庞大时存在 Prompt 溢出风险。
6. **运维安全与热加载诉求**：
   * 敏感 API Key 需支持 `${ENV:-default}` 动态占位符；
   * 算力与治理热配置的微调应当支持动态热生效（Hot Reloading），避免重启 `shield-server` 打断长耗时扫描。

---

## 二、 核心重构设计：立体三级并发与解耦模型

优化后的配置体系将 AI 扫描并发控制清晰划分为 **三级立体层次**，使基础设施与业务策略彻底解耦：

```mermaid
graph TD
    subgraph Layer1 ["Level 1: 仓级任务调度并发 (ai.worker_count & max_queue_size)"]
        W1["Worker Pool: 任务工作协程 (同时执行 N 个代码仓分析)"]
    end

    subgraph Layer2 ["Level 2: 辩论阶梯流控与分片 (ai.debate.tiers)"]
        T1["Tier 1 Hunter (初筛) -> 绑定 agy-gemini"]
        T2["Tier 2 Reasoning (辩论仲裁) -> 绑定 native"]
        T3["Tier 3 Synthesis (态势汇总) -> 绑定 native"]
    end

    subgraph Layer3 ["Level 3: 物理算力供给池 (ai.resources)"]
        R1["id: agy-gemini (concurrent: 5)"]
        R2["id: opencode-glm5 (concurrent: 10)"]
        R_Native["id: native (Thin LLM 集群)<br/>├── vllm-primary (concurrent: 30, weight: 80)<br/>└── deepseek-backup (concurrent: 5, weight: 20)"]
    end

    W1 -->|派发分片| Layer2
    Layer2 -->|申请槽位| Layer3
```

### 2.1 任务调度并发 (`ai.worker_count` & `ai.max_queue_size`)
* **`ai.worker_count`**：全局代码仓分析 Worker 数量，决定系统同时拉起多少个代码仓的分析流水线。缺省时由算力池并发总和自适应推导：$(sum\_concurrent + 1) / 2$。
* **`ai.max_queue_size`**：DB/内存队列待扫描任务排队上限，超限时保护系统返回 HTTP 429。

### 2.2 算力节点资源池 (`ai.resources`)
* **Thick Agent 节点**：每个 CLI 模型定义为一个确定的 resource 节点（包含唯一的 `id`、`driver`、`model`、`concurrent`）。
* **Native Thin LLM 引擎节点**：统一固定为 **`id: "native"`**，内部包含全局调用选项（`response_format_json`、`max_retries`、`retry_backoff_ms`），并通过 **`endpoints` 数组** 支持配置多个异构算力节点（各自拥有独立 `base_url`、`api_key`、`model`、`concurrent` 与权重 `weight`）。

### 2.3 多智能体对抗辩论流水线 (`ai.debate`)
* **拒绝模式嵌套**：系统核心扫描流水线即为辩论流水线，去除非必要的 `pipeline.mode: debate` 嵌套，直接使用 `ai.debate` 统管流控与阶梯。
* **100% 显式精确绑定 (`ai.debate.tiers`)**：每个业务阶段（`tier1_hunter`、`tier2_reasoning`、`tier3_synthesis`）通过 **`resource: "<id>"`** 直接引用资源池中的节点 ID，彻底消除模糊匹配与暗箱猜测逻辑。
* **业务阶段超时与流控**：各阶段独立配置 `timeout_seconds`，流水线全局配置 `fast_pass_enabled`（0 候选快速放行）、`backpressure_threshold`（跨阶梯背压阈值）与 `stage_timeout_seconds`。

### 2.4 微任务工具消费端 (`ai.tools`)
* 专为确定性、单轮无上下文探索的内嵌辅助工具指定路由（如 `repair_json`、`finding_match`、`feedback_extraction`），统一默认指定 **`default_resource: "native"`**，直连原生集群实现 $<800\text{ms}$ 极速响应。

### 2.5 全局流控与避峰策略 (`ai.throttling`)
* **工作时间自动避峰**：在设定的工作日与时段内自动缩放并发（如 `scale: 0.10` 代表 10% 算力），夜间自动拉满 100%。
* **动态等比缩放**：采用 `calculateLimit(res.Concurrent, scale)` 计算每个算力节点的即时上限，配合后台自愈守护心跳实现秒级唤醒。

### 2.6 企业多任务治理与历史记忆闭环 (`governance`)
* **指纹防抖 (`fingerprint`)**：启用跨扫描周期的语义/AST 抗行号抖动指纹，配置相似度比对门限；
* **生命周期守卫 (`lifecycle`)**：扫描范围守卫（`scope_guard_enabled`）杜绝局部假修复，未出现缺陷自动归档（`auto_resolve_missing`），PR 门禁严格增量模式（`diff_gate_strict`）；
* **研发误报记忆 (`feedback_memory`)**：支持负样本规则自动注入 Prompt，并引入 `max_rules_injected`（单次最大注入规则数）防止 Prompt 上下文溢出。

---

## 三、 完整规范模版 (YAML Specification)

```yaml
# ==============================================================================
# Code-Shield 🛡️ 服务端配置规范 (重构推荐标准)
# ==============================================================================

# ── 1. HTTP 服务与数据库基础层 (纯粹的低频冷配置) ──
server:
  port: ":8080"                         # HTTP 服务监听端口
  data_dir: "./data"                    # 运行时数据根目录 (自动存放 codes/ 与 reports/)
  external_url: "http://192.168.56.18:8080" # 外部访问基准 URL (邮件与外链跳转)
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
# 2. AI 执行引擎：任务并发、算力池、辩论流水线与工具 (高频易变热配置)
# ==============================================================================
ai:
  # ────────────────────────────────────────────────────────────────────────────
  # 2.1 全局扫描任务队列与并发 (Task Queue & Workers)
  # ────────────────────────────────────────────────────────────────────────────
  worker_count: 5                      # 全局扫描任务并发 Worker 数 (缺省自动按算力池总并发折中推导)
  max_queue_size: 2000                 # 待处理扫描任务排队最大上限 (超限返回 HTTP 429，-1 为无上限)
  default_resource: "native"           # 全局兜底 Resource ID
  debug_logs: false                    # 是否输出底层调试交互日志
  mock_on_missing_cli: false           # CLI 未就绪时是否阻断或模拟 (生产建议 false)

  # ────────────────────────────────────────────────────────────────────────────
  # 2.2 算力节点资源池 (Compute Resources Pool)
  # 统一定义所有可用的物理/逻辑算力节点。每个节点拥有唯一的 id 与独立并发上限
  # ────────────────────────────────────────────────────────────────────────────
  resources:
    # 节点 1: Antigravity CLI 探索型 Thick Agent (初筛主力)
    - id: "agy-gemini"
      driver: "agy"                     # 驱动类型: agy / opencode / claude / codex / native
      model: "gemini-3.7-flash"
      concurrent: 5                     # 该节点最大物理并发槽位

    # 节点 2: OpenCode GLM5.1 算力节点 (备选 Thick Agent)
    - id: "opencode-glm5"
      driver: "opencode"
      model: "models/glm5.1"
      concurrent: 10

    # 节点 3: 原生 Thin LLM 执行引擎 (统一 ID 为 native，内含算力集群 endpoints)
    - id: "native"
      driver: "native"
      response_format_json: true       # 强制 JSON Mode 约束
      max_retries: 3                   # 最大重试次数
      retry_backoff_ms: 500            # 指数退避基准时长 (毫秒)

      # ── 多端点算力集群 (支持负载打散、权重分流与故障转移) ──
      endpoints:
        - name: "local-vllm-primary"    # 内网主 GPU 节点
          base_url: "http://192.168.56.18:8000/v1/chat/completions"
          api_key: "${NATIVE_PRIMARY_KEY:-sk-internal}"
          model: "glm-4-flash"
          concurrent: 30                # 专有 30 高并发槽位
          weight: 80                    # 负载权重 80%

        - name: "cloud-deepseek-backup"  # 云端备用推理节点 (异构大模型)
          base_url: "https://api.deepseek.com/v1/chat/completions"
          api_key: "${DEEPSEEK_API_KEY}"
          model: "deepseek-chat"
          concurrent: 5
          weight: 20

  # ────────────────────────────────────────────────────────────────────────────
  # 2.3 全局流控与避峰策略 (Throttling & Work Hours)
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
  # 2.4 多智能体对抗辩论流水线 (Debate Pipeline)
  # 统管辩论流控参数与各阶段阶梯算力绑定
  # ────────────────────────────────────────────────────────────────────────────
  debate:
    enabled: true                      # 是否启用多智能体对抗辩论流水线
    fast_pass_enabled: true            # 0 候选快速放行 (节省 80%+ 辩论算力)
    max_candidates_per_chunk: 20       # 单分片最大仲裁候选数
    stage_timeout_seconds: 1800        # 单阶段全局硬超时兜底 (秒)
    backpressure_threshold: 30         # 跨 Tier 背压触发积压阈值
    backpressure_timeout_seconds: 120  # 背压超时兜底 (秒)
    log_retention_days: 30             # 辩论轨迹日志保留天数

    # 各阶段阶梯显式绑定 resource
    tiers:
      tier1_hunter:                    # Tier 1 快模型初筛 (必须 Thick Agent，自主遍历源码)
        resource: "agy-gemini"         # 显式精确绑定 agy-gemini 节点
        timeout_seconds: 1200          # 初筛单片超时 (秒)

      tier2_reasoning:                 # Tier 2 辩护与终审 (Challenger & Judge，深度逻辑推理)
        resource: "native"             # 显式精确绑定 native 原生集群
        timeout_seconds: 600           # 辩论仲裁单片超时 (秒)

      tier3_synthesis:                 # Tier 3 全仓报告汇总与态势总结 (纯文本聚合)
        resource: "native"             # 显式精确绑定 native 原生集群
        timeout_seconds: 300           # 报告汇总超时 (秒)

  # ────────────────────────────────────────────────────────────────────────────
  # 2.5 内置场景微任务路由 (Utility Tools Routing)
  # 确定性单轮无文件探索任务统一直连 native 原生集群
  # ────────────────────────────────────────────────────────────────────────────
  tools:
    default_resource: "native"         # 默认统一走 native
    overrides:                         # 特殊微任务可按需覆写
      repair_json: "native"
      finding_match: "native"
      feedback_extraction: "native"

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

// ResourceEndpointConfig 原生算力端点配置
type ResourceEndpointConfig struct {
	Name        string  `yaml:"name" json:"name"`
	BaseURL     string  `yaml:"base_url" json:"base_url"`
	APIKey      string  `yaml:"api_key" json:"api_key"`
	Model       string  `yaml:"model" json:"model"`
	Concurrent  int     `yaml:"concurrent" json:"concurrent"`
	Weight      int     `yaml:"weight" json:"weight"`
	Temperature float64 `yaml:"temperature" json:"temperature"`
}

// ComputeResourceConfig 算力节点实体定义
type ComputeResourceConfig struct {
	ID                 string                   `yaml:"id" json:"id"`
	Driver             string                   `yaml:"driver" json:"driver"` // agy / opencode / claude / codex / native
	Model              string                   `yaml:"model" json:"model"`
	Concurrent         int                      `yaml:"concurrent" json:"concurrent"`
	BaseURL            string                   `yaml:"base_url" json:"base_url"`       // 单节点 native 简写
	APIKey             string                   `yaml:"api_key" json:"api_key"`         // 单节点 native 简写
	ResponseFormatJSON bool                     `yaml:"response_format_json" json:"response_format_json"`
	MaxRetries         int                      `yaml:"max_retries" json:"max_retries"`
	RetryBackoffMs     int                      `yaml:"retry_backoff_ms" json:"retry_backoff_ms"`
	Endpoints          []ResourceEndpointConfig `yaml:"endpoints" json:"endpoints"`     // native 集群端点
}

// TierBindingConfig 阶梯绑定配置
type TierBindingConfig struct {
	Resource       string `yaml:"resource" json:"resource"`               // 引用的 Resource ID
	TimeoutSeconds int    `yaml:"timeout_seconds" json:"timeout_seconds"` // 超时时限 (秒)
}

// DebateTiersConfig 辩论阶梯流水线配置
type DebateTiersConfig struct {
	Tier1Hunter    TierBindingConfig `yaml:"tier1_hunter" json:"tier1_hunter"`
	Tier2Reasoning TierBindingConfig `yaml:"tier2_reasoning" json:"tier2_reasoning"`
	Tier3Synthesis TierBindingConfig `yaml:"tier3_synthesis" json:"tier3_synthesis"`
}

// DebateConfig 辩论流水线流控与阶梯配置
type DebateConfig struct {
	Enabled                    bool              `yaml:"enabled" json:"enabled"`
	FastPassEnabled            bool              `yaml:"fast_pass_enabled" json:"fast_pass_enabled"`
	MaxCandidatesPerChunk      int               `yaml:"max_candidates_per_chunk" json:"max_candidates_per_chunk"`
	StageTimeoutSeconds        int               `yaml:"stage_timeout_seconds" json:"stage_timeout_seconds"`
	LogRetentionDays           int               `yaml:"log_retention_days" json:"log_retention_days"`
	BackpressureThreshold      int               `yaml:"backpressure_threshold" json:"backpressure_threshold"`
	BackpressureTimeoutSeconds int               `yaml:"backpressure_timeout_seconds" json:"backpressure_timeout_seconds"`
	Tiers                      DebateTiersConfig `yaml:"tiers" json:"tiers"`
}

// ToolsConfig 微任务工具路由
type ToolsConfig struct {
	DefaultResource string            `yaml:"default_resource" json:"default_resource"`
	Overrides       map[string]string `yaml:"overrides" json:"overrides"`
}

// AIConfig AI 核心调度与算力配置
type AIConfig struct {
	WorkerCount      int                     `yaml:"worker_count" json:"worker_count"`           // 扫描任务并发 Worker 数
	MaxQueueSize     int                     `yaml:"max_queue_size" json:"max_queue_size"`       // 待处理任务排队上限
	DefaultResource  string                  `yaml:"default_resource" json:"default_resource"`   // 默认兜底 Resource ID
	DebugLogs        bool                    `yaml:"debug_logs" json:"debug_logs"`
	MockOnMissingCLI *bool                   `yaml:"mock_on_missing_cli" json:"mock_on_missing_cli"`
	Resources        []ComputeResourceConfig `yaml:"resources" json:"resources"`                 // 算力节点资源池
	Throttling       ThrottlingConfig        `yaml:"throttling" json:"throttling"`               // 工作时间自动限流
	Debate           DebateConfig            `yaml:"debate" json:"debate"`                       // 辩论流水线与阶梯
	Tools            ToolsConfig             `yaml:"tools" json:"tools"`                         // 微任务工具路由
}
```

### 4.1 向后兼容处理（Backward Compatibility）
在 `LoadConfig` 中自动执行平滑映射：
1. **任务并发兼容**：若 `ai.worker_count` 未配置，自动平滑读取 `server.worker_count`；若两者均未配置，按 `(sumConcurrent + 1) / 2` 自动推导；
2. **算力节点兼容**：若配置文件包含旧版 `ai.models`，自动转换为 `ai.resources` 列表；
3. **原生端点兼容**：若配置文件包含旧版 `ai.native`（含 `endpoints`），自动封装为 `id: "native"` 实体注入 `resources`；
4. **阶梯配置兼容**：若配置文件包含旧版 `ai.tiers.tier1_fast`，自动转换为 `ai.debate.tiers.tier1_hunter`；
5. **微工具路由兼容**：若配置文件包含旧版 `ai.tool_backends`，自动同步为 `ai.tools`；
6. **企业治理配置兼容**：若配置文件包含旧版平铺 `governance` 布尔值，自动映射到 `governance.fingerprint`、`governance.lifecycle` 与 `governance.feedback_memory`。

---

## 五、 高级演进：动态热重载机制 (Dynamic Hot Reloading)

为了解决“调算力、调任务并发、切模型需重启服务”的痛点，系统引入轻量级配置热重载架构：

```mermaid
sequenceDiagram
    autonumber
    actor Admin as 运维人员 / 管理端
    participant Watcher as fsnotify 配置文件监听器
    participant AdminAPI as /api/admin/config/reload API
    participant Loader as LoadConfig() 解析器
    participant Dispatcher as ModelDispatcher 调度器
    participant WorkerPool as 任务工作池 (services.queue)
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
    Loader->>WorkerPool: 动态调整 WorkerPool 容量 (按需扩容/缩容)
    Dispatcher->>Dispatcher: 动态更新 resources 槽位上限与模型映射
    Dispatcher->>Dispatcher: cond.Broadcast() 唤醒等待协程
    Workers-->>Dispatcher: 按最新算力配额继续执行 (零停机/零中断)
```

1. **热加载安全范围**：
   * **允许热重载**：`ai.worker_count`（任务 Worker 扩容）、`ai.resources`（算力节点增删/并发调整）、`ai.throttling`（限流时段与比例）、`ai.debate`（超时与背压门限）、`governance`（规则阈值）。
   * **需重启生效**：`server.port`、`database.*`。
2. **零中断保障**：`ReloadResources` 仅更新槽位定义与 `Limit`，已在执行中的子进程/HTTP 请求保持执行直到完成，新到请求立即使用最新算力池。

---

## 六、 方案收益总结对比

| 评估维度 | 现行配置架构 (`Current`) | 重构后配置架构 (`Proposed`) | 核心收益 |
| :--- | :--- | :--- | :--- |
| **HTTP 基础设施** | 混入了 `worker_count` 与 `max_queue_size` | 纯粹保留端口、数据目录与网络超时 | 职责 100% 单一，成为纯静态冷配置。 |
| **任务与算力并发** | 任务并发在 `server`，模型并发在 `ai`，割裂 | 统一在 `ai` 下建立“仓级 Worker ➔ 辩论阶梯 ➔ 算力节点”三级体系 | 逻辑自洽，支持依据算力总并发自动推导 Worker 并发，支持热重载。 |
| **算力资源管理** | `models` / `native.endpoints` 割裂，概念不清 | 统一收拢为 `ai.resources` 算力池，`native` 作为一等公民内聚 `endpoints` | 结构极简统一，内部原生支持权重分流与 Failover，外部显式引用。 |
| **流水线编排** | `debate` 与 `tiers` 平级，缺少上下文聚合 | 扁平收拢为 `ai.debate` 流水线编排，各阶梯 100% 显式绑定 `resource: "<id>"` | 消除无意义多层嵌套，所见即所得，业务角色与算力清晰解耦。 |
| **微工具扩展** | 写死 3 个孤立场景 | 统一在 `ai.tools` 下指定 `default_resource: "native"` | 统一直连原生集群，具备极速 $<800\text{ms}$ 响应与极简覆盖语法。 |
| **企业治理策略** | 仅 5 个平铺布尔开关 | 分层为 `fingerprint`、`lifecycle`、`feedback_memory` | 增加 `max_rules_injected` 等保护参数，防止 Prompt 爆炸，支持项目级覆盖。 |
| **运维与安全性** | 敏感 Key 易明文泄露，改配置需重启 | 支持 `${ENV}` 占位符 + 动态配置热重载（Hot Reload） | 提高安全性，修改算力、Worker 并发与阈值无需重启服务，扫描任务零中断。 |
