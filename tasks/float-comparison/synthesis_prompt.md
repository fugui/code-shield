# 浮点数比较风险报告生成指令

你将收到一份 JSON 格式的 Python 及 C/C++ 浮点数比较缺陷隐患评估数据（基于 @analysis_prompt.md 提示词进行的分析）。请基于这些发现，生成一份指导开发者进行安全修复与数值精度治理的综合评估报告。

## 输入说明

输入为 JSON 格式的评估数据对象（Schema: `code-shield.tier4-synthesis-input.v1`），包含以下关键部分：
- `report`: 代码仓与检视基本元信息（`repo_name`, `task_type`, `head_commit` 等）
- `statistics`: 各严重级别缺陷统计数据（`fatal`, `critical`, `minor`, `suggestion` 等）
- `detailed_findings`: 重点浮点数精度缺陷详细列表，每个元素包含 `severity`, `category`, `file_path`, `line_number`, `code_snippet`, `title`, `detail`, `suggestion`
- `summary_findings`: 其余缺陷简要索引列表（包含 `file_path`, `line_number`, `title`, `severity`）

## 输出格式要求

请使用简体中文，以 Markdown 格式输出，内容包括"一、检视结果概要"、"二、浮点数比较缺陷清单"、"三、总结与预防建议"三部分。

### 一、检视结果概要
- 介绍本次浮点数比较风险审查的目的、采用的范围（覆盖 Python、C/C++ 源代码）和主要方法。
- 总结潜在浮点数直接比较风险隐患分布情况（致命、严重、一般、建议）。

> ⚠️ **概要部分的最后一行，格式约束（机器解析硬性约束，不得违反）**：
> - 必须严格使用 `致命：N，严重：N，一般：N，建议：N` 的格式，关键字和标点均不得更改
> - 冒号使用中文冒号 `：`，分隔符使用中文逗号 `，`
> - N 为非负整数，无问题填 `0`

### 二、浮点数比较缺陷清单
- 按照严重程度从高到低（致命 -> 严重 -> 一般 -> 建议）对风险列表进行整理排版。
- 每一个隐患条目应展示清晰的文件位置（包含代码行号）、风险分类、严重级别。
- 提供原汁原味的代码片段并突出显示风险行。
- 提供对应的风险分析和针对该语言（如 Python 中使用 `math.isclose` / `decimal.Decimal`，C/C++ 中使用 `std::fabs` Epsilon 容差 / 整数定点化）具体可靠的修复代码方案。

### 三、总结与预防建议
- 评估本代码仓整体的防浮点数精度风险质量水平。
- 给出系统性的浮点数比较缺陷预防建议：
  - **业务设计与架构**：在涉及财务、计费、库存等强一致性场景中，全面推行定点数或以最小单位表示的整数计量（如 Python 使用 `decimal.Decimal`，C/C++ 使用 `int64_t` 分/厘）。
  - **CI/CD 自动化拦截**：在 C/C++ 编译参数中配置开启 `-Wfloat-equal` 告警；在 Python CI 中集成静态检查规则（如 Flake8 / Pylint）自动拦截浮点数的 `==` / `!=` 操作。

