# Code-Shield 智能体协同与异构调度扫描架构设计文档库

本专题收录了针对 Code-Shield 平台的深度实证分析、下一代 AI 扫描引擎（AI Engine）演进设计、异构模型调度器（Dispatcher & Scheduler）设计及工程落地路线图。

---

## 📚 文档目录索引与导读

```
agentic-scanner-architecture/
├── README.md                                          # 本文档：专题导航与核心摘要
├── 与AI结对架构设计实战指南-CodeShield案例复盘.md          # 🌟【培训复盘】如何与 AI 深度结对完成系统级架构设计的实战方法论
├── 01-fmt扫描实证比对与缺陷复盘报告.md                  # 两次扫描报告深度比对、8大缺陷源码核查与 ASan 实测
├── 02-CodeShield-下一代AI引擎架构设计.md               # 语义分片、任务自适应路由、多 Agent 辩论与定级校准
├── 03-CodeShield-异构模型流水线与DAG调度器设计.md       # 异构模型阶梯调度、动态 DAG 任务拓扑与 SSOT 指纹记忆
├── 04-CodeShield-架构演进与落地实施路线图.md            # 三阶段工程落地规划、甘特图、基准测试指标体系 (KPI)
├── 05-CodeShield-智能体协作与异构调度深度设计.md        # [阶段二专项] 猎手/辩护/法官三方协议、完整Prompt与Go实现
├── 06-CodeShield-多任务企业治理与历史记忆闭环深度设计.md # [阶段三专项] 全任务指纹算法、增量Diff状态机、人机反馈与10大规则库
├── 07-CodeShield-数据模型与系统配置变更设计.md          # [工程配套专项] 数据库 DDL、GORM 模型扩展、config.yaml 规范与兼容方案
└── 08-CodeShield-原生LLM轻量执行引擎与混合调用架构设计.md # [执行引擎专项] 原生轻量 LLM、5大单轮场景与动静分离混合调用
```

---

## 🎯 核心成果与设计摘要

### 1. 真实扫描复盘与 8 大缺陷核实 ([查看文档 01](01-fmt扫描实证比对与缺陷复盘报告.md))
*   **样本来源**：`report-147` 与 `report-150`（针对 `scylladb/fmt` 仓的 Coredump 风险扫描）。
*   **核实结论**：两份报告汇总的 8 个缺陷经 GCC 13.3 + ASan/UBSan 实测**全部为 100% 真实缺陷（零误报）**：
    *   **P0 严重级**：Grisu2 浮点栈缓冲越界写（ASan 捕获）、`parse_arg_id` 先自增后解引用堆越界读（ASan 捕获）。
    *   **P1 高危级**：`buffered_file::fileno()` 空指针段错误、`string_view` 传入 `nullptr` 触发 `strlen` 段错误、`vprint` 空指针透传。
    *   **P2 中危级**：`width` / `precision` 无上限导致的 2GB+ 内存申请（OOM/bad_alloc 风险）。
*   **核心痛点提炼**：单模型扫描存在明显的盲区互补性、分片上下文割裂、定级主观漂移及缺乏动态实证。

### 2. 下一代 AI 分析引擎 (Next-Gen AI Engine) ([查看文档 02](02-CodeShield-下一代AI引擎架构设计.md))
*   **语义感知分片 (Semantic Chunking)**：跨目录同名投影映射（`include/` 与 `src/` 配对），提取核心公用头文件声明摘要与构建宏，消除跨文件盲区。
*   **多 Agent 交叉辩论 (Multi-Agent Debate)**：
    *   `Hunter Agent`（猎手）：高召回率初筛；
    *   `Challenger Agent`（辩护人）：反向推演保护性代码与标准契约，剔除假阳性；
    *   `Judge Agent`（仲裁官）：综合事实裁决。
*   **全任务领域规则与记忆闭环 (Domain Rules & Memory Loop)**：沉淀 10 类任务确定性白名单与规则库，为人机协同记忆提供权威依据。
*   **确定性严重度校准矩阵 (Severity Matrix)**：基于 CWE、宏开关与破坏属性确定性计算等级，杜绝主观评分漂移。

### 3. 异构模型多层级调度器 (Tiered Dispatcher & DAG) ([查看文档 03](03-CodeShield-异构模型流水线与DAG调度器设计.md))
*   **异构分级调度与异步背压 (Tiered Dispatching & Backpressure)**：
    *   *Tier 1 快模型*（如 DeepSeek-V3 / Qwen-Flash）：承担 80% 代码初筛，高并发、低成本；
    *   *Tier 2 强推理模型*（如 Claude 3.5 Sonnet / o3-mini）：专注 20% 可疑点的深度辩论与状态机推演；
    *   *Tier 3 综合模型*：长上下文排版与多格式报告生成；
    *   *异步阶段管道与背压*：防止跨 Tier 资源争抢与调度死锁。
*   **动态 DAG 任务执行拓扑**：支持分片级重试、节点熔断与部分成功降级合成。
*   **跨任务抗漂移缺陷指纹库 (Memory Graph)**：基于核心触发行与 AST 规范化哈希追踪存量、增量与已修复缺陷，内置扫描覆盖域校验守卫。

### 4. 落地实施路线图 ([查看文档 04](04-CodeShield-架构演进与落地实施路线图.md))
*   **Phase 1 (基础加固)**：语义分片与投影配对 + 严重度校准中间件；
*   **Phase 2 (智能协作)**：三角色辩论流水线 + 异构模型调度器与背压流控；
*   **Phase 3 (企业治理与历史记忆)**：全任务通用缺陷指纹库 + 扫描范围守卫增量 Diff + 人机反馈知识沉淀；
*   **预期成效**：缺陷召回率提升至 $\ge 90\%$，误报率降低至 $\le 5\%$，单仓分析 Token 成本降低 $40\% \sim 55\%$。

### 5. 原生 LLM 轻量执行引擎与混合调用架构 ([查看文档 08](08-CodeShield-原生LLM轻量执行引擎与混合调用架构设计.md))
*   **破除重型 Agent 进程瓶颈**：针对单轮确定性任务（JSON 修复、裁判终审、辩护抗辩、分片初筛、报告汇总、误报规则提取），引入基于标准 HTTP REST 的 `NativeInvoker`。
*   **动静分离架构 (Hybrid Execution)**：
    *   *Thick Agent（重型/探索型）*：专注全局 Hunter 复杂跨文件污点深挖与自主工具调用；
    *   *Native LLM（轻量/推理型）*：专注高并发初筛、辩论裁判与结构化输出（原生 JSON Schema 保证）。
*   **性能飞跃**：消除 2~10s 进程启动冷开销，初筛吞吐量提升 10 倍，单仓全扫描总耗时预期缩减 70%。
