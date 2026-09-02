# Code-Shield 原生 LLM 轻量执行引擎与混合调用架构设计专题

本专题收录了针对 Code-Shield 平台的底层 AI 执行引擎演进方案、原生 HTTP REST / gRPC 直接调用架构（LLM Native Invoker）设计、5 大轻量单轮任务场景落地规范以及“重型自主探索 Agent + 轻量原生 LLM”的动静分离混合调用（Hybrid Invocation）体系。

---

## 📚 专题目录索引与导读

```
native-llm-architecture/
├── README.md                                                 # 本文档：专题导航与核心设计摘要
├── 01-CodeShield-原生LLM轻量执行引擎与混合调用架构设计.md        # 🌟【核心设计】原生轻量 LLM 引擎、5大单轮场景与动静分离混合调用架构
└── 02-Thin-LLM-Engine设计批判性审视与场景配置方案.md             # 🔍【批判审视】必要性/完备性/准确性三维审视、场景级 Thin/Thick 可配置化路由方案
```

---

## 🎯 核心成果与设计摘要

### 1. 痛点破局与动因 ([查看文档 01](01-CodeShield-原生LLM轻量执行引擎与混合调用架构设计.md))
*   **重型 Agent 瓶颈**：现存 4 大 AI 驱动（`claude`、`opencode`、`codex`、`agy`）基于 OS 进程（Fork/Exec）和本地状态管理，存在 **2~10s 进程启动冷开销**、**本地 SQLite 锁争抢**、**受限并发槽位** 以及 **单轮确定性任务内耗（如拉起 CLI 修复 JSON）** 等痛点。
*   **Thin LLM Engine**：基于标准 HTTP/2 REST 协议，实现长连接复用、毫秒级响应、原生 JSON Schema 结构化约束，彻底根治本地进程级隐患。

### 2. 五大轻量单轮场景支撑
1.  **JSON 语法与结构修复 (`RepairJSON`)**：零进程开销，毫秒级快速纠错。
2.  **辩论终审法官 (`Judge Agent`)**：纯推理与规则裁决，输入上下文固定，原生结构化输出。
3.  **辩护人抗辩 (`Challenger Agent`)**：高并发反向推演保护性代码与宏隔离。
4.  **分片快速初筛 (`Tier 1 Fast`)**：50~100 路超高并发连接池，海量代码片段秒级初筛。
5.  **报告汇总与态势总结 (`Tier 3 Synthesis`)**：内存数据直传直出，消除临时文件 I/O。

### 3. 批判性审视与修订建议 ([查看文档 02](02-Thin-LLM-Engine设计批判性审视与场景配置方案.md))
*   **补充遗漏场景**：`askLLMIfSameFinding`（缺陷指纹语义匹配）纳入 Thin LLM 候选。
*   **纠正夸大**：并发提速上限取决于 LLM 服务端算力，非客户端 HTTP 连接池能力。
*   **审慎推进**：Judge/Challenger 切换 Thin LLM 需先完成 A/B 对照基准测试。
*   **可配置化路由**：新增 `ai.tool_backends` 配置层，支持场景级 Thin/Thick 灵活切换。

