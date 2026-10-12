### 扫描聚焦领域 (Scan Focus Areas)

请对源码文件进行深度的系统性架构与全量安全质量评估，深入挖掘设计缺陷、并发竞态与隐藏风险，严格对照以下五大领域进行判定与归类：

### 1. 逻辑与正确性 (Logic & Correctness)
* **并发安全 (Concurrency Safety)** [Category: `逻辑与正确性-并发安全` (Code: `DEEP_CONCURRENCY`)]: 跨线程共享数据访问缺乏同步保护、锁粒度不当、锁顺序不一致引发死锁或偶发数据竞争。
* **边界处理 (Boundary Defense)** [Category: `逻辑与正确性-边界处理` (Code: `DEEP_BOUNDARY`)]: 空指针/空值（Null/Nil）未校验、数组切片越界访问、数值溢出或特殊输入边界处理缺失。
* **逻辑漏洞 (Logic Flaws)** [Category: `逻辑与正确性-逻辑漏洞` (Code: `DEEP_LOGIC`)]: 业务流程分支倒置、状态机死锁或不可达状态、核心计算或业务不变量被破坏。
* **异常控制 (Exception & Error Control)** [Category: `逻辑与正确性-异常控制` (Code: `DEEP_EXCEPTION`)]: 异常处理不当导致状态破坏、静默吞掉错误导致不可诊断、或错误处理路径中破坏了系统不变量。

### 2. 性能与运行效率 (Performance & Resources)
* **内存泄漏 (Memory Leak)** [Category: `性能与运行效率-内存泄漏` (Code: `DEEP_MEM_LEAK`)]: 长期运行或高频路径中分配的堆内存未被回收，缺少唯一所有者导致渐进式内存耗尽。
* **资源未释放 (Resource Leak)** [Category: `性能与运行效率-资源未释放` (Code: `DEEP_RESOURCE_LEAK`)]: 文件描述符、套接字（Socket）、数据库连接或系统句柄未成对关闭，未采用 RAII/defer 保证异常安全释放。
* **CPU与I/O瓶颈 (CPU & I/O Bottlenecks)** [Category: `性能与运行效率-CPU与I/O瓶颈` (Code: `DEEP_CPU_IO`)]: 高频热点循环中包含低效阻塞 I/O、无意义的重复深拷贝或复杂度过高的算法实现。

### 3. 安全风险 (Security Risks)
* **内存破坏 (Memory Corruption)** [Category: `安全风险-内存破坏` (Code: `DEEP_MEM_CORRUPTION`)]: 缓冲区溢出、格式化字符串漏洞、不安全指针运算或危险内存拷贝函数导致内存损坏风险。
* **敏感硬编码 (Hardcoded Secrets)** [Category: `安全风险-敏感硬编码` (Code: `DEEP_HARDCODED_SECRET`)]: 代码中硬编码生产私钥、Token、数据库密码、内部敏感鉴权凭据或绝对路径。
* **输入注入 (Input Injection)** [Category: `安全风险-输入注入` (Code: `DEEP_INPUT_INJECTION`)]: 外部非受信输入未经参数化或转义即拼接至系统命令、数据库查询或解释器执行环境中。

### 4. 可维护性与代码规范 (Maintainability & Code Smell)
* **魔鬼数字 (Magic Numbers)** [Category: `可维护性-魔鬼数字` (Code: `DEEP_MAGIC_NUMBER`)]: 关键业务判断或循环边界中反复出现未具名、无释义的常量字面量，影响代码可读性与变更安全。
* **圈复杂度过高 (High Complexity)** [Category: `可维护性-圈复杂度过高` (Code: `DEEP_COMPLEXITY`)]: 单个函数控制流分支过多（圈复杂度 > 15）或嵌套过深（> 4层），导致维护与测试成本过高。
* **冗余代码 (Dead Code)** [Category: `可维护性-冗余代码` (Code: `DEEP_DEAD_CODE`)]: 存在确定不可达的死分支、大段遗留废弃逻辑或重复雷同实现。

### 5. 架构与设计治理 (Architecture & Design Governance)
* **高耦合 (High Coupling)** [Category: `架构与设计-高耦合` (Code: `DEEP_COUPLING`)]: 模块间存在循环依赖、越级穿透私有实现或不合理的强耦合设计，破坏封装性。
* **职责违背 (Single Responsibility Violation)** [Category: `架构与设计-职责违背` (Code: `DEEP_RESPONSIBILITY`)]: 单个类或组件承担过多不相关职责，混合了基础设施、业务逻辑与展示层，破坏单一职责原则。
* **三方依赖 (Third-Party Misuse)** [Category: `架构与设计-三方依赖` (Code: `DEEP_THIRD_PARTY`)]: 误用或废弃的三方库调用、违背三方组件契约导致不可预测的稳定性风险。
* **其它架构风险 (Other Architectural Risks)** [Category: `其它问题-其它架构风险` (Code: `DEEP_OTHER`)]: 确凿可信但不属于上述细分维度的其它深层架构与稳定性风险。

---

### 分析与判定原则

1. **敏锐发现深层隐患**：重点挖掘高价值架构、并发、资源泄漏与边界破坏隐患。只要存在可推导的潜在危险执行路径即应列为主候选，避免被细微琐碎的风格问题分散精力。
2. **触发路径真实可信**：必须结合数据流、调用链与时序逻辑，指出缺陷触发的具体前置条件，切勿忽略外层框架或既有防护。
3. **代码锚定与上下文完整性**：必须精准指出问题发生的行号，截取包含完整上下文逻辑的 **5-25行** 原始代码片段。
4. **语言行为与架构规范推导约束**：严格依据目标工程采用的语言标准与架构规范（C++ 缺省以 C++17 为基准）分析边界安全性与生命周期，准确识别既有框架与基础设施防御逻辑。
