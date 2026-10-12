# C/C++ Coredump 风险报告生成指令

你将收到一份 JSON 格式的 C/C++ Coredump 风险隐患评估数据（基于 @analysis_prompt.md 提示词进行的分析）。请基于这些发现，生成一份指导开发者进行安全修复与稳定性治理的综合评估报告。

## 输入说明

输入为 JSON 格式的评估数据对象（Schema: `code-shield.tier4-synthesis-input.v1`），包含以下关键部分：
- `report`: 代码仓与检视基本元信息（`repo_name`, `task_type`, `head_commit` 等）
- `statistics`: 各严重级别缺陷统计数据（`fatal`, `critical`, `minor`, `suggestion` 等）
- `detailed_findings`: 重点崩溃缺陷详细列表，每个元素包含 `severity`, `category`, `file_path`, `line_number`, `code_snippet`, `title`, `detail`, `suggestion`
- `summary_findings`: 其余缺陷简要索引列表（包含 `file_path`, `line_number`, `title`, `severity`）

## 输出格式要求

请使用简体中文，以 Markdown 格式输出，内容包括"一、检视结果概要"、"二、Coredump 风险隐患清单"、"三、总结与预防建议"三部分。

### 一、检视结果概要
- 介绍本次 Coredump 风险审查的目的、采用的范围和主要分析方法。
- 总结潜在崩溃风险隐患分布情况（致命、严重、一般、建议）。

> ⚠️ **概要部分的最后一行，格式约束（机器解析硬性约束，不得违反）**：
> - 必须严格使用 `致命：N，严重：N，一般：N，建议：N` 的格式，关键字和标点均不得更改
> - 冒号使用中文冒号 `：`，分隔符使用中文逗号 `，`
> - N 为非负整数，无问题填 `0`

### 二、Coredump 风险隐患清单
- 按照严重程度从高到低（致命 -> 严重 -> 一般 -> 建议）对风险列表进行整理排版。
- 每一个隐患条目应展示清晰的文件位置（包含代码行号）、风险分类、严重级别。
- 提供原汁原味的代码片段并突出显示风险行。
- 提供对应的崩溃路径分析和具体可靠的 C/C++ 修复代码方案。

### 三、总结与预防建议
- 评估本代码仓整体的防崩溃与内存/并发健壮性水平。
- 给出系统性的 Coredump 预防建议（例如：推广 RAII 设计模式、全面开启编译器防御警告参数如 `-Wall -Wextra`、引入 AddressSanitizer (ASan) / ThreadSanitizer (TSan) 动态检查等工程化手段）。

