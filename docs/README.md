# Code-Shield 项目文档中心 (Documentation Center)

本目录为 Code-Shield 系统的官方技术文档库，收录系统架构演进设计、模块专项规范与运维指南。

---

## 📚 目录结构与专题导读

```
docs/
├── README.md                                          # 本文档：文档中心首页索引
├── CodeShield-智能扫描引擎与任务配置使用手册.md         # 📖【用户操作指南】5大执行模式选型、JSON配置模版与FAQ
├── agentic-scanner-architecture/                      # 🛡️【专题一】基于多 Agent 协同与异构调度的下一代扫描架构
│   ├── README.md                                      # 架构方案总览与导航
│   ├── 与AI结对架构设计实战指南-CodeShield案例复盘.md    # 🌟【培训复盘】如何与 AI 深度结对完成系统级架构设计的实战方法论
│   ├── 01-fmt扫描实证比对与缺陷复盘报告.md              # 真实扫描比对、8大缺陷源码核实与 ASan 实测
│   ├── 02-CodeShield-下一代AI引擎架构设计.md           # 语义分片、任务自适应路由、三方辩论与定级校准
│   ├── 03-CodeShield-异构模型流水线与DAG调度器设计.md   # 异构模型阶梯调度、动态 DAG 任务拓扑与 SSOT 指纹记忆
│   ├── 04-CodeShield-架构演进与落地实施路线图.md        # 三阶段落地规划、实施甘特图与基准测试 KPI 体系
│   ├── 05-CodeShield-智能体协作与异构调度深度设计.md    # [阶段二专项] 猎手/辩护/法官三方协议、完整Prompt与Go实现
│   ├── 06-CodeShield-多任务企业治理与历史记忆闭环深度设计.md # [阶段三专项] 全任务指纹算法、增量Diff状态机与10大规则库
│   └── 07-CodeShield-数据模型与系统配置变更设计.md      # [工程配套专项] 数据库 DDL、GORM 模型扩展与 config.yaml 规范
├── multi-tenancy-architecture/                        # 🏢【专题二】多租户系统架构与资源动态调度设计
│   ├── README.md                                      # 多租户方案总览与设计导航
│   ├── 01-多租户隔离方案评估与分库架构选型.md
│   ├── 02-GORM动态分库路由与连接池管理深度设计.md
│   ├── 03-租户模型服务资源隔离与动态调度设计.md
│   ├── 04-代码仓凭据保险库与工作区安全沙箱.md
│   ├── 05-租户自定义任务类型与动态规则扩展设计.md
│   ├── 06-多租户系统迁移运维与实施路线图.md
│   └── 07-租户分层配置与动态热加载深度设计.md
├── native-llm-architecture/                           # ⚡【专题三】原生 LLM 轻量执行引擎与混合调用架构设计
│   ├── README.md                                      # 原生 LLM 引擎方案总览与导航
│   └── 01-CodeShield-原生LLM轻量执行引擎与混合调用架构设计.md # 🌟【核心设计】原生轻量 LLM 引擎、6大单轮场景与动静分离混合调用
├── deterministic-fingerprint-architecture/            # 🧬【专题四】确定性源码指纹与抗抖动增量生命周期架构设计
│   ├── README.md                                      # 确定性指纹架构方案总览与导航
│   └── 01-确定性源码指纹与抗抖动增量生命周期架构设计.md # 🌟【核心设计】物理源码感知器、三级指纹分层、受控Taxonomy归一化与防假修复状态机
└── services-modular-architecture/                     # 🏗️【专题五】Services 核心服务层领域模块化与解耦重构设计
    ├── README.md                                      # 重构方案总览、量化目标与导航
    ├── 01-Services核心服务层代码现状全景与技术债务诊断报告.md # 46个平铺文件现状、1.6万行代码分布与6大技术债务深度剖析
    ├── 02-Services领域驱动子包化重构设计与契约规范.md  # 7大自治领域子包划分、EngineContext解耦契约与Facade门面设计
    ├── 03-分阶段平滑演进路线图与安全落地实施方案.md    # 4阶段渐进演进计划、甘特图、零破坏验证指令与回滚防线
    └── 04-扫描引擎与执行流水线职责边界与协同模型深度设计.md # 引擎与流水线正交解耦、6大执行阶段、断点续跑归位与协同协议
```

---

## 🚀 核心文档直达

1. 📖 **[Code-Shield 智能扫描引擎与任务配置使用手册](CodeShield-智能扫描引擎与任务配置使用手册.md)**
   涵盖 5 大执行模式（`debate_full`、`debate_selective`、`chunked_fast`、`chunked`、`single`）选型决策、`engine_config` 增量配置模版库、Tier 模型分层调度与研发反馈沉淀指南。
2. 🛡️ **[下一代 Agent 扫描架构设计总览 (agentic-scanner-architecture/README.md)](agentic-scanner-architecture/README.md)**
   收录完整的系统架构方案演进、学术级设计规范（01~07）与 AI 结对工程复盘。
3. 🏢 **[多租户系统架构设计方案总览 (multi-tenancy-architecture/README.md)](multi-tenancy-architecture/README.md)**
   收录分库多租户物理隔离、连接池动态路由、数据凭据沙箱与实施路线图（01~07）。
4. ⚡ **[原生 LLM 轻量执行引擎与混合调用架构 (native-llm-architecture/README.md)](native-llm-architecture/README.md)**
   收录原生 HTTP REST API 轻量引擎设计、5 大单轮任务场景分析与“探索型 Agent + 原生 LLM”动静分离混合调用体系。
5. 🧬 **[确定性源码指纹与抗抖动增量生命周期架构 (deterministic-fingerprint-architecture/README.md)](deterministic-fingerprint-architecture/README.md)**
   收录物理源码真实源提取（`SourceEnricher`）、三级指纹分层体系、受控 Taxonomy 归一化与代码变更守卫防假修复状态机。
6. 🏗️ **[Services 核心服务层领域模块化与解耦重构设计 (services-modular-architecture/README.md)](services-modular-architecture/README.md)**
   收录 Services 核心服务层 46 个平铺文件量化体检画像、6 大核心技术债务诊断、7 大领域子包规范、EngineContext 解耦契约、扫描引擎与执行流水线职责边界与协同模型（01~04）。

