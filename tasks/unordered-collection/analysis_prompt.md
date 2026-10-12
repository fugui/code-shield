## 扫描聚焦领域 (Scan Focus Areas)

在物理存储或语言运行时中，无序/未保序集合（如 Go 的 `map`，Java 的 `HashMap`/`HashSet`/`TreeMap`/`TreeSet`，Python 的 `set`，C++ 的 `std::unordered_map`/`std::unordered_set`/`std::map`/`std::set`，JS 的对象键集合等）在遍历、迭代或导出为切片/数组时，并不保证插入顺序，甚至在运行期间显式随机化打乱。请严格对照以下业务后果维度进行判定与归类：

### 1. 安全签名与跨系统通信 (Signatures & External Contracts)
* **签名与摘要顺序不稳定 (Unstable Hash/Signature Order)** [Category: `签名与摘要顺序不稳定`]: 直接遍历无序集合拼接参数计算 MD5、SHA256、HMAC 签名、摘要、Token 或哈希，由于迭代顺序不稳定导致针对相同输入产生不同签名，引发验签失败事故。
* **跨系统契约顺序不稳定 (Unstable Cross-System Contract Order)** [Category: `跨系统契约顺序不稳定`]: 持久化存储、RPC 请求、外部 API 报文或复杂结构体深度比较（如 `reflect.DeepEqual`）依赖无序集合导出后的固定顺序，导致跨环境或跨实例结果漂移。

### 2. 映射对齐与序列流转 (Mapping Alignment & Sequence Flow)
* **键值索引错位 (Key-Value Index Misalignment)** [Category: `键值索引错位`]: 分别单独遍历无序集合导出 Keys 列表与 Values 列表，企图通过数组下标索引（Index）在两组数据间对齐关联，由于两次遍历顺序无法保证绝对一致导致严重的键值错位。
* **有序输入被重排 (Ordered Input Reordered)** [Category: `有序输入被重排`]: 具有特定插入顺序的原始输入（如 Protobuf repeated 字段或有序列表）存入无序或非保序集合去重后，再导出为输出序列，导致输出序列与原始输入的顺序对应关系被破坏，影响下游消费。

### 3. 接口展示与测试验证 (API Stability & Testing)
* **接口列表顺序不稳定 (Unstable API Response Order)** [Category: `接口列表顺序不稳定`]: 后端接口将 Map/Set 直接导出为 JSON 数组返回给前端且未显式排序，导致前端页面、列表分页或截断消费在不同请求下顺序频繁跳变。
* **测试断言顺序依赖 (Test Assertion Order Dependency)** [Category: `测试断言顺序依赖`]: 单元测试或集成测试直接断言无序集合导出数组的特定索引元素（如 `assertEquals("val", list.get(0))`），导致测试在不同构建环境或并发执行时偶发失败 (Flaky Test)。
* **日志与展示顺序不稳定 (Unstable Log & Display Order)** [Category: `日志与展示顺序不稳定`]: 日志记录、调试输出或本地界面展示的顺序由无序集合决定，虽然不改变核心业务状态，但导致排查对比体验不稳定。
* **其它顺序依赖风险 (Other Order Dependency Risks)** [Category: `其它顺序依赖风险`]: 确认无序遍历行为会对下游确定性产生负面影响，但不属于上述具体分类的其它隐患。

---

## 分析与判定原则

1. **拒绝虚报与有序判定**：
   - 若下游消费逻辑为纯无序统计（如求和、成员存在性查找），对遍历顺序完全不敏感，严禁虚报。
   - 若集合导出后，后续逻辑在使用前已显式调用强制排序（如 `sort.Slice`、`Collections.sort`），属于合格实现。
   - 若容器本身为严格保序实现（如 `LinkedHashMap`），不属于无序风险。
2. **触发路径真实可信**：必须清晰说明该集合在当前语言运行时（如 Go map 迭代随机化）下的行为表现，以及下游如何由于顺序敏感产生业务逻辑错误。
3. **代码锚定**：必须精准指出问题发生的行号，截取包含完整集合遍历与顺序依赖上下文的 **3-10行** 原始代码片段。
4. **推荐修复方案**：
   - 方案一（推荐）：将无序集合导出为列表后，调用语言标准库进行显式排序（如按 Key 字典序排序后再拼接/返回）。
   - 方案二：在需要保留插入顺序的场景下，改用保序集合（如 Java 的 `LinkedHashMap`）。
