### 审计维度与判定标准

以单个显式创建的线程（如 `pthread_create`、`std::thread`、`CFThreadCreate`、`QThread`、`omp parallel`、`boost::asio::thread_pool`、`std::async` 等）为最小分析粒度，对每个显式创建线程的架构合理性、生命周期与安全性进行全面评估，严格对照以下受控维度进行判定与归类：

### 1. 线程创建的必要性与平台能力优先 (Necessity & Platform Capabilities)
* **无问题 (No Issue)** [Category: `无问题` (Code: `THREAD_NO_ISSUE`)]: 线程创建机制良好，生命周期清晰且受控，无安全缺陷，且无法或不需要使用平台能力进行重构。
* **频繁创建 (Frequent Creation)** [Category: `必要性-频繁创建` (Code: `THREAD_FREQUENT_CREATE`)]: 在频繁调用的热点函数、高频事件回调或循环体内显式频繁创建短生命周期线程，导致严重的系统调用和上下文切换开销。
* **未使用平台调度组 (Unused Platform Dispatch Group)** [Category: `必要性-未使用平台调度组` (Code: `THREAD_UNUSED_DISPATCH_GROUP`)]: 平台提供了调度组能力（一个调度组对应一个物理线程，支持串行接收处理任务），对于单次的、无状态的后台计算或异步任务，未采用平台调度组而是自行启动了裸线程。
* **未使用平台定时器 (Unused Platform Timer)** [Category: `必要性-未使用平台定时器` (Code: `THREAD_UNUSED_TIMER`)]: 采用在新线程中 `sleep` 循环的方式调度周期性业务，未采用平台提供的统一定时器机制，导致物理线程资源浪费。

### 2. 生命周期与退出管理 (Lifecycle & Resource Management)
* **join遗漏 (Missing Join or Detach)** [Category: `生命周期-join遗漏` (Code: `THREAD_MISSING_JOIN`)]: 创建了可汇合（Joinable）的线程，但在句柄销毁前缺少 `.join()` 或 `.detach()` 协调，导致生命周期与资源回收时机不明确。
* **未安全退出 (Unsafe Exit)** [Category: `生命周期-未安全退出` (Code: `THREAD_UNSAFE_EXIT`)]: 线程主循环缺少明确、安全的停止与退出协议（如缺少原子退出标志或条件变量唤醒），在宿主对象析构或进程终止时容易导致资源未释放或暴力终止。
* **析构崩溃 (Destructor Crash Risk)** [Category: `异常安全-析构崩溃` (Code: `THREAD_DESTRUCTOR_CRASH`)]: `std::thread` 对象在析构时若仍处于 joinable 状态（未调用 join 或 detach），在 C++ 标准中会直接触发 `std::terminate` 导致程序异常崩溃；或宿主析构提前完成而工作线程仍在访问成员。

### 3. 数据安全与并发同步 (Data Safety & Concurrency)
* **悬空引用 (Dangling Reference)** [Category: `数据安全-悬空引用` (Code: `THREAD_DANGLING_REFERENCE`)]: 传递给线程入口函数的参数中包含了局部栈变量或即将析构对象的指针/引用，当外层作用域退出后，子线程仍访问该地址导致未定义行为或内存崩溃。
* **数据竞争 (Data Race)** [Category: `同步安全-数据竞争` (Code: `THREAD_DATA_RACE`)]: 子线程内部访问的外部共享数据（如全局变量、宿主成员变量）缺乏互斥锁（`std::mutex`）、原子变量（`std::atomic`）或读写锁的同步保护。
* **其它隐患 (Other Concurrency Risks)** [Category: `其它` (Code: `THREAD_OTHER`)]: 线程入口未捕获向外抛出的异常导致进程终止，或其它不属于上述具体维度的线程并发隐患。

---

### 分析与判定原则

1. **平台能力优先原则**：
   - 优先引导业务使用平台提供的调度组与定时器能力，避免随意创建裸线程。
   - 调度组可解决临时线程创建销毁开销，且组内单线程机制能从源头减少数据竞争与死锁。
   - 若逻辑无安全缺陷但脱离了平台统一线程治理规范，建议引导重构为平台调度组或定时器。
2. **敏锐发现与风险推导**：全面排查线程创建点及异步生命周期路径。只要存在潜在的数据竞争、析构竞态或退出资源泄漏风险即应列为主候选，切勿过度保守隐瞒。
3. **代码锚定与上下文完整性**：必须精准指出显式创建线程与生命周期控制的行号，截取包含完整创建、参数传递及同步管理的 **5-25行** 原始代码片段。
4. **并发模型与平台规范推导约束**：严格依据目标工程所采用的并发编程规范（缺省以 C++17 为基准）与平台调度治理标准，客观推导线程生命周期与同步语义，不得虚构竞态或退出问题。
