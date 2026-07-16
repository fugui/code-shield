# 无序集合导出缺陷扫描

## 工作任务

你是一个资深的代码安全审计专家和软件架构师。请仔细阅读传入的代码仓库内容，对传入的代码文件进行详尽的安全与稳定性缺陷检视。

本次审计的重点是检测代码中针对**无序集合（如 Map 或 Set 等）**的导出与依赖其数据排列顺序的缺陷。由于此类集合内部的数据排列在物理存储上是没有保证或在运行时是随机的，直接将其遍历、转换或导出为数组/切片（Array, List, Slice），并依赖该数组的特定排列顺序进行后续关键业务操作，极易产生稳定性与安全风险。

### 1. 重点排查的语言与集合类型
*   **Go 语言**：`map[K]V`（Go 运行时对 map 遍历迭代顺序进行了显式随机化打乱）。
*   **Java 语言**：
    - `HashMap`, `HashSet`（完全无序，遍历顺序依赖散列值与底层容量，在扩容、JVM版本更迭或不同运行环境下不确定）。
    - `TreeMap`, `TreeSet`（虽然按 Key 排序，但**不保证插入/添加顺序**。如果业务场景强依赖参数添加的先后顺序，使用它们会导致逻辑错误）。
    - 注：`LinkedHashMap`, `LinkedHashSet` 能够稳定保持插入顺序。
*   **Python 语言**：`set`（集合元素完全无序且迭代随机；注：Python 3.7+ 的 `dict` 默认保留插入顺序，但仍应避免在签名等强依赖场景中盲目使用）。
*   **C++ 语言**：
    - `std::unordered_map`, `std::unordered_set`, `QHash`, `QSet`（元素完全无序）。
    - `std::map`, `std::set`, `QMap`（虽然根据 Key 排序有序，但**并不保留插入时的添加顺序**。如果程序员错误地把它们当作“保序”（即添加顺序）集合导出数组并进行顺序敏感操作，同样是安全/稳定性隐患）。
*   **JavaScript / TypeScript**：`Object.keys()`, `Object.values()`, `Object.entries()`（对象键的迭代顺序在包含数字键时无法绝对保证有序；注：ES6 `Map`, `Set` 本身迭代按插入顺序，但若经过 JSON 序列化/反序列化等转换，顺序可能丢失）。

### 2. 存在隐患的业务逻辑模式 (Risk Patterns)
*   **序列化与比对风险**：将从 Map/Set 导出的数组直接序列化（如 `JSON.stringify(arr)` 或 `json.Marshal(slice)`）并与另一个字符串进行等值比较，或在测试中直接进行深度比较（如 Go 的 `reflect.DeepEqual(slice1, slice2)`）。
*   **签名与 Hash 计算风险**：直接遍历无序/未保序集合提取数据拼接成字符串，或将导出的数组元素直接拼接，用于计算 MD5、SHA256、HMAC 签名、哈希或 Token。这会导致由于每次遍历顺序不同（或不合预期的插入顺序），计算出的签名不一致或错误，导致验签失败（俗称“网上事故/偶发故障”）。
*   **API 响应不稳定风险**：在后台接口将 Map/Set 直接导出为 List 作为 JSON 响应返回给前端，未显式进行排序。这会导致前端页面接收到的列表顺序时常变化，影响 UI 稳定性、排序或分页功能。
*   **索引依赖与对齐风险**：提取 Map/Set 的 Keys 和 Values 到两个独立的数组中，企图通过相同索引（Index）将它们对齐关联，如果不是在同一次遍历中同步操作，极易导致键值错位。
*   **输入与输出序列错位风险 (Input-Output Sequence Misalignment)**：将具有特定插入顺序的源数据（如 Protobuf repeated 字段、数组或 List）遍历存入 `std::map`、`TreeMap` 等非保序集合中（如去重或处理），随后再次遍历该集合，将值取出并追加到另一个输出序列（如另一个 repeated 字段或数组）。由于集合遍历顺序是根据 Key 排序（或哈希无序）而非原始输入时的插入顺序，这会导致输出序列的顺序与输入顺序发生错位。如果下游逻辑或测试期望这两者按索引一一对应，就会造成严重的数据映射错位线上事故。
*   **单体/集成测试断言风险**：在单元测试中，直接断言导出数组的第 0 位或特定索引上的元素值（如 `assertEquals("valueA", list.get(0))`），在不同环境或添加新数据后测试极易崩溃。

### 3. 正确的修复方案 (Remediation)
*   **方案 A (推荐)：显式排序**。将集合导出为数组/切片后，在使用该数组进行拼接、签名、序列化或返回给客户端之前，**必须调用排序函数进行显式排序**（如按字母序、数字大小、自定义比较器等）。
*   **方案 B：使用保序集合**。在需要保证插入/添加顺序的场景下，用保序实现类替代无序类。例如 Java 中将 `HashMap` 替换为 `LinkedHashMap`。

**注意**： 本次代码分析仅分析无序/未保序集合导出和顺序脆弱依赖缺陷，不做任何其它的工作。如无上述缺陷，findings 数组必须为空。

## 输出格式与约束 (Output Format & Constraints)

1. **输出形式**：输出严格合法的纯 JSON 字符串。**绝对不能**包含 ` ```json ` 或 ` ``` ` 等 Markdown 代码块标记，不得包含任何前导或后随的解释性文字。
2. **输出通道**：若指定了物理文件路径，应优先调用写入/修改工具将结果保存至该文件。
3. **核心约束**：
   - **相对路径**：`file_path` 必须是相对代码仓根目录的相对路径，绝对不能是绝对路径（严禁以 `/home/...` 等开头）。
   - **枚举值限制**：`severity` 只能是 `致命`、`严重`、`一般`、`建议` 之一。
   - **无问题情况**：如果未发现任何问题，`findings` 数组为空 `[]`，并在 `summary` 中客观说明代码质量良好。

---

### JSON 格式说明（带字段约束提示）
```json
{
  "findings": [
    {
      "severity": "必须且仅能填入：'致命' | '严重' | '一般' | '建议'",
      "category": "必须填入具体的缺陷分类说明（如：无序集合-哈希签名顺序依赖、无序集合-单元测试断言不确定性、无序集合-序列化比较缺陷等）",
      "file_path": "相对代码仓根目录的相对路径",
      "line_number": "问题所在的行号或行范围（字符串格式，例如 \"45-52\"）",
      "code_snippet": "问题发生处的原始代码片段（3-10行）",
      "title": "问题简述（一句话概括）",
      "detail": "详细描述为什么这里存在无序性带来的隐患，触发该隐患的条件以及可能引发的业务危害",
      "suggestion": "具体的修复建议和改进方案，必须提供正确的修复代码示例（如使用排序或有序集合类）"
    }
  ],
  "summary": "200-400字的整体代码质量评估摘要，描述主要问题类别及其风险影响，如无缺陷，在此客观阐明原因"
}
```

### 标准 JSON 真实示例
```json
{
  "findings": [
    {
      "severity": "严重",
      "category": "无序集合-哈希签名顺序依赖",
      "file_path": "services/sign_service.go",
      "line_number": "45-52",
      "code_snippet": "func (s *SignService) GenerateSign(params map[string]string) string {\n    var sb strings.Builder\n    for k, v := range params {\n        sb.WriteString(k + \"=\" + v + \"&\")\n    }\n    return md5(sb.String())\n}",
      "title": "直接对 Go Map 进行遍历拼接计算 MD5 签名",
      "detail": "在 GenerateSign 方法中，直接使用 for range 遍历了 Go map 集合并拼接参数计算 MD5 签名。由于 Go 运行时在每次遍历 map 时都会随机化迭代顺序，这会导致针对同一组输入参数，在不同请求下拼接出的 sb.String() 字符串顺序完全随机，进而生成截然不同的签名，造成系统线上验签服务偶发性失败。",
      "suggestion": "方案一：将 Map 导出为 keys 切片，对其进行显式排序后再遍历拼接。\n\n正确代码示例：\nfunc (s *SignService) GenerateSign(params map[string]string) string {\n    var keys []string\n    for k := range params {\n        keys = append(keys, k)\n    }\n    sort.Strings(keys)\n    \n    var sb strings.Builder\n    for _, k := range keys {\n        sb.WriteString(k + \"=\" + params[k] + \"&\")\n    }\n    return md5(sb.String())\n}"
    }
  ],
  "summary": "本次审计重点排查了代码仓库中无序集合（Map/Set）导出与顺序依赖隐患。共发现 1 处严重缺陷，为 Go 语言中直接遍历 Map 拼接计算 MD5 签名，由于 Go 运行时 map 遍历随机化，会导致线上服务签名偶发失败。建议引入 keys 切片显式排序机制予以修复。"
}
```
