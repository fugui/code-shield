## 评估宗旨

本次审计以单个测试用例（Test Case）为最小分析单元，对每一个分配的测试用例出具客观、严谨的质量体检报告：
- **客观公正**：测试用例“合格 (pass)”与“存在缺陷 (defect)”是平等的正确评估结果，严禁为了输出缺陷而过度苛责正常合规的测试。
- **全量覆盖**：每个分配的 Primary Unit 均必须出具独立 assessment，无论合格还是有问题。

---

### 审计维度与判定标准

请针对分配的测试用例进行深度审计，严格对照以下维度进行判定与归类：

### 1. 断言有效性 (Assertion Validity)
* **空测试 (Empty Test)** [Category: `断言有效性-空测试` (Code: `UTE_EMPTY_TEST`), Severity: `严重`]: 仅调用了被测方法，但没有任何断言语句。通常纯粹为了刷覆盖率而写。
* **预期结果不完整 (Incomplete Expectation)** [Category: `断言有效性-预期结果不完整` (Code: `UTE_INCOMPLETE_EXPECTATION`), Severity: `一般`]: 仅对返回值的局部（如仅断言状态码非空）进行了校验，未对关键业务载荷、关键副作用或核心属性进行断言，遗漏关键预期结果。
* **永真断言 (Tautological Assertions)** [Category: `断言有效性-永真断言` (Code: `UTE_ALWAYS_TRUE`), Severity: `严重`]: 使用硬编码对比（如 `assertTrue(true)`, `EXPECT_EQ(1, 1)`），或对必然不可能为空的常量/全局变量做存在性断言，导致测试永远不会失败。
* **无效异常捕获 (Silent Catching)** [Category: `断言有效性-无效捕获` (Code: `UTE_INVALID_CAPTURE`), Severity: `致命`]: 在测试中使用 `try-catch` 捕获异常，但在 `catch` 块中漏掉了唤起测试失败的语句（如 `fail()` 或 `ADD_FAILURE()`），使得被测代码抛出异常时测试依然被判断为通过。
* **Mock 契约/行为校验遗漏 (Missing Mock Verification)** [Category: `断言有效性-Mock遗漏` (Code: `UTE_MISSING_MOCK`), Severity: `一般`]: 使用了 Mock 对象但仅做了行为插桩（Stub），缺少对关键交互与契约的最终行为验证（如缺少 Mockito 的 `verify(...)` 或 GTest 的 `EXPECT_CALL`），只确保了代码不崩溃，未验证内部协作逻辑。

### 2. 提前返回与跳过缺陷 (Early Exit & Skip Abuse)
* **非法提前退出 (Illegal Early Exit)** [Category: `提前返回-非法退出` (Code: `UTE_INVALID_EARLY_RETURN`), Severity: `严重`]: 测试用例内部存在 `if (condition) return;` 或逻辑分支跳出，导致后面的关键断言逻辑在某些环境下被静默跳过。
* **过宽的静默跳过 (Silent Skip Abuse)** [Category: `提前返回-静默跳过` (Code: `UTE_SILENT_SKIP`), Severity: `一般`]: 滥用 `assumeTrue` (JUnit) 或 `GTEST_SKIP()` (GTest) 机制，导致大批测试用例在自动化 CI 流水线中实际上根本没有被执行，却显示为通过状态。

### 3. 用例单一性与职责 (Test Singularity)
* **面条式混合断言 (Spaghetti Test)** [Category: `功能单一性-面条测试` (Code: `UTE_SPAGHETTI_TEST`), Severity: `建议`]: 一个测试用例内部包含多组不相关的 Arrange-Act-Assert（AAA）序列，混杂了过多独立的业务检查点，破坏了单一职责原则。
* **路径混合 (Path Mixture)** [Category: `功能单一性-路径混合` (Code: `UTE_MIXED_PATHS`), Severity: `一般`]: 在同一个测试用例里同时验证了正常成功逻辑和异常失败逻辑，应当拆分为独立的用例。

### 4. 测试目的与行为有效性 (Test Intent & Coverage Validity)
* **逻辑空跑 (Logic Dry-Run)** [Category: `其它` (Code: `UTE_OTHER`), Severity: `致命`]: 过度 Mock。连同“被测目标类/方法”本身的行为也被 Mock 掉，或者将所有底层依赖全部 Mock 且只返回空值，导致测试只在 Mock 框架内存中空跑，没有真实执行核心业务逻辑。
* **无意义的重复测试 (Redundant Verification)** [Category: `其它` (Code: `UTE_OTHER`), Severity: `建议`]: 多个用例使用完全相同的 Arrange 场景与断言，没有覆盖任何新边界或输入，属于冗余用例。

### 5. 合格测试判定基准 (Pass Criteria)
当测试用例满足以下条件且未触犯上述四大缺陷维度时，应判定为合格（`outcome: "pass"`）：
* **断言有效性**：显式断言、编译期静态断言（如 C++ 的 `static_assert` 模板特化与类型萃取约束）或封装的辅助校验函数（Helper Assertion）有效校验了核心业务结果或状态；
* **契约完整性**：对于安全兜底或验证不抛异常（No-throw / Crash-free Contract）的方法，正常执行完成即代表符合预期契约，无需多余显式断言；
* **存根契约覆盖**：Mock 或 Stub 的调用期望本身构成隐式逻辑检验；
* **意图独立聚焦**：单一用例职责明确，真实独立地执行了被测目标逻辑。

---

### 分析与判定原则

1. **客观双向判定**：全面审视断言完整性与 Mock 边界。确有无效断言、永真断言、空跑或静默跳过才判定为 defect；符合合格基准与白名单豁免的用例应果断判定为 pass。
2. **正向白名单豁免**：遇到静态断言（`static_assert`）、辅助函数内聚断言、无异常安全契约等合法模式时，严禁误报为“空测试”或“漏断言”。
3. **触发路径与失效机理明确**：判定为 defect 时，必须清晰阐述如果被测业务逻辑出现特定缺陷时，为何该测试用例无法正常感知并报错失败。
4. **代码锚定与行号规范**：必须在 detail 中精准指出问题发生的行号并引用关键语句；源码片段由系统自动提取，请勿在 issue 顶层注入未定义的字段。
5. **多语言单测生态适配**：遵循目标工程所采用的测试框架最佳实践（如 GTest/Catch2, JUnit/Mockito, Go testing/testify, pytest 等）。