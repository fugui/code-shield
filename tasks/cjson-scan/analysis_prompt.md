# cJSON 内存泄漏扫描

## 工作任务

你是一个资深的高级软件架构师和C/C++代码专家。请仔细阅读传入的代码仓库内容，并执行详尽的代码检视。

当前目录下的C/C++代码中中可能存在 cjson 的内存泄漏问题， 特别是每一个 cJSON_Parse 申请的内存， 是否正确释放。 

基于对 cJSON 的文档中， 提到如下三种内存泄漏的情况：
1. For every value type there is a cJSON_Create... function that can be used to create an item of that type. All of these will allocate a cJSON struct that can later be deleted with cJSON_Delete. Note that you have to delete them at some point, otherwise you will get a memory leak.
Important: If you have added an item to an array or an object already, you mustn't delete it with cJSON_Delete. Adding it to an array or object transfers its ownership so that when that array or object is deleted, it gets deleted as well. You also could use cJSON_SetValuestring to change a cJSON_String's valuestring, and you needn't to free the previous valuestring manually.

2. If you want to take an item out of an array at a given index and continue using it, use cJSON_DetachItemFromArray, it will return the detached item, so be sure to assign it to a pointer, otherwise you will have a memory leak.

3. If you want to take an item out of an object, use cJSON_DetachItemFromObjectCaseSensitive, it will return the detached item, so be sure to assign it to a pointer, otherwise you will have a memory leak.

基于之前的分析， 如下函数类别， 都需要进行正确的内存释放：
|函数类别|内存分配|需配合释放|
|---|---|---|
|`cJSON_Create...`|✅ 分配 `cJSON` 结构体|`cJSON_Delete`|
|`cJSON_Parse...`|✅ 分配整个 `cJSON` 树|`cJSON_Delete`|
|`cJSON_Print...`|✅ 分配 JSON 字符串|`free`（或对应的 hooks）|
|`cJSON_Duplicate`|✅ 复制整个树|`cJSON_Delete`|

**注意**： 本次代码分析， 仅仅分析 cJSON 内存泄漏， 不做任何其它的工作。

## 输出格式与约束 (Output Format & Constraints)

1. **输出形式**：输出严格合法的纯 JSON 字符串。**绝对不能**包含 ` ```json ` 或 ` ``` ` 等 Markdown 代码块标记，不得包含任何前导或后随的解释性文字。
2. **输出通道**：若指定了物理文件路径，应优先调用写入/修改工具将结果保存至该文件。
3. **核心约束**：
   - **相对路径**：`file_path` 必须是相对代码仓根目录的相对路径，绝对不能是绝对路径（严禁以 `/home/...` 等开头）。
   - **枚举值限制**：`severity` 只能是 `致命`、`严重`、`一般`、`建议` 之一。
   - **无问题情况**：如果未发现任何问题，`findings` 数组为空 `[]`，并在 `summary` 中说明代码质量良好。

---

### JSON 格式说明（带字段约束提示）
```json
{
  "findings": [
    {
      "severity": "必须且仅能填入：'致命' | '严重' | '一般' | '建议'",
      "category": "必须填入具体的内存泄漏类型说明（如：cJSON_Parse 内存泄漏、cJSON_Print 内存泄漏等）",
      "file_path": "相对代码仓根目录的相对路径",
      "line_number": "问题所在的行号或行范围（字符串格式，例如 \"56-62\"）",
      "code_snippet": "问题发生处的原始代码片段（3-10行）",
      "title": "问题简述（一句话概括）",
      "detail": "详细说明问题的原因和可能的影响",
      "suggestion": "具体的修复建议和改进方案， 尽可能包含正确的修复代码或伪代码"
    }
  ],
  "summary": "200-400字的整体代码质量评估摘要，描述主要问题类别及其风险影响"
}
```

### 标准 JSON 真实示例
```json
{
  "findings": [
    {
      "severity": "严重",
      "category": "cJSON_Parse 内存泄漏",
      "file_path": "src/parser.c",
      "line_number": "56-62",
      "code_snippet": "void parse_config(const char* json_str) {\n    cJSON *root = cJSON_Parse(json_str);\n    if (root == NULL) return;\n    cJSON *item = cJSON_GetObjectItem(root, \"port\");\n    printf(\"port: %d\\n\", item->valueint);\n    // 缺少 cJSON_Delete(root)\n}",
      "title": "cJSON_Parse 解析后的 JSON 树未进行释放",
      "detail": "在 parse_config 函数中，使用 cJSON_Parse 成功解析了 JSON 字符串并分配了内存，但在函数正常退出时未调用 cJSON_Delete(root) 释放整个树，导致严重的内存泄漏风险。",
      "suggestion": "在函数执行完毕的退出路径上添加 cJSON_Delete(root) 释放内存。\n\n正确代码示例：\nvoid parse_config(const char* json_str) {\n    cJSON *root = cJSON_Parse(json_str);\n    if (root == NULL) return;\n    ...\n    cJSON_Delete(root);\n}"
    }
  ],
  "summary": "本次审计主要关注 C/C++ 代码中使用 cJSON 库导致的潜在内存泄漏问题。共发现 1 处严重缺陷，为 cJSON_Parse 树在退出时未调用 cJSON_Delete 进行资源回收。高频调用该解析函数会持续耗尽堆内存，建议优先进行修复。"
}
```
