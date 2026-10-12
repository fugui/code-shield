### 1. 多线程并发问题 (Multi-Threading & Concurrency)
* **数据竞争 (Data Race)** [Category: `多线程并发问题-数据竞争` (Code: `THREAD_DATA_RACE`)]: 多个线程同时读写共享变量/数据结构，缺乏互斥锁 (`std::mutex`, `pthread_mutex_t`)、自旋锁或读写锁保护。
* **锁同步不当 (Lock Misuse)** [Category: `多线程并发问题-锁同步不当` (Code: `THREAD_LOCK_SYNC`)]: 临界区未受保护，或使用了错误的同步机制；误用 `volatile` 代替原子变量 (`std::atomic`)。
* **死锁风险 (Deadlock)** [Category: `多线程并发问题-死锁风险` (Code: `THREAD_DEADLOCK`)]: 多锁并发加锁环境中，加锁顺序不一致导致循环等待的风险。

### 2. 内存管理缺陷 (Memory Management)
* **空指针解引用 (Null Pointer Dereference)** [Category: `内存管理问题-空指针解引用` (Code: `MEM_NULL_DEREF`)]: 指针使用前未校验 `NULL`/`nullptr`；直接解引用空值导致段错误 (Segmentation Fault)。
* **越界访问/缓冲区溢出 (Buffer Overflow)** [Category: `内存管理问题-越界访问/缓冲区溢出` (Code: `MEM_BUFFER_OVERFLOW`)]: 数组下标越界、指针移动超出合法边界、使用不安全拷贝函数（如 `strcpy`, `sprintf`, `gets`, `strcat` 等）未限制拷贝长度导致堆/栈溢出。
* **双重释放 (Double Free)** [Category: `内存管理问题-双重释放` (Code: `MEM_DOUBLE_FREE`)]: 同一指针在同一执行路径上被释放多次。
* **释放后使用 (Use-After-Free)** [Category: `内存管理问题-释放后使用` (Code: `MEM_USE_AFTER_FREE`)]: 使用已销毁或已释放对象的指针/引用。
* **内存泄漏 (OOM Risk)** [Category: `内存管理问题-内存泄漏(OOM风险)` (Code: `MEM_LEAK_OOM`)]: 长生命周期对象或高频循环中不断分配资源但未对应释放，导致内存耗尽，触发系统 OOM Killer。

### 3. 指针与生命周期管理 (Pointer & Lifecycle)
* **悬挂指针与引用 (Dangling Pointer & Reference)** [Category: `生命周期管理问题-悬挂指针与引用` (Code: `LIFECYCLE_DANGLING_REF`)]: 函数返回局部变量/临时对象的引用或指针；异步调用中传递的对象在被访问前已被提前销毁。
* **回调对象销毁竞争 (Callback Destroy Race)** [Category: `生命周期管理问题-回调对象销毁竞争` (Code: `LIFECYCLE_CALLBACK_DESTROY_RACE`)]: 在观察者模式或异步回调触发时，目标回调承载对象已被销毁，且未检测回调指针的有效性。

### 4. 资源初始化与终止时序 (Initialization & Termination)
* **未初始化变量 (Uninitialized Variable)** [Category: `时序与初始化问题-未初始化变量` (Code: `INIT_UNINITIALIZED`)]: 类成员变量、局部指针/变量或全局结构体未初始化即被读取或参与核心逻辑判断。
* **释放与访问竞态 (Release-Access Race)** [Category: `时序与初始化问题-释放与访问竞态` (Code: `RACE_RELEASE_ACCESS`)]: 线程 A 正在销毁/终止某个单例、全局资源或特定对象周期，而线程 B 仍在并发尝试访问该资源。

### 5. 运行时异常与栈溢出风险 (Runtime Exception & Stack Overflow)
* **除零错误 (Divide-by-Zero)** [Category: `运行时异常-除零错误` (Code: `RUNTIME_DIV_ZERO`)]: 除法或取模操作（`/` 或 `%`）的右操作数（除数）未进行非零校验，可能在特定运行情况下为零。
* **栈溢出 (Stack Overflow)** [Category: `运行时异常-栈溢出` (Code: `RUNTIME_STACK_OVERFLOW`)]: 缺乏边界终止条件或可能导致无限递归的函数调用链路；在栈上直接分配过大的局部数组或结构体超出线程栈深度限制。
* **未捕获异常/致命退出 (Uncaught Exception & Fatal Abort)** [Category: `运行时异常-未捕获异常/致命退出` (Code: `RUNTIME_UNCAUGHT_EXCEPTION`)]: 在 `noexcept` 函数或析构函数中抛出异常导致进程强制终止；标准库或代码抛出的异常未被捕获；直接调用 `abort()` 或 `terminate()` 触发致命退出的情形。
* **信号处理不当 (Unsafe Signal Handling)** [Category: `运行时异常-信号处理不当` (Code: `RUNTIME_SIGNAL_MISUSE`)]: 在信号处理函数中调用了非异步信号安全（Async-Signal-Safe）的函数（例如 `printf`, `malloc`, `free` 等）。

### 6. 特定框架与其它隐患
* **Qt跨线程UI操作 (Qt Cross-Thread UI)** [Category: `第三方框架限制-Qt跨线程UI操作` (Code: `FRAMEWORK_QT_CROSS_THREAD_UI`)]: 在非 GUI 主线程中直接操作、修改 UI 控件或对象。
* **其它崩溃隐患 (Other Crash Risks)** [Category: `其它问题-其它崩溃隐患` (Code: `GENERIC_CRASH_RISK`)]: 违反特定组件线程安全规范或生命周期托管约束可能导致 Coredump 的其它问题。

---

### 分析与判定原则

1. **敏锐发现与路径推导**：重点挖掘存在并发冲突、时序异常或生命周期失控的崩溃隐患。只要存在可推导的潜在危险执行路径即应列为主候选，详细阐述触发前提，切勿因跨函数调用复杂或缺乏全局上下文而过度保守隐瞒。
2. **结构化因果推导与指控**：必须结合数据流、调用链与时序逻辑，在 `trigger_condition` 中明确三要素：【前置假设】（外部输入、数据状态或并发边界）、【触发路径】（调用时序与传播过程）、【崩溃机理】（触发段错误/死锁/越界的物理机制与致命后果）。切勿忽略代码中已有的防御判断与前置断言。
3. **代码锚定与上下文完整性**：必须精准指出问题发生的行号，截取包含完整上下文逻辑（包括前置检查、锁临界区或核心调用）的 **5-25行** 原始代码片段。
4. **目标语言标准规范约束**：所有的风险推导与语言行为判定都必须符合目标工程所采用的 C++ 标准规范（缺省以 C++17 为基准，严禁臆测高版本特有语义）。
