## 扫描聚焦领域 (Scan Focus Areas)

请对 C/C++ 源码文件进行全面的内存与资源安全分析，重点排查未配对释放、所有权丢失、异常安全缺失以及指针悬挂隐患，严格对照以下八大维度进行判定与归类：

### 1. 堆内存与系统资源管理 (Heap & Resource Lifecycle)
* **基础内存泄漏 (Heap Allocation Leak)** [Category: `基础内存泄漏`]: `malloc`/`calloc`/`realloc` 未配对 `free`，或 `new`/`new[]` 未配对 `delete`/`delete[]`；在多分支或提前返回（Early Return）路径中丢失唯一指针所有权。
* **资源泄漏 (System Resource Leak)** [Category: `资源泄漏`]: 文件描述符（`fopen`/`open` 未配对 `fclose`/`close`）、Socket 套接字、管道（`popen`/`pclose`）或目录句柄（`opendir`/`closedir`）在打开使用后未成对关闭，未采用 RAII 托管。

### 2. 指针有效性与释放安全性 (Pointer Validity & Double Free)
* **野指针与悬空指针 (Dangling & Wild Pointer)** [Category: `野指针与悬空指针`]: 指针指向的内存释放后仍被继续访问；返回局部栈变量的指针或引用；使用未初始化的裸指针。
* **重复释放 (Double Free)** [Category: `重复释放`]: 同一块堆内存指针在同一执行链路或不同分支中被多次调用 `free` 或 `delete`；释放后未将裸指针置空导致二次析构。

### 3. 现代 C++ 与异常安全 (Modern C++ & Exception Safety)
* **异常安全 (Exception Safety)** [Category: `异常安全`]: 在 C++ 代码中，函数分配资源后发生异常抛出，因缺乏局部清理导致栈展开时跳过释放逻辑，破坏系统状态或泄露资源。
* **智能指针问题 (Smart Pointer Misuse)** [Category: `智能指针问题`]: `std::shared_ptr` 循环引用导致引用计数无法归零；混用智能指针内部裸指针（`.get()`）导致意外释放或多次接管所有权。
* **RAII原则违反 (RAII Violation)** [Category: `RAII原则违反`]: 可以在栈上由构造/析构自动管理生命周期的资源被手动裸管，或类成员资源在析构函数中未予释放。
* **其他常见问题 (Other Memory Risks)** [Category: `其他常见问题`]: `realloc` 失败未保存原指针导致原内存孤立泄漏、`strdup` 返回值未释放、`getline` 缓冲区未清理等其它内存/资源管理隐患。

---

## 分析与判定原则

1. **拒绝虚报与所有权识别**：充分识别所有权移交（如转移至外部容器、智能指针或调用方托管）及类析构函数中的自动清理逻辑；短生命周期离线脚本或静态单例不轻易判定为严重缺陷。
2. **全执行路径覆盖**：逐函数、逐代码块分析所有可能的执行分支，包括入参校验失败、异常抛出、系统调用返回负值等提前退出路径。
3. **代码锚定**：必须精准指出问题发生的行号，截取包含完整分配与遗漏退出上下文的 **3-10行** 原始代码片段。
4. **C++17 标准约束**：所有的重构建议必须兼容 C++17 标准，提倡采用 RAII（智能指针、ScopeGuard）进行自动化生命周期管理。
