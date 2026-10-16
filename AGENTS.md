# AGENTS.md - 码盾 (Code-Shield) 🛡️ 智能体与开发者指南

本文档专为参与 **Code-Shield** 开发的 AI 助手与团队成员制定，聚焦 Code-Shield 专有的系统架构、核心领域划分与防腐工程约束。

> 💡 **基础规章约定**：工作区通用编码风格、Design Tokens 语义化变量、标准分页规范、中文沟通及 Conventional Commits 提交规范，请统一严格遵循工作区通用准则（参见 `code-common/rules/AGENTS.md`）。

---

## 📌 1. 项目定位与核心技术栈

**码盾（Code-Shield）** 是一套由 AI 驱动的企业级代码质量与安全自动化看护系统，无缝嵌入 `code-bench` 开发者综合工作台。

* **后端服务**：Go 1.22+, Gin, GORM v2, PostgreSQL
* **前端工程**：React 18, Vite 5, TypeScript, Lucide React, `@code/common`（纯原生 Vanilla CSS 自研组件体系，无 AntD 等第三方重量级 UI 框架捆绑）
* **AI 调度适配**：适配无状态 Claude Code CLI, OpenCode CLI, Codex CLI 等执行器

---

## 🗂️ 2. 关键目录结构与职责划分

```text
code-shield/
├── main.go                     # API 路由集中注册与服务启动入口
├── Makefile                    # 统一构建、测试、门禁 (lint-arch) 与打包入口
├── config.yaml[.example]       # 运行时配置文件模板
├── models/                     # GORM 数据模型及全局配置单例 (config.go, db.go, models.go)
├── handlers/                   # HTTP 控制器与鉴权中间件 (AuthMiddleware, workbench, task, report 等)
├── cron_jobs/                  # 定时任务调度器与日常巡检清理作业
├── tasks/                      # 内置 10 大专项检视策略 (change-review, deep-review, ut-effectiveness 等)
├── services/                   # 核心领域服务架构层
│   ├── dispatcher/             # 持久化任务队列抢占调度、租约控制、失败恢复 (recovery)、算力阶梯
│   ├── engines/                # AI 扫描执行引擎族
│   │   ├── single/             # 小型仓库单体分析引擎
│   │   ├── chunked/            # 大型项目自动分片并发分析引擎
│   │   ├── debate/             # 双模型多轮对抗辩论引擎
│   │   ├── assessment/         # 测试用例有效性评估与契约自愈引擎
│   │   ├── planner/            # 任务分析规划引擎
│   │   ├── profile/            # 仓库特征画像分析引擎
│   │   └── chunker/            # 代码 AST 语法树智能分片器
│   ├── invoker/                # 底层 AI 执行驱动适配器 (Claude / OpenCode / Codex / Native / AGY)
│   ├── runner/                 # 任务流水线执行编排 (analysis 切片分析 / synthesis 汇总 / git 同步 / 恢复)
│   ├── defectlifecycle/        # 缺陷全生命周期追踪 (指纹锚点 anchor, 台账 ledger, 行号稳定器 stabilizer, 会战 campaign)
│   ├── governance/             # 缺陷类目治理中心 (分类学 taxonomy, 语义匹配, 误报校准 calibrator, 反馈学习)
│   ├── coverage/               # 扫描覆盖度计算与分析对齐机制 (reconciliation)
│   ├── queue/                  # 持久化任务队列模型与执行状态快照
│   └── reports/                # 企业级多格式报告导出中心 (Excel / Markdown / JSON / Archive)
├── docs/                       # 系统架构、RFC、方法论与开发指南中心
└── frontend/                   # React 18 前端工程 (纯原生 Vanilla CSS，无第三方 UI 框架)
    ├── src/pages/              # 业务路由页面 (Workbench, Campaign, Reports, Scan, ConfigCenter 等)
    ├── src/components/         # 业务公共组件 (report 报告中心, defects 缺陷, execution 轨迹看板)
    └── src/styles/             # 本地与全局样式
```

---

## 🛡️ 3. 架构防腐红线 (ArchGuard 静态检查)

执行 `make lint-arch` 时系统会自动校验以下边界，**严禁违反**：
1. **引擎解耦**：`services/engines/` 下的扫描执行引擎**绝对禁止直接访问 `models.DB`**（必须由上层 dispatcher 注入或通过隔离接口操作）。
2. **驱动层独立**：`services/invoker/` CLI 驱动适配器**绝对禁止反向依赖 `engines` 或 `runner`**。
3. **避免循环依赖**：`services/runner/` **绝对禁止循环依赖 `code-shield/services` 根包**。

---

## 🛠️ 4. Code-Shield 专有开发与测试命令

* **全量编译打包**：
  ```bash
  make build
  ```
* **独立编译后端与前端**：
  ```bash
  make backend
  make frontend
  ```
* **运行架构防腐门禁检查 (ArchGuard)**：
  ```bash
  make lint-arch
  ```
* **运行特定领域模块测试**：
  ```bash
  go test -v ./models/...
  go test -v ./services/dispatcher/...
  go test -v ./services/defectlifecycle/...
  ```
* **前端静态检查与风格校验**：
  ```bash
  make lint
  ```
