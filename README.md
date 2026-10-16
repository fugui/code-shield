# 码盾 · Code-Shield 🛡️

[![Version](https://img.shields.io/badge/version-2.0.0-blue.svg)](file:///home/fugui/codes/code-shield/README.md)
[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8.svg)](file:///home/fugui/codes/code-shield/go.mod)
[![React](https://img.shields.io/badge/Frontend-React%2018%20%7C%20Vite%205-61DAFB.svg)](file:///home/fugui/codes/code-shield/frontend/package.json)

**码盾**（Code-Shield）是一套由 AI 驱动的企业级代码质量、安全扫描与全生命周期缺陷治理系统。系统基于微前端架构设计，深度集成于 `code-bench` 开发者综合工作台。

自 **v2.0** 架构演进以来，码盾全面升级为**多智能体对抗辩论（Multi-Agent Debate）**与**分层算力体系（Tiered Computing Pipeline）**，并首创**声明式 SPI 插件架构**与**物理槽位极简缺陷对账机制（Slot-Based Reconciliation）**。系统原生支持 **Google Antigravity (AGY)**、**OpenCode**、**Codex**、**Claude Code** 以及高性能 **原生 Native LLM 引擎**，对海量代码仓进行多维度深度检视与单元测试有效性评估，全面闭环“初筛 - 辩护 - 裁决 - 归因 - 对账 - 治理”全生命周期。

---

## 🌟 核心特性

### 🤖 1. 多智能体对抗辩论与 AI 执行引擎族
- **多智能体三阶段对抗辩论（Multi-Agent Debate Pipeline）**：
  - **Hunter（初筛探索）**：利用 Thick Agent 自由穿梭与探索源码上下文，发掘深层潜在代码缺陷。
  - **Defender/Challenger（辩护对抗）**：针对初筛嫌疑单，快速多维度寻找代码防御证据与上下文反证，发起交叉质询。
  - **Judge（终审法官）**：由独立权威模型基于指控与辩护证据链进行结构化裁决，彻底抑制 LLM 幻觉与误报。
  - **0 候选快速放行（Fast-Pass）**：初筛无异常分片毫秒级放行，节省 80%+ 辩论阶段物理算力。
- **全矩阵 AI 执行器驱动（Invoker Matrix）**：
  - **Google Antigravity (AGY) CLI**：原生支持 Antigravity 深度代码推理与交互式探索引擎。
  - **Native LLM 执行引擎（Thin LLM）**：基于标准 HTTP/SSE 协议直连大模型 API，支持多异构 Endpoint 独立 BaseURL/APIKey、加权轮询负载打散与透明故障转移。
  - **OpenCode CLI**：轻量级无状态基座 Agent 注入，优化并发 SQLite 锁、失败会话续跑（Continuation）与输出契约守护。
  - **Codex CLI**：支持 OpenAI Codex 免交互 headless 运行，完美兼容只读沙箱与 `danger-full-access` 权限模式。
  - **Claude Code CLI**：系统提示词内联注入与流式 JSON 解析。

### ⚡ 2. 4 级分层算力阶梯与调度架构 (Tiered Computing)
- **解耦式算力阶梯配置**：
  - **Tier 1 (Hunter)**：负责源码初筛，支持分片二分重试（Split on Timeout）与空闲看门狗（Idle Watchdog）。
  - **Tier 2 (Challenger)**：负责辩护对抗，推荐低延迟高吞吐 Thin LLM。
  - **Tier 3 (Judge)**：负责终审裁决，采用旗舰推理模型进行事实性仲裁。
  - **Tier 4 (Synthesis)**：负责全仓态势综合汇总，生成结构化总结报告。
- **动态背压与容灾恢复机制**：
  - 跨 Tier 调度具备背压感知，防止下游裁判队列积压；失败分片具备类型化重试、候补资源自动故障切换与局部精准恢复。

### 🔌 3. 声明式 SPI 插件架构与任务形态差异化扫描
- **核心调度去特化**：
  - 剥离核心调度中针对特定语言与任务形态的硬编码规则，抽象出标准的 Gate（门禁）、Verifier（复核器）及 Profile（画像）插件扩展点。
- **内置 10 大专项检视策略**：
  - 覆盖增量变更审查 (`change-review`)、全仓深度检视 (`deep-review`)、Coredump 风险 (`coredump-risk`)、内存泄漏 (`memory-leak`)、多线程并发安全 (`thread-create`)、浮点比较精度失真 (`float-comparison`)、cJSON 安全解析 (`cjson-scan`)、无序集合遍历风险 (`unordered-collection`) 以及单元测试质量/有效性评估 (`ut-quality`, `ut-effectiveness`)。
- **单元测试有效性评估专项引擎**：
  - 支持实体测试用例动态切分与容灾重试；内置微空桩确诊、防弱断言（Weak Assertion）门禁、控制流护栏与契约自愈定向修复管线。
- **精准 Git 增量变更提取**：
  - 支持基于 `SinceDays` 与 `DiffBase` 提取差异变更，精准锁定修改文件与上下文切片。

### 🎯 4. 极简缺陷对账架构与确定性指纹治理 (Slot-Based Reconciliation)
- **纯 Go 物理词法定桩与两级槽位匹配**：
  - 告别高复杂度的模糊几何对账，升级为纯 Go 物理词法扫描（Lexer-based Anchor），具备跨行宏穿透与抗代码抖动能力。
  - 引入离散两级槽位匹配（Slot-Based Matching），彻底解决跨版本扫描行号漂移、增量漏对账与历史缺陷重复判定的痛点。
- **统一缺陷台账 SSOT 与稀疏存储**：
  - 缺陷台账稀疏存储（Sparse Storage）、短事务解耦与快照 TTL 分级归档，有效消除存储膨胀与数据库高压。
  - 缺陷全闭环追踪：支持源文件删除自动关闭关联缺陷、追踪最近确认时间与累计确认次数。
- **缺陷分类学与确定性校准器（Calibrator）**：
  - 缺陷分类代码强绑定与推导准则，归一化致命/严重/一般/建议四级标准风险。

### 🖥️ 5. 沉浸式扫描工作台与端到端观测遥测
- **实时扫描工作台（Scan Workbench）**：
  - 任务日志展开面板重构为沉浸式全景工作台，分片矩阵并发状态、多智能体阶段流向与实时缺陷流一览无余。
  - 支持全量裁判事实链归档（debate-chunks 与 debate-verdicts.log），阶段耗时与执行链路通过可视化表格清晰呈现。
- **报告中心与协作体验**：
  - 任务报告新增多智能体对抗辩论轨迹 TAB，支持全局流水号（#1, #2...）追踪裁决事实。
  - 缺陷机理分析、防御依据、终审结论分层结构化展示；全量支持 URL 直达、问题快速定位与一键分享链接。
  - 企业级多格式导出：支持 Markdown、JSON、印刷级 PDF，以及含【检视概要】与【问题明细】双 Sheet 的 Excel。

### 🏢 6. 专项治理攻坚与企业级治理中心
- **专项缺陷审计工作区**：
  - 专为大规模治理战役设计，支持多维度过滤、指派处理人与状态流转历史追踪。
- **部门排行榜与折叠大纲战报**：
  - 统计未关闭缺陷数与审计缺陷数，Excel 战报升级为单 Sheet 二级可折叠大纲数据表。
- **生产级弹性调度与安全管控**：
  - 支持任务队列优雅排空（Drain Mode）与工作时间自动限流避峰；废除粗暴的全局熔断，采用自适应暂停退避重试。
  - 操作触发审计（Audit Logs）全面记录，权限严格管控。

---

## 🏗️ 系统架构

```text
┌────────────────────────────────────────────────────────┐
│               前端展现层 (React 18 / Vite 5)            │
│  - 沉浸式扫描工作台  - 报告中心 (辩论轨迹)  - 缺陷审计工作区 │
│  - 纯原生 Vanilla CSS  - Design Tokens  - @code/common   │
└───────────────────────────┬────────────────────────────┘
                            │ HTTP / REST API
┌───────────────────────────▼────────────────────────────┐
│              code-shield 核心服务 (Go 1.25+)           │
│                                                        │
│  ┌──────────────────────────────────────────────────┐  │
│  │ Dispatcher 调度器 & 持久化任务队列 (DB-backed)     │  │
│  │ (工作时间限流 / 优雅排空 / 背压控制 / 失败恢复)     │  │
│  └────────────────────────┬─────────────────────────┘  │
│                           │                            │
│  ┌────────────────────────▼─────────────────────────┐  │
│  │ 声明式 SPI 扫描引擎族 (services/engines)          │  │
│  │ ├─ DebateEngine (Hunter / Challenger / Judge)    │  │
│  │ ├─ AssessmentEngine (单测有效性 / 门禁 / 修复)   │  │
│  │ ├─ Single & Chunked 智能分片引擎                 │  │
│  │ └─ SPI 插件层 (Gate 前置门禁 / Verifier 复核)    │  │
│  └────────────────────────┬─────────────────────────┘  │
│                           │                            │
│  ┌────────────────────────▼─────────────────────────┐  │
│  │ 极简缺陷对账与治理中心 (services/defectlifecycle)   │  │
│  │ ├─ 物理词法定桩 (Lexer Anchor) & 槽位匹配 (Slot) │  │
│  │ ├─ 缺陷台账 SSOT & 稀疏存储 (Sparse Retention)   │  │
│  │ └─ 分类学治理 (Taxonomy) & 确定性校准器          │  │
│  └────────────────────────┬─────────────────────────┘  │
│                           │ Invoker 抽象适配驱动       │
└───────────────────────────┼────────────────────────────┘
                            │
       ┌────────────────────┴────────────────────┐
       ▼                                         ▼
┌───────────────────────────────┐     ┌───────────────────────┐
│ 异构 LLM 算力池 (llm.resources)│     │ 外部支撑体系          │
│ ├─ Native Thin 集群 (OpenAI)  │     │ ├─ PostgreSQL 共享库  │
│ ├─ Antigravity (AGY) CLI      │     │ ├─ code-notifier      │
│ ├─ OpenCode CLI               │     │ │  (邮件通知微服务)   │
│ ├─ Codex CLI (Headless)       │     │ └─ 本地代码缓存与产物 │
│ └─ Claude Code CLI            │     │    (codes/ & reports/)│
└───────────────────────────────┘     └───────────────────────┘
```

| 组件 | 技术栈 | 默认端口 | 说明 |
| :--- | :--- | :--- | :--- |
| **code-shield** | Go 1.25+, Gin, GORM, PostgreSQL | `8080` | 核心服务：包含 SPI 扫描引擎、调度队列、缺陷对账与治理 API |
| **frontend** | React 18, Vite 5, TS, Lucide, @code/common | `5173` | Web 控制台，基于纯原生 Vanilla CSS，支持嵌入 `code-bench` |
| **notifier** | Go, Win32 GUI, Outlook COM | `8081` | 独立的邮件投递微服务（Windows / Linux Webhook） |

> 📚 **系统深度架构与设计文档中心**：详见 **[docs/README.md](docs/README.md)**（包含 6 大体系：使用手册、核心流水线、扫描引擎 SPI、算力调度、指纹对账治理、多租户设计与 RFC 规范）。

---

## ⚙️ 系统配置指南 (config.yaml)

系统采用**动静分层**配置规范：基础设施静态低频配置直接读取 `config.yaml`；模型与扫描策略在首次初始化时写入数据库，后续直接在 Web 界面实时热调整。

```yaml
# ── 1. 基础设施层 (静态冷配置，修改需重启) ──
server:
  port: ":8080"
  external_url: "http://192.168.56.18:8080"
  read_timeout: 120s
  write_timeout: 120s

storage:
  root: "."                           # 数据根目录，自动创建 codes/ 与 reports/

database:
  driver: "postgres"
  host: "127.0.0.1"
  port: 5432
  user: "postgres"
  password: "YOUR_POSTGRES_PASSWORD"
  dbname: "code_shield"
  sslmode: "disable"
  timezone: "Asia/Shanghai"
  max_open_conns: 50
  max_idle_conns: 10

auth:
  jwt_secret: "YOUR_SHARED_JWT_SECRET" # 必须与平台保持一致
  password_login_enabled: true
  admin_list:
    - "admin@code-shield.com"

# ── 2. 大模型与算力供给池 (首次导入种子模版) ──
llm:
  default_resource: "native"
  resources:
    - id: "agy"                        # Antigravity CLI 节点 (探索型 Thick Agent)
      driver: "agy"
      model: "gemini-3.7-flash"
      concurrent: 5
    - id: "opencode"                   # OpenCode 算力节点
      driver: "opencode"
      model: "modelgate/GLM-4.7-XS"
      concurrent: 5
    - id: "native"                     # 原生 Thin LLM 高并发算力集群
      driver: "native"
      concurrent: 10
      endpoints:
        - name: "deepseek-primary"
          base_url: "https://api.deepseek.com/v1/chat/completions"
          api_key: "sk-xxxxxxxx"
          model: "deepseek-v4-flash"
          concurrent: 10
          weight: 100

# ── 3. 智能扫描引擎与 4-Tier 辩论流水线 ──
scanner:
  worker_count: 5                     # 全局并发任务 Worker 数
  max_queue_size: 2000                # 排队上限 (-1 为不限)
  debate:
    enabled: true                     # 开启多智能体对抗辩论
    fast_pass_enabled: true           # 0 候选快速放行 (节省 80%+ 辩论开销)
    max_candidates_per_chunk: 20      # 单分片最大仲裁候选数
    tiers:
      tier1_hunter:                   # Tier 1 源码初筛
        resources: ["agy", "opencode"]
        timeout_seconds: 7200
        timeout_recovery: "split"     # 超时自动二分拆片
      tier2_challenger:               # Tier 2 辩护对抗
        resource: "native"
        timeout_seconds: 1200
      tier3_judge:                    # Tier 3 终审法官
        resource: "agy"
        timeout_seconds: 1800
      tier4_synthesis:                # Tier 4 态势汇总
        resource: "native"
        timeout_seconds: 300

# ── 4. 企业治理与台账快照生命周期 ──
retention:
  ledger_retention_days: 30           # 历史快照保留天数
  max_retained_scans_per_repo: 10     # 代码仓保底保留轮次
  enable_sparse_scope: true           # 启用稀疏存储节省空间

notification:
  webhook: "http://192.168.56.18:8081/api/notify/email"
```

---

## 🛠️ 快速开始

### 1. 构建与编译
```bash
# 一键完成前端构建打包与后端 Go 编译
make build
```

### 2. 运行自动化测试与架构门禁
```bash
# 运行后端全量单元测试
make test

# 运行架构防腐门禁检查 (ArchGuard)
make lint-arch
```

### 3. 运行服务
```bash
# 启动码盾核心服务
make run
```
默认监听 `:8080`，管理员初始账号：`admin@code-shield.com` / `admin123`。

### 4. 前端独立开发
```bash
cd frontend
npm install
npm run dev
```

---

## 🌐 子路径（Sub-path）部署

系统原生支持子路径打包（例如嵌入网关 `/shield/`）：
```bash
# 注入子路径环境变量构建前端
VITE_BASE_PATH=/shield/ make build
```

Nginx 反向代理配置示例：
```nginx
server {
    listen 80;
    server_name 192.168.56.18;

    location /shield/ {
        alias /path/to/code-shield/frontend/dist/;
        index index.html;
        try_files $uri $uri/ /shield/index.html;
    }

    location /shield/api/ {
        proxy_pass http://127.0.0.1:8080/api/;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

---

## 📁 目录结构

```text
code-shield/
├── main.go                     # API 集中路由配置与服务启动入口
├── Makefile                    # 统一构建、测试、架构防腐门禁 (lint-arch) 与打包
├── config.yaml[.example]       # 动静分层系统配置与示例模板
├── cron_jobs/                  # 定时任务调度器与日常巡检清理作业 (jobs.go)
├── tasks/                      # 内置 10 大专项检视策略提示词与规约模板库
│   ├── change-review/          # 增量变更代码审查 (支持 SinceDays / DiffBase)
│   ├── deep-review/            # 全仓深度代码质量审查
│   ├── coredump-risk/          # Coredump 与野指针风险排查
│   ├── memory-leak/            # 内存泄漏与资源未释放排查
│   ├── thread-create/          # 多线程与并发竞争看护
│   ├── float-comparison/       # 浮点数比较精度失真排查 (支持 C/C++)
│   ├── cjson-scan/             # cJSON 解析与边界溢出安全扫描
│   ├── unordered-collection/   # 无序集合遍历风险扫描
│   ├── ut-effectiveness/       # 单元测试用例有效性评估
│   └── ut-quality/             # 单元测试用例设计质量评估
├── models/                     # GORM 数据模型及全局配置单例
│   ├── config.go               # 运行时配置解析与动态单例
│   ├── db.go                   # PostgreSQL 连接池与初始化
│   ├── models.go               # Task, Schedule, Issue, TriggerLog 等核心实体
│   ├── ledger_schema.go        # 统一缺陷台账 (Ledger) 持久化模型
│   ├── chunk_execution.go      # 分片原子执行记录与重试状态追踪
│   └── artifact_repair.go      # 契约修复与产物审计实体
├── handlers/                   # HTTP REST API 控制层
│   ├── task.go                 # 扫描任务创建、取消、重试与局部恢复
│   ├── report_handler.go       # 任务报告领域聚合与多格式导出入口
│   ├── debate_handler.go       # 多智能体辩论轨迹数据拉取接口
│   ├── defect_handler.go       # 缺陷问题追踪与状态流转
│   ├── workbench.go            # 开发者治理工作台与缺陷处理接口
│   ├── campaign_generic.go     # 专项治理攻坚会战大盘与统计接口
│   ├── campaign_excel.go       # 会战专项折叠大纲 Excel 战报导出
│   ├── category_governance.go  # 缺陷类目分类学治理与规则映射
│   ├── feedback_handler.go     # 误报反馈与分类校准接口
│   ├── config.go               # 动态配置中心与运行时更新
│   └── trigger_log.go          # 触发日志与安全操作审计
├── services/                   # 核心领域业务服务与引擎架构
│   ├── dispatcher/             # 任务队列调度、优雅排空 (Drain)、算力池与背压管控
│   ├── engines/                # AI 扫描执行引擎族
│   │   ├── debate/             # 多智能体对抗辩论引擎 (Hunter / Challenger / Judge)
│   │   ├── assessment/         # 测试用例有效性评估与契约自愈定向修复引擎
│   │   ├── plugins/            # 声明式 SPI 插件架构 (前置门禁 Gates / Verifiers)
│   │   ├── chunked/            # 大型项目自动分片并发分析引擎
│   │   ├── single/             # 小型仓库单体分析引擎
│   │   ├── planner/            # 任务切片与分析规划器
│   │   ├── profile/            # 代码仓特征画像分析引擎
│   │   └── chunker/            # 基于 AST 语法的智能代码切片器
│   ├── invoker/                # AI 执行驱动适配器 (AGY / OpenCode / Codex / Claude / Native)
│   ├── runner/                 # 任务流水线执行编排 (analysis / synthesis / git 同步)
│   ├── defectlifecycle/        # 极简缺陷对账架构 (物理词法定桩 anchor, 离散槽位 slot, 台账 ledger)
│   ├── governance/             # 缺陷类目分类学治理、语义匹配与确定性校准 (Calibrator)
│   ├── coverage/               # 扫描覆盖度计算与代码树对齐机制 (Reconciliation)
│   ├── queue/                  # 持久化任务队列模型与执行状态快照
│   └── reports/                # 企业级多格式报告导出中心 (Excel / Markdown / JSON / Archive)
├── docs/                       # 系统架构、RFC、方法论与开发指南中心
│   ├── 01-guides/              # 智能扫描使用手册与任务配置指南
│   ├── 02-architecture/        # 核心流水线、扫描引擎、调度器、对账治理与多租户架构设计
│   ├── 03-rfcs/                # 架构设计 RFC 提案中心 (RFC-001 ~ RFC-004)
│   ├── 04-reports/             # 压测基准、事故复盘与里程碑落地复盘
│   ├── 05-methodology/         # 与 AI 结对架构设计实战指南与方法论
│   └── 06-reviews/             # 架构深度检视与副作用评估报告
└── frontend/                   # React 18 前端 SPA (纯原生 Vanilla CSS，无第三方 UI 框架捆绑)
    ├── src/pages/              # 业务路由页面 (Scan 扫描工作台, Workbench, Campaign, Reports 等)
    ├── src/components/         # 业务公共组件 (report 报告中心, debate 辩论轨迹, defects 缺陷)
    └── src/styles/             # 本地与全局样式 (严格对齐 Design Tokens)
```

---

## 🏷️ 版本历史

### v2.0.0 (2026-10-17)
- **多智能体对抗辩论流水线（Multi-Agent Debate Pipeline）全面落地**：
  - 引入 **Hunter（初筛探索）**、**Defender/Challenger（辩护对抗）** 与 **Judge（终审法官）** 三阶段多智能体质证机制，显著压制大语言模型的幻觉与代码误报率。
  - 支持 **0 候选快速放行（Fast-Pass）** 机制，无嫌疑分片毫秒级放行，节省 80%+ 的辩论阶段物理算力。
  - 完整归档全量裁决事实链（`debate-chunks` 与 `debate-verdicts.log`），分片执行具备全局连续流水序号（`#1`, `#2`...）。
- **4 级算力阶梯与异构 LLM 混合调用架构（Tiered Computing Architecture）**：
  - 构建 **Tier 1 (初筛)**、**Tier 2 (辩护)**、**Tier 3 (裁决)** 与 **Tier 4 (态势汇总)** 四级流水线阶梯，各阶梯具备独立的超时、重试退避与候补资源池切换策略。
  - 引入阶段空闲看门狗（Idle Watchdog）与分片超时二分重试（Split on Timeout），兼顾分析吞吐与极端大分片容错。
- **高性能原生 Native LLM 轻量执行引擎（Thin LLM Engine）**：
  - 摆脱对重量级 CLI 外部进程的单一依赖，直接通过标准 HTTP/SSE 协议直连 OpenAI / DeepSeek 等兼容接口。
  - 采用动静分离架构，支持配置多异构 Endpoint 独立 BaseURL / APIKey / Model，内置加权轮询负载打散与故障自动转移。
- **全矩阵 AI 执行器生态全面完善**：
  - 原生适配 **Google Antigravity (AGY) CLI**，提供强大的代码交互探索与推理能力。
  - 深度优化 **OpenCode CLI**：解决高并发 SQLite 锁冲突，支持失败会话原地续跑（Continuation）与输出契约守护。
  - 深度优化 **Codex CLI**：支持免交互 headless 模式，兼容 Ubuntu 24.04 `danger-full-access` 沙箱与只读隔离。
- **声明式 SPI 插件架构与任务形态差异化扫描**：
  - 调度核心去特化，抽象出标准的 Gate（门禁）、Verifier（复核器）及 Profile（画像）插件契约扩展点。
  - 单元测试有效性评估全面升级：支持实体级用例动态拆分与容灾重试，内置微空桩确诊、防弱断言（Weak Assertion）门禁、控制流护栏与契约自愈定向修复管线。
  - 支持 Git 增量变更提取（`SinceDays` / `DiffBase`），为增量审查启用精准分片。
- **极简缺陷对账架构（Slot-Based Reconciliation）与统一缺陷台账**：
  - 告别复杂的空间几何距离算法，演进落地**纯 Go 物理词法定桩**（穿透 C++ 跨行宏）与**两级离散槽位匹配**，认知降维，彻底攻克代码行号抖动漂移与增量漏对账难题。
  - 统一缺陷台账 SSOT 结构，实现**覆盖明细稀疏存储（Sparse Storage）**、短事务解耦与快照 TTL 分级归档，有效消除存储膨胀与数据库压力。
  - 完善缺陷闭环机制：支持源文件删除自动关闭缺陷、历史确认次数与最近确认时间追踪。
- **沉浸式扫描工作台（Scan Workbench）与端到端观测遥测**：
  - 任务日志面板重塑为全景扫描工作台，分片矩阵并发状态、多智能体辩论时序与实时缺陷流一览无余。
  - 任务报告中心新增**多智能体对抗辩论轨迹 TAB**，法官裁决与问题机理结构化分层渲染。
  - 诊断报告阶段执行链路与耗时明细重构为表格化展示，清晰区分流水线阶段与异常重试。
- **专项治理攻坚会战大盘与导出引擎升级**：
  - 专项缺陷审计工作区支持 URL 直达、一键分享链接及实体评估模式下致命/严重指标拓展。
  - 部门治理排行榜支持未关闭缺陷统计，Excel 导出全面升级为单 Sheet 二级可折叠大纲数据表。
- **生产级弹性调度与安全防护**：
  - 任务队列新增**优雅排空模式（Drain Mode）**，保障服务维护期间在途任务安全收敛。
  - 废除粗暴的全局熔断机制，全面改用出错自适应暂停退避重试；完善工作时间自动限流避峰机制。
- **技术债务全面清零与工程规范化（RFC-004 落地）**：
  - 彻底清理启动期废弃 DDL 探测与旧业务实体（KeyIssue 等），消除双轨镜像兼容层，归一 SSOT 配置体系。
  - 前端彻底清理内联静态样式与 JS 模拟 hover，删除冗余专用 CSS，全面对齐 Design Tokens 语义规范。

### v1.6.0 (2026-08-23)
- **任务报告中心统一重构 (Task Report Center Refactor)**：
  - 彻底重构统一了任务报告抽屉与全屏详情查看器（`ReportViewer`），统筹收拢了全局 7 个业务页面的报告入口。
  - 全新设计并拆分三大核心模块：`📑 审计总结报告 (Summary)`、`📋 详细问题清单 (Findings)`、`🔬 运行轨迹与诊断 (Diagnostics)`。
  - 引入统一领域模型 DTO（`TaskReportMeta`、`SummaryDTO`、`FindingsPageResponse`、`DiagnosticsDTO`），杜绝大 JSON 一次性拉取性能瓶颈，支持服务端轻量分页（50 条/页）与级别/状态多维过滤。
- **企业级多格式报告导出引擎 (Multi-Format Exporter Engine)**：
  - 搭建独立报告导出引擎，支持 `Markdown`、`JSON`、`Excel (.xlsx)` 及出版级 `PDF 打印` 多格式导出。
  - Excel 报告内置【检视概要看板】与【问题明细清单】双 Sheet 豪华排版，自适应中文双字节列宽。
- **现代化 CI/CD 流水线诊断看板 (Pipeline Stepper Flow)**：
  - 全新设计流水线阶段时序流（`STAGE 01/02/03`、微光状态徽章、耗时胶囊），支持分片矩阵并发状态与终端输出日志优雅折叠。
- **高质感卡片布局与微前端样式强隔离**：
  - 抽屉视窗宽度扩充至 `min(1200px, 95vw)`，采用科技浅灰底色（`#f1f5f9`）衬托白底圆角大卡片，告别局促拥挤感。
  - 彻底解决微前端模块联邦（Module Federation）样式时序竞争导致的界面跳跃问题，固化组件级内边距与标准 Checkbox 勾选交互。
- **100% 确定性路径寻址 (Deterministic Path Resolution)**：
  - 彻底消除全部 `filepath.Glob` 模糊匹配依赖，历史产物与标准产物路径 100% 确定性寻址。

### v1.5.0 (2026-08-14)
- **数据库持久化任务队列 (DB Persistent Queue)**：将扫描任务队列全面升级为基于 PostgreSQL 数据库持久化拉取与原子抢占模型，新增 `max_queue_size` 限额保护，杜绝重启任务丢失。
- **分片失败恢复与日志标记 (Failed Chunk Recovery)**：支持对偶发失败的分片任务发起精确恢复，在执行日志中高优标记并通过队列原子抢占调度。
- **工作时间自动限流 (Work Hours Throttle)**：支持在配置的工作时间段内自动降低并发占用比例，非工作时间自动恢复 100% 满速扫描。
- **AI 扫描并发流控死锁修复**：彻底修复并发流控到期死锁缺陷，引入定时唤醒与持久化状态。
- **全平台公共库接入**：全面接入 `code-common/backend` 数据模型与鉴权中间件，前端统一采用 `@code/common` 的 `Pagination` 与 `ErrorBoundary`。
- **交互与体验优化**：在审计工作区、个人工作台与公开报告页面新增一键复制精确文件名与行号功能（Tooltip 动态呈现），缺陷分析指派默认处理人设定为当前登录用户。

### v1.4.0 (2026-08-08)
- **公开接口安全收紧**：收紧任务与漏洞查询公开接口，强制要求 JWT / SSO 登录认证。
- **多模型并发负载映射**：支持在 `config.yaml` 中配置多 LLM 服务器端点与差异化并发额度。
- **管理员种子账号初始化优化**：优化默认 admin 账号初始化逻辑，精确匹配邮箱并赋予 `super_admin` 角色与 `IsAdmin` 标记。

### v1.3.0 (2026-07-31)
- **触发日志与操作审计 (Audit Logs)**：新增任务触发历史日志功能，所有手动与定时触发事件均通过 `TaskTriggerLog` 模型进行结构化审计记录。
- **权限控制收紧**：将触发日志仅对超级管理员（`super_admin`）开放，并提供一键清除历史日志功能。
- **专项分析导出性能优化**：优化 Excel 导出列宽计算性能，彻底解决大批量数据导出 502 超时问题。

### v1.2.0 (2026-07-05)
- **专项分析漏洞报告导出**：支持漏洞报告导出为 Excel / CSV 格式。
- **提示词规范化**：多线程审计提示词优化，推荐平台内置调度组。

### v1.1.0 (2026-05-31)
- **以报告为入口的全新概览**：重塑报告列表页面，支持报告 ID 抽屉快速滑出详情与精确缺陷统计。
