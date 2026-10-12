## 扫描聚焦领域 (Scan Focus Areas)

请对代码中使用的 cJSON 库相关操作进行深度审计，重点排查因生命周期失衡、所有权未移交或异常提前退出导致的内存泄漏问题，严格对照以下分类进行判定与归类：

### 1. 解析与构造生命周期 (Parse & Create Lifecycle)
* **cJSON 解析结果未释放 (Parse Result Leak)** [Category: `cJSON 解析结果未释放`]: 调用 `cJSON_Parse` / `cJSON_ParseWithOpts` 解析文本生成的 JSON 树对象，在正常返回、解析失败校验或提前退出分支中缺少配对的 `cJSON_Delete` 调用，且未移交给全局/长生命周期管理者。
* **cJSON 组装临时节点泄漏 (Detached Temporary Node Leak)** [Category: `cJSON 组装临时节点泄漏`]: 使用 `cJSON_Create...` 系列 API 分配的临时节点，在挂载至对象/数组失败、参数校验失败或提前返回时未被释放；注意：若节点已成功通过 `cJSON_AddItemToObject` / `cJSON_AddItemToArray` 挂载，所有权已转移给根节点，无需单独释放。

### 2. 序列化与拷贝管理 (Print & Duplicate Lifecycle)
* **cJSON 序列化输出未释放 (Print Output String Leak)** [Category: `cJSON 序列化输出未释放`]: 调用 `cJSON_Print` / `cJSON_PrintUnformatted` / `cJSON_PrintBuffered` 生成的堆内存字符串，在消费完成后未调用 `free`（或配对的 hook 释放函数），或在错误分支中丢失指针导致内存泄漏。
* **cJSON 深拷贝副本泄漏 (Duplicate Copy Leak)** [Category: `cJSON 深拷贝副本泄漏`]: 使用 `cJSON_Duplicate` 复制出的独立 JSON 树副本，在使用完毕后未调用 `cJSON_Delete` 回收，或在失败处理路径中丢失所有权。

### 3. 节点摘除与其它所有权问题 (Detach & Other Lifecycle)
* **cJSON 节点摘除后泄漏 (Detached Item Leak)** [Category: `cJSON 节点摘除后泄漏`]: 使用 `cJSON_DetachItemFromArray` 或 `cJSON_DetachItemFromObject` 等函数将节点从原树中摘除后，未将其保存至新所有者（未挂载到新树），也未显式调用 `cJSON_Delete` 释放该脱离节点。
* **其它 cJSON 生命周期泄漏 (Other cJSON Leak)** [Category: `其它 cJSON 生命周期泄漏`]: 其它涉及 cJSON 对象所有权转换失衡、结构体字段替换未清理旧值（如未妥善处理 `cJSON_SetValuestring` 或自定义 hook 内存管理）导致的资源泄漏隐患。

---

## 分析与判定原则

1. **拒绝虚报与所有权识别**：充分识别 cJSON 的所有权转移机制。若节点已被成功 `AddItem...` 挂载至父节点，其生命周期由根节点接管，严禁将其误报为泄漏。
2. **完整覆盖执行路径**：重点排查入参为 NULL、解析失败、JSON 字段不存在及各提前返回（Early Return）的异常分支，证明其存在确定性的资源丢失路径。
3. **代码锚定**：必须精准指出问题发生的行号，截取包含完整分配与遗漏退出上下文的 **3-10行** 原始代码片段。
4. **C/C++ 规范适配**：修复建议应符合标准 C/C++ 内存管理规范，对于 C++ 代码可优先推荐使用 RAII 智能指针或自定义 Deleter 进行自动回收。
