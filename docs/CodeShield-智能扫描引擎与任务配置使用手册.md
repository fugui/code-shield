# Code-Shield 智能扫描引擎与任务配置使用手册 🛡️

欢迎使用 **Code-Shield 智能代码安全与质量分析平台**。本手册旨在帮助研发人员、安全专家与系统管理员深入理解系统的核心能力，掌握任务类型（Task Type）的高级配置技巧，以及在不同业务场景下如何合理选择执行引擎、分片参数与服务端底层并发配置。

---

## 目录
- [一、 核心架构与核心理念](#一-核心架构与核心理念)
- [二、 五大执行模式详解 (Engine Modes)](#二-五大执行模式详解-engine-modes)
  - [2.1 模式选型决策矩阵](#21-模式选型决策矩阵)
  - [2.2 模式详细说明](#22-模式详细说明)
- [三、 任务类型与引擎配置指南 (Task Types & Engine Config)](#三-任务类型与引擎配置指南-task-types--engine-config)
  - [3.1 引擎配置参数速查表](#31-引擎配置参数速查表)
  - [3.2 受控标准分类白名单 (SSOT Categories)](#32-受控标准分类白名单-ssot-categories)
  - [3.3 典型场景 JSON 配置模版](#33-典型场景-json-配置模版)
- [四、 服务端核心架构配置指南 (config.yaml 与配置中心)](#四-服务端核心架构配置指南-configyaml-与配置中心)
  - [4.1 核心理念：动静分层与数据库单一真值源 (Database SSOT)](#41-核心理念动静分层与数据库单一真值源-database-ssot)
  - [4.2 基础设施层 (纯静态冷配置 - `server`, `storage`, `database`, `auth`)](#42-基础设施层-纯静态冷配置---server-storage-database-auth)
  - [4.3 算力供给层：大模型与算力资源池 (`llm`)](#43-算力供给层大模型与算力资源池-llm)
  - [4.4 动静分离算力选型指南 (Thick Agent vs. Thin LLM)](#44-动静分离算力选型指南-thick-agent-vs-thin-llm)
  - [4.5 业务需求层：智能扫描引擎与辩论流水线 (`scanner`)](#45-业务需求层智能扫描引擎与辩论流水线-scanner)
  - [4.6 各阶段算力阶梯多资源池化与调度算法 (`scanner.debate.tiers`)](#46-各阶段算力阶梯多资源池化与调度算法-scannerdebatetiers)
  - [4.7 场景级内嵌微任务专有路由 (`scanner.tools`)](#47-场景级内嵌微任务专有路由-scannertools)
  - [4.8 企业多任务治理与历史记忆闭环 (`governance`)](#48-企业多任务治理与历史记忆闭环-governance)
  - [4.9 事件通知服务 (`notification`)](#49-事件通知服务-notification)
- [五、 系统诊断与应急运维自愈 (/admin/debug)](#五-系统诊断与应急运维自愈-admindebug)
  - [5.1 算力槽位实时负载透视](#51-算力槽位实时负载透视)
  - [5.2 活跃槽位一键校准自愈 (Reset Active Slots)](#52-活跃槽位一键校准自愈-reset-active-slots)
- [六、 缺陷生命周期与人机反馈记忆闭环](#六-缺陷生命周期与人机反馈记忆闭环)
  - [6.1 增量状态徽标 (Diff Status)](#61-增量状态徽标-diff-status)
  - [6.2 智能体三方对抗事实链查看](#62-智能体三方对抗事实链查看)
  - [6.3 研发人员一键标记反馈与规则沉淀](#63-研发人员一键标记反馈与规则沉淀)
- [七、 常见问题与排查指引 (FAQ)](#七-常见问题与排查指引-faq)

---

## 一、 核心架构与核心理念

Code-Shield 摒弃了传统 SAST 规则匹配的高误报，也规避了简单 Prompt 调用在大仓场景下的“上下文丢失”、“幻觉”与“算力浪费”，构建了六大核心技术支柱：

```mermaid
flowchart LR
    A["代码仓 (Git/Workdir)"] --> B["1. 语义感知分片器\n(同名头文件投影 + 宏注入 + Git Diff)"]
    B --> C["2. 动静分离混合执行引擎\n(Thick Agent 探索 + Thin LLM 推理)"]
    C --> D["3. 阶梯多智能体对抗辩论\n(Hunter ➜ Challenger ➜ Judge ➜ Synthesis)"]
    D --> E["4. 物理行锚点与确定性校准\n(SourceEnricher + CWE 决策树)"]
    E --> F["5. 受控标准分类白名单\n(Task SSOT Categories 强约束)"]
    F --> G["6. 通用缺陷指纹与历史记忆\n(抗抖动指纹 + 范围守卫 + 规则沉淀)"]
    G --> H["结构化高质感报告与治理看板"]
```

1. **语义感知分片 (Semantic Chunking)**：
   自动将 `src/` 实现文件与其依赖的 `include/` 同名头文件配对归组，并自动提取公共头文件结构体声明摘要（Header Outline）与全局构建宏（`#define`），随分片一同送给大模型，彻底解决跨文件上下文缺失问题。
2. **动静分离混合执行引擎 (Hybrid Execution Engine)**：
   - **Thick Agent（重型/探索型，如 `agy` / `opencode` / `claude`）**：专注于 Hunter 阶段，在代码仓中自主递归穿透与按需读取磁盘源码文件；
   - **Thin LLM（轻量/推理型，`NativeInvoker`）**：基于 HTTP REST 长连接直接调用 LLM API，专用于 Challenger 抗辩、Judge 终审、Tier 3 报告汇总、JSON 语法修复及指纹匹配，彻底消除本地 CLI 子进程开销与 SQLite 状态锁冲突。
3. **阶梯多智能体三方对抗辩论 (Agentic Debate Pipeline)**：
   - **Hunter (初筛猎手 - Tier 1)**：高召回快速发掘可疑漏洞与攻击假设，0 候选分片秒级快速放行（Fast-Pass）；
   - **Challenger (对抗辩护人 - Tier 2)**：从断言、条件宏、语言规范四维发起严厉反向质询；
   - **Judge (终审法官 - Tier 2)**：对照源码做出最终判词，判定 `CONFIRMED`、`REJECTED` 或 `CONDITIONAL`，彻底剔除误报；
   - **Synthesis (态势汇总 - Tier 3)**：长上下文综合总结与全仓治理态势归纳。
4. **物理行锚点与确定性严重度校准 (SourceEnricher & Deterministic Calibrator)**：
   - 通过 `SourceEnricher` 引擎直接定位物理源码文件，校准起始行/结束行范围、提取规范化物理代码 Token 与函数作用域签名，根治行号偏移问题；
   - 基于 CWE 决策树、内存越界/UAF 特征与外部宏隔离判定，纠正大模型的自由裁量与定级倒挂。
5. **受控标准分类白名单 (SSOT Categories)**：
   各任务类型在 `meta.json` 中配置严谨的标准分类白名单，并在 Prompt 中强制约束。后端在入库前经过清洗与强校验，杜绝模型自由捏造分类，保证看板与报表统计维度的高度一致。
6. **通用缺陷指纹与历史记忆闭环 (SSOT Fingerprint & Memory)**：
   基于公式 $\text{SHA256}(\text{RepoID} + \text{TaskTypeID} + \text{Path} + \text{Scope} + \text{PhysicalToken})$ 计算抗行号抖动的唯一指纹，实现跨扫描的增量生命周期追踪（`NEW` / `EXISTED` / `RESOLVED` / `REOPENED`），并支持研发人员一键沉淀负样本规则。

---

## 二、 五大执行模式详解 (Engine Modes)

系统支持 5 种执行引擎模式，可在任务类型配置的 **「执行模式」** 下拉框中随时切换。

### 2.1 模式选型决策矩阵

| 执行模式 | 内部标识 | 核心流程 | 推荐适用任务类型 | 精度/召回/速度评估 |
| :--- | :--- | :--- | :--- | :--- |
| **全量多智能体辩论** | `debate_full` | 语义分片 ➜ Hunter 初筛 ➜ 0 候选秒级放行 / 候选切拆分批 ➜ Challenger 抗辩 ➜ Judge 终审 ➜ 物理锚点校准 ➜ 增量打标 | Coredump 风险、内存泄漏、深度代码检视、近期变更检视 | ⭐⭐⭐⭐⭐ 极高精度<br>⭐⭐⭐⭐⭐ 极低误报<br>中等耗时 |
| **选择性智能体辩论** | `debate_selective` | 语义分片 ➜ 规则/关键字初筛 ➜ 仅对疑点触发对抗辩论 ➜ 物理锚点校准 ➜ 增量打标 | cJSON 泄漏、无序集合导出、单测质量审计 | ⭐⭐⭐⭐⭐ 高精度<br>⭐⭐⭐⭐ 较低成本<br>较快耗时 |
| **语义分片快扫** | `chunked_fast` | 语义分片 ➜ 规则初筛 ➜ 确定性规则校准 ➜ 增量打标 | 浮点数直接比较、裸线程创建、单测有效性评估 | ⭐⭐⭐⭐ 高召回<br>⭐⭐⭐⭐⭐ 极低 Token 消耗<br>极速并发 |
| **经典分片引擎** | `chunked` | 目录结构拆分 ➜ 并发大模型分析 ➜ 规则校准 ➜ 增量打标 ➜ 报告综合 | 通用代码安全扫描、大仓分层分析 | ⭐⭐⭐⭐ 良好稳定<br>⭐⭐⭐ 传统模式 |
| **单次全仓引擎** | `single` | 单次整仓提问 ➜ 规则校准 ➜ 增量打标 ➜ 报告综合 | 小型脚本工具仓、Demo 工程、轻量初筛 | ⭐⭐⭐ 单次直出<br>适用于 ≤ 20 个小文件 |

---

### 2.2 模式详细说明

#### 1. 🤖 全量对抗辩论模式 (`debate_full`)
- **工作机制**：每一个代码分片均由 Tier 1 初筛模型（Thick Agent）快速发掘候选，零可疑分片（0 Candidates）秒级快速放行（Fast-Pass）；一旦发掘可疑漏洞，自动按每批 5 个候选切拆（Batching），调取 Tier 2 强推理模型启动 Challenger 抗辩与 Judge 终审仲裁，最后由 Tier 3 汇总全仓态势。
- **最佳场景**：
  - `coredump_risk`（进程崩溃隐患）：需要推演复杂空指针、野指针与栈越界；
  - `memory_leak`（内存泄漏）：需要追踪跨函数、跨分支的申请与释放；
  - `change_review`（近期变更检视）：对代码改动质量做严格守门。

#### 2. ⚖️ 选择性辩论模式 (`debate_selective`)
- **工作机制**：对于特定 API 库（如 cJSON、STL 容器）先基于内容关键字极速过滤，仅当命中了可疑使用模式时，才向辩论法庭提交仲裁案卷。
- **最佳场景**：
  - `cjson_scan`（cJSON_Delete 缺失/双重释放）；
  - `unordered_collection`（签名或导出场景下无序 map 的确定性排查）。

#### 3. ⚡ 语义分片快扫模式 (`chunked_fast`)
- **工作机制**：跳过重量级的大模型多轮反向辩论，利用语义分片器注入的宏与头文件上下文，配合后置确定性严重度决策树快速判定。
- **最佳场景**：
  - `float_comparison`（浮点数直接 `==` 或 `!=` 比较）；
  - `thread_create`（显式创建 `std::thread` / `pthread_create` 治理）；
  - `ut_effectiveness`（单元测试断言有效性与空测试）。

---

## 三、 任务类型与引擎配置指南 (Task Types & Engine Config)

在 **「任务类型」➜「编辑」** 中的 **「引擎配置 (JSON)」** 文本框中，可通过 JSON 格式定制化微调扫描行为。

### 3.1 引擎配置参数速查表

| 配置参数 (JSON Key) | 类型 | 默认值 | 作用说明 |
| :--- | :---: | :---: | :--- |
| **`since_days`** | `int` | `0` | **增量变更提取**：仅检视最近 N 天内有 Git 提交的文件（如 `7`）。设为 `0` 表示全仓扫描。若无提交则触发 Precondition 跳过扫描。 |
| **`diff_base`** | `string` | `""` | **分支/基准差异比对**：仅检视与基准分支或 Commit 发生差异的文件（例如 `"origin/main"`）。 |
| **`max_files`** | `int` | `20` | 单个语义分片包含的最大文件数。大仓建议设为 `15~30`。 |
| **`depth`** | `int` | `1` | 目录归组深度。`1` 代表按顶层一级目录分片，`2` 代表按二级子目录分片。 |
| **`concurrency`** | `int` | `6` | 该任务分析时分片并发的最大协程数。 |
| **`file_extensions`** | `array` | `[]` | 任务级扩展名白名单（如 `[".c", ".cpp", ".h"]`），为空时匹配全部常见源码。 |
| **`content_keywords`** | `array` | `[]` | 内容关键字预过滤器，仅当文件包含其中关键字时才进行分析（如 `["cJSON_"]`）。 |
| **`exclude_paths`** | `array` | `[]` | 忽略路径列表（如 `["thirdparts", "vendor", "test/mock"]`）。 |

---

### 3.2 受控标准分类白名单 (SSOT Categories)

为了杜绝大模型随机捏造自由分类导致报表混乱，各任务类型支持配置标准受控分类（`categories`）：
- **配置方式**：在任务类型元定义（`meta.json`）中声明受控分类数组；
- **Prompt 强制约束**：系统自动将标准分类列表注入大模型 Prompt，要求模型严格对照分类进行归因；
- **后端强校验清洗**：模型返回的类别若不在白名单中，后端将在入库前自动匹配最接近的规范类别或打标为兜底分类，确保统计分析指标的绝对权威。

---

### 3.3 典型场景 JSON 配置模版

#### 场景一：近期代码检视 (最近 7 天变更精准检视)
适用于周度代码评审或定期质量守门，自动识别 7 天内的变动文件并智能绑定同名头文件：
```json
{
  "since_days": 7,
  "max_files": 15,
  "depth": 1,
  "exclude_paths": [
    "thirdparts",
    "build",
    "third_party"
  ]
}
```

#### 场景二：C/C++ 核心代码库深度攻关 (Coredump / 内存泄漏)
限制在 C/C++ 源文件，按二级目录精细化分片，自动排除第三方依赖：
```json
{
  "file_extensions": [
    ".c",
    ".cpp",
    ".cc",
    ".cxx",
    ".h",
    ".hpp"
  ],
  "depth": 2,
  "max_files": 20,
  "concurrency": 8,
  "exclude_paths": [
    "thirdparts",
    "vendor",
    "docs"
  ]
}
```

#### 场景三：特定关键字专项排查 (如 cJSON 内存泄漏)
利用 `content_keywords` 毫秒级跳过无关源码文件：
```json
{
  "content_keywords": [
    "cJSON_",
    "cJSON"
  ],
  "file_extensions": [
    ".c",
    ".cpp",
    ".h"
  ],
  "max_files": 30,
  "concurrency": 6,
  "exclude_paths": [
    "thirdparts"
  ]
}
```

---

## 四、 服务端核心架构配置指南 (config.yaml 与配置中心)

### 4.1 核心理念：动静分层与数据库单一真值源 (Database SSOT)

Code-Shield 采用严格的**动静分层治理体系 (CS-NATIVE-02 架构规范)**：

```mermaid
flowchart TD
    subgraph ColdConfig ["1. 基础设施冷配置 (config.yaml 静态加载)"]
        server["server: 端口、超时、外链"]
        storage["storage: 根目录"]
        database["database: PostgreSQL 连接池"]
        auth["auth: JWT 密钥、OAuth2 SSO"]
    end

    subgraph HotConfig ["2. 动态业务中心 (Database SSOT 数据库唯一真值源)"]
        direction TB
        seed["首次初始化: config.yaml 导入初始种子数据"] --> DB[(PostgreSQL 数据库)]
        DB --> Web["前端可视化配置中心 (/admin/config)"]
        Web -->|实时调整保存| DB
        DB -->|热重载 (HotReload)| Dispatcher["ModelDispatcher 内存调度器"]
        Reset["重置预设种子 (POST /reset-to-seed)"] -.->|覆盖重置| DB
    end
```

> [!IMPORTANT]
> - **基础设施层**（`server`、`storage`、`database`、`auth`）：属于纯静态冷配置，修改后必须重启服务生效。
> - **动态业务中心层**（`llm`、`scanner`、`governance`、`notification`）：`config.yaml` 中的内容仅在系统首次安装部署（数据库为空）时作为初始种子数据导入；系统初始化后，**以数据库为唯一真实源（DB SSOT）**，超级管理员可在 Web 前端「配置中心 (`/admin/config`)」实时热重载更新，**无需重启服务**。

---

### 4.2 基础设施层 (纯静态冷配置 - `server`, `storage`, `database`, `auth`)

在 `config.yaml` 中配置底层系统运行环境：

```yaml
server:
  port: ":8080"                         # HTTP 服务监听端口
  external_url: "http://192.168.56.18:8080" # 外部访问基准 URL (用于邮件通知与报告外链跳转)
  read_timeout: 120s                    # HTTP 请求读取超时
  write_timeout: 120s                   # HTTP 响应写入超时
  idle_timeout: 180s                    # HTTP Keep-Alive 空闲保持超时

storage:
  root: "."                             # 数据根目录，下设 codes/ (缓存) 与 reports/ (报告)

database:
  driver: "postgres"                    # 数据库驱动，固定为 postgres
  host: "127.0.0.1"                     # PostgreSQL 主机地址
  port: 5432                            # PostgreSQL 端口
  user: "postgres"                      # 数据库用户名
  password: "CodeShield618!"            # 数据库密码
  dbname: "code_shield"                 # 数据库名称
  sslmode: "disable"                    # SSL 模式
  timezone: "Asia/Shanghai"             # 会话时区
  max_open_conns: 50                    # 连接池最大打开连接数
  max_idle_conns: 10                    # 连接池最大空闲连接数

auth:
  jwt_secret: "YOUR_JWT_SECRET_KEY"     # JWT 签名密钥 (生产环境请务必修改)
  password_login_enabled: true          # 是否启用账号密码登录
  oauth2:
    enabled: true                       # 是否启用 OAuth2 / OIDC 单点登录
    client_id: "code-shield"
    client_secret: ""
    auth_url: "https://sso.yourcompany.com/realms/main/protocol/openid-connect/auth"
    token_url: "https://sso.yourcompany.com/realms/main/protocol/openid-connect/token"
    userinfo_url: "https://sso.yourcompany.com/realms/main/protocol/openid-connect/userinfo"
    admin_list: ["super_admin", "admin@company.com"] # SSO 自动提权为超级管理员的白名单
```

---

### 4.3 算力供给层：大模型与算力资源池 (`llm`)

**定位**：**物理算力供给池**，解决“模型是什么、驱动类型为何、API 在何处、物理并发多少”的问题。

```yaml
llm:
  default_resource: "native"            # 全局兜底 Resource ID
  debug_logs: false                     # 是否输出大模型底层交互日志 (.debug.log)

  # 算力节点资源池定义
  resources:
    # 算力节点 1: Antigravity CLI 探索型 Thick Agent (初筛与复杂代码探索)
    - id: "agy"
      driver: "agy"                     # 驱动类型: agy / opencode / claude / codex / native
      model: "gemini-3.7-flash"
      concurrent: 5                     # 该节点最大物理并发槽位

    # 算力节点 2: OpenCode 算力节点 (备选 Thick Agent)
    - id: "opencode"
      driver: "opencode"
      model: "models/glm5.1"
      concurrent: 5

    # 算力节点 3: 原生 Thin LLM 执行引擎 (统一 ID 为 native，内含高并发集群 endpoints)
    - id: "native"
      driver: "native"
      model: "deepseek-v4-flash-vision-exp"
      concurrent: 10
      base_url: "https://api.deepseek.com/v1/chat/completions"
      api_key: "sk-internal-token"
      response_format_json: false
      max_retries: 3                    # 网络抖动/429/5xx 时的最大重试次数
      retry_backoff_ms: 500             # 指数退避基准时长 (毫秒)

      # ── 多端点算力集群 (支持加权负载打散与自动故障转移 Failover) ──
      endpoints:
        - name: "deepseek-primary"
          base_url: "https://api.deepseek.com/v1/chat/completions"
          api_key: "sk-internal-token-primary"
          model: "deepseek-v4-flash-vision-exp"
          concurrent: 10
          weight: 80                    # 相对权重比例
        - name: "local-vllm-backup"
          base_url: "http://192.168.56.18:8000/v1/chat/completions"
          api_key: "sk-internal-token-backup"
          model: "glm-4-flash"
          concurrent: 5
          weight: 20
```

---

### 4.4 动静分离算力选型指南 (Thick Agent vs. Thin LLM)

系统严格践行**动静分离**的算力选型规范：

| 维度 | Thick Agent (重型/探索型) | Thin LLM (`native` 原生轻量推理型) |
| :--- | :--- | :--- |
| **代表驱动** | `agy`, `opencode`, `claude`, `codex` | `native` (兼容 OpenAI / vLLM / LiteLLM / DeepSeek) |
| **底层实现** | 本地 `os/exec` 创建子进程，需加载 CLI 运行时 | 基于 HTTP/2 REST 长连接直接与模型服务端交互 |
| **磁盘访问能力** | **具备本地文件系统完整权限**，能自主遍历代码、穿透调用链 | **无磁盘访问权限**，仅接收内存中已经打包内联的代码片段 |
| **启动耗时** | 2s ~ 8s (沙箱加载、本地状态初始化) | **< 10ms** (零进程开销，毫秒级快速启动) |
| **并发与稳定性** | 受限于单机 CPU/内存与 CLI 锁竞争，并发容量中等 (3~10) | **极高并发吞吐** (10~100+)，自带端点权重轮询与故障转移 |
| **流水线选型准则** | **Tier 1 (Hunter 猎手初筛) 必须使用**<br>*(初筛阶段仅传入文件清单，模型需自主读盘遍历上下文)* | **Tier 2 (辩论仲裁) 与 Tier 3 (汇总) 强烈推荐**<br>*(案卷代码已全量内联，无需读盘，追求极速推理与超大吞吐)* |

---

### 4.5 业务需求层：智能扫描引擎与辩论流水线 (`scanner`)

```yaml
scanner:
  # 任务并发与排队队列
  worker_count: 5                      # 全局扫描任务并发 Worker 数
  max_queue_size: 2000                 # 待处理扫描任务排队最大上限 (超限返回 HTTP 429，-1 为无上限)
  mock_on_missing_cli: false           # CLI 未就绪时是否阻断或模拟 (生产环境必须为 false)

  # 全局流控与避峰策略 (Throttling & Work Hours)
  throttling:
    work_hours:
      enabled: false                   # 是否开启工作时间自动限流
      workdays: [1, 2, 3, 4, 5]        # 生效星期 (周一至周五)
      start_time: "09:00"              # 限流开始时刻 (HH:MM)
      end_time: "22:00"                # 限流结束时刻 (HH:MM)
      scale: 0.10                      # 工作时间并发比例 (0.10 代表压低至 10%，0.0 代表白天完全暂停)

  # 多智能体对抗辩论流水线 (Debate Pipeline)
  debate:
    enabled: true                      # 是否启用多智能体对抗辩论流水线
    fast_pass_enabled: true            # 0 候选快速放行 (无疑点分片秒级放行，节省 80%+ 辩论算力)
    max_candidates_per_chunk: 100      # 单分片最大仲裁候选数上限 (防异常 Prompt 爆炸)
    stage_timeout_seconds: 1800        # 单阶段全局硬超时兜底 (秒，默认 30 分钟)
    backpressure_threshold: 30         # 跨 Tier 背压触发积压阈值
    backpressure_timeout_seconds: 120  # 背压超时兜底 (秒)
    log_retention_days: 30             # 辩论轨迹日志保留天数
```

#### 辩论流水线关键性能保障：
1. **0 候选秒级快速放行 (Fast-Pass)**：Hunter 初筛如未发现任何可疑漏洞，分片立即标记为合格，直接跳过 Challenger 与 Judge，节约海量深度推理算力。
2. **批次切拆 (Batching)**：若初筛发现多项候选，引擎自动以每批 5 个候选切拆为多个子批次，独立进行辩驳与仲裁，消除单次 Prompt 超过 128K/200K 上限的风险，且极大缩短单次调用延迟。
3. **超长代码软截断**：超过安全界限的巨型函数代码段自动做语义化软截断，保护推理模型不发生 OOM 或超时。

---

### 4.6 各阶段算力阶梯多资源池化与调度算法 (`scanner.debate.tiers`)

系统支持在流水线的每个阶段配置**多算力资源池化 (Multi-Resource Pooling)**，支持单一资源绑定或候选资源列表多选：

```yaml
scanner:
  debate:
    tiers:
      # Tier 1 初筛 (Hunter 角色，负责挖掘潜在漏洞)
      tier1_hunter:
        resource: "agy"                # 兼容单选字段
        resources: ["agy", "opencode"] # 推荐：多 Thick Agent 资源池化打散并发
        timeout_seconds: 1200          # 初筛单片超时时限 (秒)

      # Tier 2 推理与仲裁 (统一承载 Challenger 辩护与 Judge 终审)
      tier2_reasoning:
        resource: "native"             # 兼容单选字段
        resources: ["native", "agy"]   # 推荐：Thin LLM 为主力，Thick Agent 为容灾兜底
        timeout_seconds: 1800          # 辩论仲裁单片超时时限 (秒)

      # Tier 3 全仓态势汇总 (Synthesis 角色，负责报告整合排版)
      tier3_synthesis:
        resource: "native"             # 纯文本与 JSON 聚合，首选 Thin LLM
        resources: ["native"]
        timeout_seconds: 300           # 报告汇总超时时限 (秒)
```

#### 智能调度算法：
1. **容量加权最低负载率 (Capacity-Weighted Least-Loaded)**：
   调度器优先评估各节点的实时负载率 $\text{LoadRatio} = \frac{\text{Active}}{\text{Limit}}$，动态将任务分配给负载率最低的算力节点，防止“大池空闲、小池打满”的失衡现象。
2. **平滑加权轮询 (Smooth Weighted Round-Robin, SWRR)**：
   当多个算力节点同时存在空闲槽位时，基于节点的实时剩余空闲权重执行平滑加权轮询，避免突发并发流量瞬间全部击穿到单一节点。

---

### 4.7 场景级内嵌微任务专有路由 (`scanner.tools`)

流水线中存在高频、低延迟、确定性的微小任务（如 JSON 格式修复、缺陷指纹跨周期判定），系统提供专有微任务路由，**强制直传原生 Thin LLM**：

```yaml
scanner:
  tools:
    default_resource: "native"         # 默认统一走 native
    overrides:
      repair_json: "native"            # JSON 语法修复工具 -> 耗时由 15s 降至 < 800ms
      finding_match: "native"          # 缺陷跨周期比对 -> 纯文本毫秒级判定
      feedback_extraction: "native"    # 研发标记反馈时提炼正则是负样本 -> UI 交互即时响应
```

---

### 4.8 企业多任务治理与历史记忆闭环 (`governance`)

```yaml
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
```

---

### 4.9 事件通知服务 (`notification`)

```yaml
notification:
  webhook: "http://127.0.0.1:8081/api/notify/email" # 任务完成后的通知回调 Webhook
```

---

## 五、 系统诊断与应急运维自愈 (/admin/debug)

为了保障大仓高并发扫描的稳定可靠，系统在 **「系统诊断」 (`/admin/debug`)** 页面中提供了全方位的算力与队列透视能力。

### 5.1 算力槽位实时负载透视

系统诊断页面实时呈现 **LLM 模型调度器 (ModelDispatcher)** 的槽位健康状态：
- **资源标识 (Resource ID)**：展示如 `agy`、`opencode`、`native` 等节点标识；
- **驱动与映射模型 (Driver & Model)**：展示底层调用的具体模型（如 `gemini-3.7-flash`、`deepseek-v4`）；
- **实时槽位负载 (Active / Limit / Raw)**：
  - **Active**：当前正在执行中的任务数；
  - **Limit**：当前流控比例（Scale）折算后的允许最大并发；
  - **Raw**：物理配置的最大容量；
- **节点健康状态**：标识正常运行、满载排队或异常告警。

---

### 5.2 活跃槽位一键校准自愈 (Reset Active Slots)

在极端异常场景下（如热重载网络中断、底层宿主机偶发强杀子进程），若出现并发计数器悬挂未能正常扣减、导致新任务一直在排队等待时：
1. 超级管理员进入 **「系统诊断」** 页面；
2. 点击右上角的 **「校准活跃槽位」** 按钮；
3. 后端将安全调用 `POST /api/admin/debug/reset-slots`，一键重置所有算力节点的活跃计数（Active $\rightarrow$ 0），并向所有等待排队的协程发送全局唤醒广播（`Broadcast`），**秒级解除死锁挂死，无需重启整个后台服务**！

---

## 六、 缺陷生命周期与人机反馈记忆闭环

每次任务分析完成后，系统会自动执行增量比对状态机，并在界面上呈现闭环治理能力：

### 6.1 增量状态徽标 (Diff Status)
- `[NEW] 本次新增`：本次扫描首次发现的新缺陷；门禁流水线（`diff_gate_strict`）仅对此类新增缺陷进行红线拦截；
- `[EXISTED] 历史存量`：历史扫描已存在且未修复的持续性缺陷；
- `[RESOLVED] 已修复`：历史存量缺陷所在文件经本次成功扫描后确认已消除（内置**扫描范围守卫**，未扫描的文件绝不会误判为修复）；
- `[REOPENED] 复发激活`：曾经标记已修复或关闭的问题再次被检出。

---

### 6.2 智能体三方对抗事实链查看
在报告的详细清单中，点击卡片中的 **「🤖 智能体三方对抗事实链」**，可展开查阅：
- **🎯 Hunter 主张**：初筛猎手提出的漏洞成因与攻击输入假设；
- **⚖️ Challenger 辩护**：辩护人提出的前置断言、锁保护或宏隔离证据；
- **📜 Judge 裁决书**：终审法官出具的裁决依据、定级法理与权威修复代码建议。

---

### 6.3 研发人员一键标记反馈与规则沉淀
- 若某条发现经业务团队核实属于**误报 (False Positive)** 或 **已知业务设计豁免 (Won't Fix)**：
- 点击缺陷卡片右上角的 **「🛡️ 标记反馈」** 按钮；
- 填写排查理由并提交；
- 系统会自动将该缺陷指纹与文件路径沉淀为代码仓的 **负样本例外规则**，在后续所有扫描中**永久自动过滤，彻底杜绝重复上报打扰**！

---

## 七、 常见问题与排查指引 (FAQ)

### Q1: 近期代码检视为什么提示“过去 7 天内无代码提交，跳过扫描”？
**答**：这是系统的前置防御机制（Precondition Script）。若该代码仓在最近 7 天内无任何 Git Commit，系统会自动判定为无需重复检视并标记为 `SKIPPED`，为您节省算力。若需强制全量扫描，可在任务类型中选用普通代码检视任务或将 `since_days` 调大（如设为 `0` 代表全仓扫描）。

### Q2: 为什么有些头文件的缺陷会被归在 `src/` 分片中？
**答**：这是语义感知分片器的**跨目录同名投影特性**。为了让大模型在分析实现代码时拥有完整的类定义和结构体签名，系统特意将 `include/xxx.h` 与 `src/xxx.cc` 归并在同一个分片中一同提供给 AI，避免跨文件上下文割裂。

### Q3: 为什么 Tier 1 初筛必须配置 Thick Agent，而 Tier 2 和 Tier 3 强烈推荐 Thin LLM？
**答**：
1. **Tier 1 初筛阶段**：Prompt 中只传入待分析的文件清单，AI 必须具备本地文件系统读取权限，以便自主递归探索头文件声明、宏定义和函数调用链，因此**必须使用具备磁盘 I/O 能力的 Thick Agent**（如 `agy`、`opencode`）；
2. **Tier 2 辩论与 Tier 3 汇总阶段**：案卷材料（源码片段、猎手观点、辩护理由）已经在案卷结构体中全量内联，模型不需要任何本地磁盘访问，仅需进行纯文本逻辑推理与裁决，因此**优先选用高性能、低延迟、高并发的 Thin LLM (`native`)**，既能将推理耗时降至最低，又能彻底避免本地 CLI 子进程开销。

### Q4: 各阶段配置多个资源（Multi-Resource Pooling）是如何分流调度的？
**答**：当在某个阶段配置了多个候选资源（如 `resources: ["agy", "opencode"]`）时，调度器会采用**容量加权最低负载率 (Capacity-Weighted Least-Loaded)** 算法。系统根据每个节点的当前运行任务数和允许的最大槽位数，计算负载百分比，动态把任务分发给当前最清闲的节点；若多个节点负载相同，则使用平滑加权轮询（SWRR）均匀打散，有效防止单节点瓶颈或局部超载。

### Q5: 任务显示“排队中 (PENDING)”很久没有开始是什么原因？
**答**：
1. 请检查配置中心中的 `scanner.worker_count` 与 `llm.resources` 各节点的并发槽位数（`concurrent`）；若正在执行的任务已占满所有槽位，新任务会自动在内存队列中排队等待；
2. 检查是否命中了工作时间自动限流（`scanner.throttling.work_hours`），限流期间并发可能被压低至 10% 甚至完全暂停；
3. 进入 **「系统诊断」 (`/admin/debug`)** 查看是否有算力节点的活跃槽位挂起。如有异常悬挂，可点击 **「校准活跃槽位」** 一键复位。

### Q6: 底层某个物理 LLM 节点偶尔超时或报错，任务会直接失败吗？
**答**：不会。系统内置了多层容错韧性：
1. **分片级指数退避重试**：失败分片自动在后台退避重试（最多 3 次）；
2. **Native 端点故障转移 (Failover)**：`native` 模式下若主端点报 5xx 或网络超时，重试会自动切换至备用集群端点；
3. **辩护人降级**：若辩护人节点超时，法官会自动基于猎手原始材料与源码独立裁决；
4. **部分成功合成**：若全仓大部分分片成功，系统会优雅输出报告并附带未扫警告，保证整体任务不作废。

### Q7: 修改了 `config.yaml` 为什么前端配置中心没有立即变化？
**答**：这是系统的 **Database SSOT（数据库单一真值源）** 机制。`config.yaml` 仅在系统首次安装部署（数据库为空）时作为初始种子数据导入一次。系统初始化后，配置已持久化在数据库中，所有在线修改请直接在前端「配置中心 (`/admin/config`)」进行保存热重载。若需强制使用 `config.yaml` 中的配置覆盖数据库，可在配置中心点击「重置为预设种子」。

---

*如有更多配置疑问或定制化规则需求，请联系 Code-Shield 平台研发与架构支持团队。*
