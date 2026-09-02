# Code-Shield 原生 LLM 轻量执行引擎与混合调用架构设计专题

本专题收录了针对 Code-Shield 平台的底层 AI 执行引擎演进方案、原生 HTTP REST / gRPC 直接调用架构（LLM Native Invoker）设计、6 大轻量单轮任务场景落地规范以及“重型自主探索 Agent + 轻量原生 LLM”的动静分离混合调用（Hybrid Invocation）体系。

---

## 📚 专题目录索引与导读

```
native-llm-architecture/
├── README.md                                                 # 本文档：专题导航与核心设计摘要
├── 01-CodeShield-原生LLM轻量执行引擎与混合调用架构设计.md        # 🌟【核心设计】原生轻量 LLM 引擎、6大单轮场景与动静分离混合调用架构
└── 02-CodeShield-AI执行引擎与企业治理配置架构优化设计.md       # 🌟【配置架构】算力供给池、流水线阶梯编排与企业治理配置重构设计
```

---

## 🎯 核心成果与设计摘要

### 1. 痛点破局与动因 ([查看文档 01](01-CodeShield-原生LLM轻量执行引擎与混合调用架构设计.md))
*   **重型 Agent 瓶颈**：现存 4 大 AI 驱动（`claude`、`opencode`、`codex`、`agy`）基于 OS 进程（Fork/Exec）和本地状态管理，存在 **2~10s 进程启动冷开销**、**本地 SQLite 锁争抢**、**受限并发槽位** 以及 **单轮确定性任务内耗（如拉起 CLI 修复 JSON）** 等痛点。
*   **Thin LLM Engine**：基于标准 HTTP/2 REST 协议，实现长连接复用、毫秒级响应、原生 JSON Schema 结构化约束，彻底根治本地进程级隐患。

### 2. 六大 Thin LLM 轻量单轮场景支撑
1.  **JSON 语法与结构修复 (`RepairJSON`)**：零进程开销，毫秒级快速语法纠错。
2.  **缺陷指纹语义比对 (`askLLMIfSameFinding`)**：跨周期指纹相似度判定，纯文本二分类。
3.  **辩论终审法官 (`Judge Agent`)**：纯推理与规则裁决，输入上下文内联，原生 JSON Schema 输出。
4.  **辩护人反向抗辩 (`Challenger Agent`)**：快速反向推演保护性代码与宏隔离。
5.  **报告汇总与态势总结 (`Tier 3 Synthesis`)**：内存数据直传直出，消除临时文件与进程 I/O。
6.  **误报反馈规则提炼 (`Feedback Memory`)**：Few-shot 模式抽取，UI 交互即时响应。

> **边界澄清**：Hunter 初筛与深度扫描因 Prompt 仅传入文件名列表，Agent 必须自主从工作区读取并探索源码文件，**必须保持 Thick Agent 模式**。

### 3. 动静分离与场景级可配置化路由
*   **Thick Agent（重型/探索型）**：专注全局 Hunter 复杂跨文件污点深挖与自主代码读取；
*   **Thin LLM（轻量/推理型）**：专注辩护、仲裁、汇总、修复与匹配，原生保证强结构化输出。
*   **场景级灵活路由**：通过 `ai.tiers` 与 `ai.tool_backends` 协同，实现零侵入、按需渐进切换。

---

## 🚀 落地状态与收益实测 (Status: IMPLEMENTED)

截至 2026-09-02，该方案已在 `code-shield` 核心代码库中完成生产级落地交付：
1.  **原生引擎就绪**：[`services/native_cli.go`](file:///home/fugui/codes/code-shield/services/native_cli.go) 已注入 `AIInvoker` 体系，支持 Failover、指数退避与断路器平滑降级；
2.  **关键场景提速**：
    *   `RepairJSON` 耗时由 $8\text{s} \sim 25\text{s}$ 降至 $<800\text{ms}$（提速 $10 \sim 15$ 倍）；
    *   `askLLMIfSameFinding` 跨周期指纹语义比对耗时降至 $<500\text{ms}$；
    *   `executeSynthesisOnce` 报告汇总直传直出，消除磁盘临时中间文件；
3.  **测试全覆盖**：全量单元测试（[`services/native_cli_test.go`](file:///home/fugui/codes/code-shield/services/native_cli_test.go)）与全系统构建验证通过。

