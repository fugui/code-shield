# cJSON 内存泄漏代码检视综合报告生成指令

你将收到一份 JSON 格式的 cJSON 内存泄漏风险评估数据（基于 @analysis_prompt.md 提示词进行的分析）。请基于这些发现，生成一份指导开发者进行修复与生命周期治理的综合评估报告。

## 输入说明

输入为 JSON 格式的评估数据对象（Schema: `code-shield.tier4-synthesis-input.v1`），包含以下关键部分：
- `report`: 代码仓与检视基本元信息（`repo_name`, `task_type`, `head_commit` 等）
- `statistics`: 各严重级别缺陷统计数据（`fatal`, `critical`, `minor`, `suggestion` 等）
- `detailed_findings`: 重点 cJSON 内存泄漏缺陷详细列表，每个元素包含 `severity`, `category`, `file_path`, `line_number`, `code_snippet`, `title`, `detail`, `suggestion`
- `summary_findings`: 其余缺陷简要索引列表（包含 `file_path`, `line_number`, `title`, `severity`）

## 输出格式要求

请使用简体中文，以 Markdown 格式输出，内容包括"一、检视结果概要"、"二、cJSON 风险隐患清单"、"三、总结与预防建议"三部分。

### 一、检视结果概要
- 检视目的和 cJSON 生命周期管理评估方法的简要介绍。
- 总结潜在 cJSON 内存泄漏风险分布情况（致命、严重、一般、建议）。

> ⚠️ **概要部分的最后一行，格式约束（机器解析硬性约束，不得违反）**：
> - 必须严格使用 `致命：N，严重：N，一般：N，建议：N` 的格式，关键字和标点均不得更改
> - 冒号使用中文冒号 `：`，分隔符使用中文逗号 `，`
> - N 为非负整数，无问题填 `0`

### 二、cJSON 风险隐患清单
- 按照严重程度从高到低（致命 -> 严重 -> 一般 -> 建议）对风险列表进行整理排版。
- 每一个隐患条目应展示清晰的文件位置（包含代码行号）、风险分类、严重级别。
- 提供原汁原味的代码片段并突出显示风险行。
- 提供对应的泄漏路径分析和具体可靠的 cJSON 对象释放/所有权转移修复方案。

### 三、总结与预防建议
- 评估本代码仓整体的 cJSON 对象所有权管理与防泄漏水平。
- 给出系统性的 cJSON 内存泄漏预防建议（例如：明确 `cJSON_Delete` 释放责任、优先使用 RAII 智能指针或统一包装器、开启 ASan 动态检测等）。
