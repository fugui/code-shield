# 02-CodeShield-智能扫描引擎与大模型算力配置架构优化设计

## 📋 方案元数据与导读

*   **文档编号**：`CS-NATIVE-02`
*   **文档类型**：系统配置架构、数据库动态配置中心与算力治理专项设计
*   **当前状态**：`PROPOSED`（架构方案定稿，待代码实施）
*   **涉及模块**：`models/config.go`、`models/models.go`、`config.yaml`、`tasks/*/meta.json`、`handlers/config.go`、`services/dispatcher.go`、`services/queue.go`、`services/native_cli.go`、`frontend/src/pages/ConfigCenter.tsx`
*   **核心目标**：针对 Code-Shield 在配置管理中存在的“`ai:` 模块宽泛臃肿”、“扫描任务并发混在 HTTP Server 中”、“算力供给与扫描引擎耦合”、“改配置需登录服务器修改 YAML 并重启服务打断扫描”、“`tasks` 规则层混杂了底层算力参数 `ai_backend`”、“缺乏多租户配置扩展能力”等痛点，提出：
    1. **顶级领域解耦**：拆分为 `server/database`（静态基础设施）、`llm`（大模型与算力资源池）、`scanner`（扫描引擎与辩论流水线）、`governance`（质量治理与生命周期）四大支柱；
    2. **数据库动态配置中心（Seed-Once & Dynamic DB Config Center）**：`config.yaml` 仅保留启动引导冷配置，`llm`、`scanner`、`governance`、`notification` 全量入库，首次启动从 YAML 导入种子数据（Seed-Once），后续以数据库为唯一真实源（SSOT）；
    3. **业务规则与算力层彻底解耦**：废弃并清理 `tasks/*/meta.json` 及 `TaskType` 中历史遗留的 `ai_backend` 参数，任务规则纯粹化；
    4. **前端可视化配置控制台（Web UI Config Center）**：提供多 Tab 实时可视化配置、端点 Ping 测速、显式阶梯绑定、操作审计与零停机动态热重载（Hot Reloading），为未来多租户独立配置铺平道路。

---

## 一、 背景与现状剖析：为什么需要配置中心化？

### 1.1 现状痛点归因分析

1. **`ai:` 顶层命名过于泛化且严重臃肿（God Section）**：
   * `ai` 词义过于宽泛，导致其内部既包含了“底层算力模型与 GPU 端点”，又包含了“任务调度队列”、“工作时间避峰”、“对抗辩论流水线”和“场景微任务工具”，单章节占全文件 70% 篇幅，层级深且概念混杂。
2. **扫描任务并发混在 HTTP Server 配置中**：
   * `server.worker_count`（扫描任务并发数）与 `server.max_queue_size`（排队上限）放在 `server:` 块中，容易误导为 HTTP 请求处理并发；实际上 `worker_count` 是 **AI 扫描任务工作池（Task Workers）**，其默认值根据大模型算力总并发动态推导 $(sum\_concurrent + 1) / 2$，理应归属于扫描引擎层。
3. **改配置需登机改 YAML 并重启服务（运维成本高、打断扫描）**：
   * 算力扩容、节点上下线、改 API Key、调工作时间限流比例、改辩论超时等均属于高频业务行为。每次调整都需修改服务器上的 `config.yaml` 并重启 `shield-server`，导致正在运行的长耗时分片扫描任务意外中断。
4. **`tasks` 规则层混杂了底层算力参数 `ai_backend`**：
   * 历史单体 CLI 架构在 `tasks/*/meta.json` 中遗留了 `ai_backend` 字段。在现代化三方对抗辩论流水线（Hunter ➔ Challenger ➔ Judge ➔ Synthesis）下，单一扁平字段既无法表达多阶梯分工，又污染了安全规则的纯粹性。
5. **缺乏多租户（Multi-Tenancy）配置扩展能力**：
   * 静态 `config.yaml` 无法支持不同租户/部门拥有专属私有 GPU 算力端点、独立任务队列或差异化 PR 门禁策略。

---

## 二、 核心重构设计：五大顶层领域与“动静冷热分离”模型

系统遵循 **“领域职责单一、显式精准绑定、种子导入+数据库动态主导（Seed-Once & DB SSOT）、规则与算力解耦、多租户就绪”** 的原则，将配置做清晰的动静冷热分离：

```mermaid
graph TD
    subgraph ColdConfig ["1. 基础设施层 (config.yaml - 静态低频冷配置 / 启动引导必需)"]
        Server["server: HTTP 端口 / 网络超时 / 数据目录"]
        DB["database: PostgreSQL 驱动 / 连接池 / 凭据"]
        Auth["auth: JWT 密钥 / SSO OAuth2 端点 / 字段映射"]
    end

    subgraph HotConfig ["2. 动态业务配置中心 (PostgreSQL 数据库持久化 + Web UI 实时修改)"]
        subgraph S_LLM ["大模型算力供给层 (llm)"]
            LLM_Res["llm.resources: Thick Agent 节点 / Native 算力集群 endpoints / 独立并发配额"]
        end
        subgraph S_Scan ["智能扫描引擎与调度层 (scanner)"]
            Scan_Queue["worker_count & max_queue_size: 任务并发与排队"]
            Scan_Throttle["throttling: 工作时间避峰限流"]
            Scan_Debate["debate.tiers: Hunter/Reasoning/Synthesis 阶梯显式绑定"]
            Scan_Tools["tools: RepairJSON / FindingMatch 直连 native"]
        end
        subgraph S_Gov ["质量治理与生命周期层 (governance)"]
            Gov_FP["fingerprint: 缺陷指纹防抖与相似度门限"]
            Gov_LC["lifecycle: 范围守卫 / 自动修复消退 / PR 门禁增量拦截"]
            Gov_FM["feedback_memory: 研发误报负样本记忆注入与容量保护"]
        end
        subgraph S_Notif ["通知服务层 (notification)"]
            Notif_WH["notification.webhook: 通知回调地址"]
        end
    end

    ColdConfig -->|引导服务启动并连接 DB| HotConfig
    S_LLM -->|提供算力槽位| S_Scan
    S_Scan -->|输出原始缺陷| S_Gov
    S_Gov -->|触发事件通知| S_Notif
```

---

## 三、 数据库生命周期设计（Seed-Once 导入与 SSOT 机制）

```mermaid
sequenceDiagram
    autonumber
    participant Server as shield-server 启动
    participant DB as PostgreSQL (system_configs 表)
    participant YAML as config.yaml (本地引导文件)
    participant AdminUI as 前端管理控制台 (ConfigCenter)
    participant Dispatcher as ModelDispatcher 调度器

    Server->>DB: 1. 查询 system_configs 表记录数
    alt 场景 A: 首次部署启动 (DB 记录为空)
        Server->>YAML: 读取 config.yaml 中的 llm, scanner, governance, notification 初始模版
        Server->>DB: 写入 DB 作为初始种子数据 (Seed-Once)
        Server->>Dispatcher: 基于初始种子数据初始化算力池与调度器
    else 场景 B: 日常运行启动 (DB 已有持久化配置)
        Server->>DB: 直接加载 DB 最新配置 (忽略 YAML 中的同名动态配置)
        Server->>Dispatcher: 基于 DB 配置初始化算力池与调度器
    end

    AdminUI->>Server: 2. 管理员在前端修改配置并保存 (PUT /api/admin/config/full)
    Server->>DB: 持久化新配置 (记录 sys_audit_logs 操作审计)
    Server->>Dispatcher: 调用 Dispatcher.ReloadResources() 动态热生效 (零停机)
    Server-->>AdminUI: 返回成功，界面即刻呈现最新状态
```

### 3.1 动静分界清单

| 层次 | 归属位置 | 包含模块与字段 | 存储与生效方式 |
| :--- | :--- | :--- | :--- |
| **静态引导冷配置** | `config.yaml` | `server.port`, `server.data_dir`, `server.read_timeout`<br>`database.*`（主机/端口/账号密码/连接池）<br>`auth.jwt_secret`, `auth.oauth2.*` | 服务启动前必需，仅通过修改文件并重启服务生效。 |
| **动态业务热配置** | **PostgreSQL 数据库**<br>(初次从 YAML 导入) | **`llm`**（算力节点、模型名称、Native Endpoints 集群、并发与权重）<br>**`scanner`**（任务 Worker 数、排队上限、避峰时段、辩论阶梯与超时、微工具路由）<br>**`governance`**（指纹防抖、范围守卫、消退、PR 门禁、负样本规则数）<br>**`notification`**（Webhook 地址） | 首次从 YAML Seed 导入；后续由 Web UI 实时修改、持久化并**零停机动态热生效**。 |

---

## 四、 业务规则与算力层解耦：废弃 tasks 中的 `ai_backend` 参数

### 4.1 废弃动因与冲突剖析
在早期单体 CLI 架构中，系统通过 `tasks/*/meta.json` 及 `TaskType.ai_backend` 指定特定任务使用 `opencode` 或 `claude`。但在现行架构下暴露出三大致命冲突：
1. **破坏动静分离流水线**：Debate 辩论流水线要求 Hunter 必须是具备源码遍历能力的 Thick Agent，而 Challenger/Judge/Synthesis 必须是纯推理的 Thin LLM。单个扁平的 `ai_backend` 无法描述多阶梯分工，强行指定会导致概念混乱；
2. **混淆“业务规则”与“物理算力”**：`tasks` 属于安全审计规则库（定义 Prompt 模板、检测模式与评估标准），不应硬编码底层运行环境是 `opencode` 还是 `agy`；
3. **实际早已名存实亡**：目前系统中所有 `tasks/*/meta.json` 中 `ai_backend` 字段均为空字符串 `""`。

```mermaid
graph LR
    subgraph Old_Way ["过去模式 (规则层硬编码算力 - 冲突混乱)"]
        Task_Old["tasks/meta.json<br/>{ name: 'memory-leak', ai_backend: 'opencode' }"]
        Task_Old -->|硬绑定| CLI["强制拉起单一 CLI 进程"]
    end

    subgraph New_Way ["优化后模式 (规则与算力彻底解耦)"]
        Task_New["tasks/meta.json<br/>纯粹关注 Prompt / 范围 / 判定规则"]
        Config_Center["配置中心 (scanner.debate.tiers)<br/>统一决定 Hunter / Judge 使用哪个 Resource"]
        Task_New --> Config_Center
    end
```

### 4.2 清理与平滑下线措施
1. **清理 `tasks/*/meta.json`**：彻底移除各任务元数据中的 `"ai_backend": ""` 冗余字段；
2. **前端页面解绑**：在「任务类型管理」与「定时策略」页面中移除“AI 后端”下拉选择框；
3. **后端调度归一**：扫描执行时统一由 `scanner.debate.tiers` 决定各阶段算力，数据库 `task_types.ai_backend` 标记为 Deprecated，不再读取。

---

## 五、 完整配置规范模版 (YAML Specification)

`config.yaml` 文件完整结构如下（其中动态部分作为系统初次安装部署时的初始默认值）：

```yaml
# ==============================================================================
# Code-Shield 🛡️ 服务端配置规范 (最终定稿标准)
# 注：llm、scanner、governance、notification 仅在系统首次启动时作为种子数据导入 DB，
# 后续以数据库和 Web UI 配置中心修改为准。
# ==============================================================================

# ── 1. 基础设施层 (纯粹的静态低频冷配置) ──
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

# ==============================================================================
# 2. 大模型与算力供给池 (LLM Compute Resources & Endpoints - 初次导入模版)
# 职责：专注解决“模型是什么、API在何处、物理并发多大、如何负载分流”
# ==============================================================================
llm:
  default_resource: "native"           # 全局兜底 Resource ID
  debug_logs: false                    # 是否输出大模型底层交互调试日志

  # 算力节点资源池
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

# ==============================================================================
# 3. 智能扫描引擎与任务调度 (Scanner & Debate Pipeline Engine - 初次导入模版)
# 职责：专注解决“任务如何排队、如何避峰、Hunter/Judge 阶梯流水线如何执行”
# ==============================================================================
scanner:
  # 任务并发与排队队列
  worker_count: 5                      # 全局扫描任务并发 Worker 数 (缺省自动按算力池总并发折中推导)
  max_queue_size: 2000                 # 待处理扫描任务排队最大上限 (超限返回 HTTP 429，-1 为无上限)
  mock_on_missing_cli: false           # CLI 未就绪时是否阻断或模拟 (生产建议 false)

  # 全局流控与避峰策略 (Throttling & Work Hours)
  throttling:
    work_hours:
      enabled: true                    # 开启工作时间自动限流
      workdays: [1, 2, 3, 4, 5]        # 生效星期 (周一至周五)
      start_time: "09:00"              # 限流开始时刻
      end_time: "22:00"                # 限流结束时刻
      scale: 0.10                      # 工作时间算力比例 (0.10 代表 10%，0.0 代表暂停)

  # 多智能体对抗辩论流水线 (Debate Pipeline)
  debate:
    enabled: true                      # 是否启用多智能体对抗辩论流水线
    fast_pass_enabled: true            # 0 候选快速放行 (节省 80%+ 辩论算力)
    max_candidates_per_chunk: 20       # 单分片最大仲裁候选数
    stage_timeout_seconds: 1800        # 单阶段全局硬超时兜底 (秒)
    backpressure_threshold: 30         # 跨 Tier 背压触发积压阈值
    backpressure_timeout_seconds: 120  # 背压超时兜底 (秒)
    log_retention_days: 30             # 辩论轨迹日志保留天数

    # 各阶段阶梯显式绑定 llm.resources 节点
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

  # 内置场景微任务路由 (Utility Tools Routing)
  tools:
    default_resource: "native"         # 默认统一走 native
    overrides:                         # 特殊微任务可按需覆写
      repair_json: "native"
      finding_match: "native"
      feedback_extraction: "native"

# ==============================================================================
# 4. 企业多任务治理与历史记忆闭环 (Enterprise Governance & SSOT Memory - 初次导入模版)
# 职责：专注解决“质量红线、缺陷指纹防抖、范围守卫、PR门禁与知识沉淀”
# ==============================================================================
governance:
  # 跨扫描缺陷抗抖动指纹
  fingerprint:
    enabled: true                      # 启用抗行号抖动的语义缺陷指纹
    similarity_threshold: 0.85         # 跨版本模糊匹配相似度门限

  # 缺陷生命周期与门禁状态
  lifecycle:
    scope_guard_enabled: true          # 扫描范围守卫 (杜绝局部扫描产生的假修复)
    auto_resolve_missing: true         # 守卫范围内消失的缺陷自动标记为 RESOLVED
    diff_gate_strict: true             # PR/MR 门禁流水线仅阻断 [NEW] 增量缺陷

  # 研发误报记忆与反馈注入
  feedback_memory:
    injection_enabled: true            # 自动将研发标记的误报规则注入 Prompt
    max_rules_injected: 10             # 单次扫描最大注入负样本规则数 (防 Prompt 溢出)

# ==============================================================================
# 5. 事件通知服务 (Notification - 初次导入模版)
# ==============================================================================
notification:
  webhook: "http://127.0.0.1:8081/api/notify/email"
```

---

## 六、 前端配置中心控制台设计 (Web UI Specification)

### 6.1 页面入口与布局架构
页面路由：`/admin/config-center`（系统管理 ➔ 配置中心），采用卡片化、多 Tab 分组与抽屉交互：

```text
┌──────────────────────────────────────────────────────────────────────────────────────────────────┐
│  ⚙️ 系统配置中心 (System Config Center)                               [重置为初始模版]  [保存生效] │
├──────────────────────────────────────────────────────────────────────────────────────────────────┤
│  [ 🤖 大模型与算力池 ]   [ ⚙️ 扫描引擎与流水线 ]   [ 🛡️ 质量治理与门禁 ]   [ 🔔 通知服务 ]               │
└──────────────────────────────────────────────────────────────────────────────────────────────────┘
```

### 6.2 各 Tab 详细设计规范

#### Tab 1: 🤖 大模型与算力池 (`llm`)
* **算力节点列表（Resource Cards）**：
  * 卡片直观呈现每个算力节点：节点 ID、驱动徽章（`agy` / `opencode` / `native`）、绑定模型名、物理并发槽位数字输入框、当前活跃负载指示条（如 `Active: 2 / Limit: 10`）、开关切换（Switch）。
  * 具备 **「➕ 新增算力节点」** 抽屉。
* **Native REST 算力集群管理（Endpoints Manager Panel）**：
  * 点击 `native` 节点展开集群抽屉，可视化管理多个物理端点（`local-vllm-primary`、`cloud-deepseek-backup`）。
  * 字段项：端点名称、BaseURL、API Key（密文掩码显示，支持点击眼睛查看与修改）、模型名、并发数、权重滑块（`0% ~ 100%`）。
  * **「🔌 测试连接 (Ping API)」按钮**：点击向对应端点发送一次探测请求，展示延迟（如 `🟢 218ms HTTP 200 OK`）或错误提示。

#### Tab 2: ⚙️ 扫描引擎与流水线 (`scanner`)
* **仓级任务吞吐控制**：
  * 全局任务并发 Worker 数（数字输入框，附带提示与一键填充按钮：*“💡 根据当前算力总并发推荐: 12”*）。
  * 待扫描任务排队上限（超限返回 429 保护系统）。
* **工作时间自动避峰（Work Hours Throttling）**：
  * 开关、生效星期多选（周一至周五）、时段范围选择（`09:00 ~ 22:00`）、限流比例滑块（`10% ~ 100%`）。
* **多智能体对抗辩论流水线（Debate Pipeline）**：
  * 0 候选快速放行开关（Switch，附带效益说明：*“初筛无缺陷时跳过后续辩论，节省 80%+ 算力”*）。
  * 单分片最大仲裁候选数、全局阶段硬超时（秒）、跨 Tier 背压触发阈值。
* **阶梯角色显式绑定表（Debate Tiers Binding）**：
  * | 阶段角色 | 职责定位 | 绑定算力节点 (下拉选择 `llm.resources`) | 阶段单片超时 (秒) |
    | :--- | :--- | :--- | :--- |
    | **Tier 1 Hunter** | 缺陷初筛 (Thick Agent 自主探索) | `[ agy-gemini ▼ ]` | `1200` |
    | **Tier 2 Reasoning** | 辩护与终审 (Thin LLM 深度推理) | `[ native ▼ ]` | `600` |
    | **Tier 3 Synthesis** | 态势汇总 (Thin LLM 报告排版) | `[ native ▼ ]` | `300` |
* **微任务工具直连（Tools Routing）**：
  * 默认节点选择下拉框（默认 `native`）。

#### Tab 3: 🛡️ 质量治理与生命周期 (`governance`)
* **跨周期缺陷指纹防抖**：
  * 指纹计算开关；
  * 语义相似度门限滑块（`0.50 ~ 1.00`，标明推荐值 `0.85`）。
* **生命周期守卫与 PR 门禁**：
  * 扫描范围守卫开关（Scope Guard，防止局部假修复）；
  * 存量消失缺陷自动标记修复（Auto Resolve Missing 开关）；
  * PR/MR 门禁增量拦截模式（Diff Gate Strict 开关，仅阻断 `NEW` 增量缺陷）。
* **研发误报记忆与反馈**：
  * 负样本规则自动注入 Prompt 开关；
  * 单次最大注入规则数（数字输入框，默认 `10`，防 Prompt 溢出）。

#### Tab 4: 🔔 通知服务 (`notification`)
* Webhook 回调地址输入框、测试发送通知按钮。

---

## 七、 数据模型与 REST API 契约

### 7.1 GORM 数据模型扩展 (`models/models.go`)

```go
package models

import (
	"time"
	"gorm.io/datatypes"
)

// SystemDynamicConfig 数据库持久化动态配置表 (ID=1 为全局系统基线，预留 TenantID 支持多租户)
type SystemDynamicConfig struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	TenantID  uint           `gorm:"default:0;index" json:"tenant_id"` // 0=全局系统配置，>0=指定租户专属配置
	Category  string         `gorm:"size:50;not null;index" json:"category"` // "llm", "scanner", "governance", "notification"
	Data      datatypes.JSON `gorm:"type:jsonb;not null" json:"data"`        // 对应的结构化 JSON 数据
	Version   int            `gorm:"default:1" json:"version"`               // 乐观锁版本号
	UpdatedBy string         `gorm:"size:100" json:"updated_by"`             // 最后修改人
	UpdatedAt time.Time      `json:"updated_at"`
}
```

### 7.2 管理端 REST API 契约

| 方法 | API 路径 | 鉴权要求 | 描述 |
| :--- | :--- | :--- | :--- |
| `GET` | `/api/admin/config/full` | Admin | 获取当前生效的全量动态配置（聚合 DB 中的 `llm`、`scanner`、`governance`、`notification`） |
| `PUT` | `/api/admin/config/full` | Admin | 保存全量配置，持久化到 DB 并即时触发 `Dispatcher.ReloadResources` 动态热重载 |
| `POST`| `/api/admin/config/ping-endpoint` | Admin | 测试指定 Native 算力端点连通性与模型响应延迟 |
| `POST`| `/api/admin/config/reset-to-seed` | Admin | 将配置重置为 `config.yaml` 初始模版（带审计与二次确认） |

---

## 八、 方案收益总结对比

| 评估维度 | 现行配置架构 (`Current`) | 优化后配置中心架构 (`Proposed`) | 核心收益 |
| :--- | :--- | :--- | :--- |
| **配置存储载体** | 纯静态本地 `config.yaml` | **冷配置留在 YAML + 热配置纳入 DB 动态中心** | 免登机修改、免重启服务，界面实时修改生效。 |
| **规则与算力解耦** | `tasks/*/meta.json` 硬编码 `ai_backend` | **彻底废弃任务级 `ai_backend`** | 安全规则与算力环境彻底解耦，Debate 阶梯语义统一。 |
| **多租户演进能力** | 静态单文件，无法扩展租户隔离 | **DB 表挂载 TenantID 隔离** | 天然支持多租户独立算力池、独立队列与差异化质量门禁。 |
| **顶级领域解耦** | `ai` 包含一切，庞大臃肿且词义模糊 | 拆分为 **`llm`（算力）+ `scanner`（引擎）+ `governance`（治理）** | 职责 100% 单一，概念精准自解释，篇幅均衡对称。 |
| **HTTP 基础设施** | 混入了 `worker_count` 与 `max_queue_size` | 纯粹保留端口、数据目录与网络超时 | 职责彻底纯粹，成为纯静态低频冷配置。 |
| **算力资源管理** | `models` / `native.endpoints` 割裂 | 统一收拢为 `llm.resources` 算力池，`native` 作为一等公民内聚 `endpoints` | 结构极简统一，内部原生支持权重分流与 Failover，外部显式引用。 |
| **辩论流水线编排** | `debate` 与 `tiers` 平级，缺少上下文聚合 | 扁平收拢为 `scanner.debate` 流水线编排，各阶梯 100% 显式绑定 `resource: "<id>"` | 消除无意义多层嵌套，所见即所得，业务角色与算力清晰解耦。 |
| **微工具扩展** | 写死 3 个孤立场景 | 统一在 `scanner.tools` 下指定 `default_resource: "native"` | 统一直连原生集群，具备极速 $<800\text{ms}$ 响应与极简覆盖语法。 |
| **企业治理策略** | 仅 5 个平铺布尔开关 | 分层为 `fingerprint`、`lifecycle`、`feedback_memory` | 增加 `max_rules_injected` 等保护参数，防止 Prompt 爆炸，支持项目级覆盖。 |
| **运维与安全性** | 敏感 Key 易明文泄露，改配置需重启 | 支持 `${ENV}` 占位符 + 数据库持久化 + 动态热重载 + 操作审计 Diff | 提高安全性，修改算力、Worker 并发与阈值无需重启服务，扫描任务零中断。 |
