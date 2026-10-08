# ⚡ 03-dispatcher: 物理算力池与任务调度

本领域涵盖 Code-Shield 平台的物理算力池管理、平滑加权轮询（SWRR）、槽位租约并发流控与异构多模型分级调度。

---

## 📚 现行设计规范

*   **[01-异构模型流水线与DAG调度器设计.md](01-异构模型流水线与DAG调度器设计.md)**  
    异构调度拓扑：Tier 1（快模型初筛）、Tier 2（强推理模型深度辩论）、Tier 3（长上下文报告合成）多层级异步阶梯调度、动态 DAG 任务拓扑与异步背压流控机制。
*   **[02-算力池实时提示词与Agent控制台透视设计.md](02-算力池实时提示词与Agent控制台透视设计.md)**
    算力槽位调用级只读透视：Prompt 详情、Agent stdout/stderr、Native 请求生命周期、模型输出、有界 Console Ring 与 super_admin 安全边界。
*   **[03-Tier1Hunter空闲超时与分片自治重试设计.md](03-Tier1Hunter空闲超时与分片自治重试设计.md)**
    Tier 1 活动看门狗、执行保护预算、semantic bundle 超时二分与候选 ID 重编规则。
*   **[04-算力池与Tier调用成功率及耗时统计设计.md](04-算力池与Tier调用成功率及耗时统计设计.md)**
    算力池与 Tier 维度的模型调用质量统计：单次调用口径、队列等待与执行耗时拆分、成功率与 P95 指标、Tier x Pool 交叉视图与有界进程内聚合方案。
*   **[05-OpenCode失败会话续跑与输出守卫设计.md](05-OpenCode失败会话续跑与输出守卫设计.md)**
    OpenCode 会话级失败恢复：解析 sessionID、白名单化 idle / output-missing / total-timeout 续跑、短收尾 prompt、剩余预算治理与续跑成功率观测。
*   **[06-Tier分片失败恢复与重试设计.md](06-Tier分片失败恢复与重试设计.md)**
    分片级统一恢复状态机：错误分类、同 resource 有限重试、超时二分、OpenCode / Codex 续跑、显式候选降级、资源健康熔断与 attempt 历史观测。
