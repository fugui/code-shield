# Code-Shield 项目文档中心 (Documentation Center)

本目录为 Code-Shield 系统的官方技术文档库，收录系统架构演进设计、模块专项规范与运维指南。

---

## 📚 目录结构与专题导读

```
docs/
├── README.md                                          # 本文档：文档中心首页索引
└── architecture-evolution/                            # 【专题】下一代 AI 引擎与异构调度架构演进方案
    ├── README.md                                      # 演进方案总览与导航
    ├── 01-fmt扫描实证比对与缺陷复盘报告.md              # 真实扫描比对、8大缺陷源码核实与 ASan 实测
    ├── 02-CodeShield-下一代AI引擎架构设计.md           # 语义分片、任务自适应路由、三方辩论与定级校准
    ├── 03-CodeShield-异构模型流水线与DAG调度器设计.md   # 异构模型阶梯调度、动态 DAG 任务拓扑与 SSOT 指纹记忆
    ├── 04-CodeShield-架构演进与落地实施路线图.md        # 三阶段落地规划、实施甘特图与基准测试 KPI 体系
    ├── 05-CodeShield-智能体协作与异构调度深度设计.md    # [阶段二专项] 猎手/辩护/法官三方协议、完整Prompt与Go实现
    ├── 06-CodeShield-多任务企业治理与历史记忆闭环深度设计.md # [阶段三专项] 全任务指纹算法、增量Diff状态机与10大规则库
    └── 07-CodeShield-数据模型与系统配置变更设计.md      # [工程配套专项] 数据库 DDL、GORM 模型扩展与 config.yaml 规范
```

---

## 🚀 核心架构演进专题导读

关于 Code-Shield 下一代 AI 引擎、智能体三方辩论、异构模型阶梯调度及多任务治理记忆闭环的完整方案，请直接阅读：
👉 **[下一代架构演进方案总览 (architecture-evolution/README.md)](architecture-evolution/README.md)**
