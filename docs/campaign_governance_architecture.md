# Code-Shield 专项分析通用化与元数据驱动架构设计

> **版本**：v1.0  
> **状态**：已评审 (Approved)  
> **适用系统**：Code-Shield 质量与安全扫描平台  
> **核心目标**：解除专项分析对硬编码的依赖，实现**“零编码配置，新扫描任务一键升级为专项治理”**

---

## 1. 背景与现状问题

### 1.1 业务背景
Code-Shield 系统当前核心能力分为两层：
1. **扫描任务（Scan Tasks）**：支持单仓或大代码仓分片并行扫描，结合大模型输出结构化缺陷发现（`AnalysisFinding`）。
2. **专项分析（Campaign Analysis）**：针对特定的质量/安全专项（如 Python 浮点数、显式创建线程、cJSON 内存泄漏、Coredump 风险、UT 测试用例有效性等），进行代码仓维度聚合看板、部门排名、30天缺陷收敛趋势跟踪、问题人工审计与闭环核销。

### 1.2 现状痛点：新增专项需要重度编码
目前系统中，任意新增一个扫描任务类型若想具备“专项分析”能力，必须由研发人员修改 **6~8 处前后端代码** 并重新发布上线：

```mermaid
flowchart TD
    subgraph Current ["当前硬编码架构（紧耦合）"]
        A["1. 后端 Models"] -->|为每个专项新建独立 Struct| B["FloatFinding, CjsonFinding, DeepReviewFinding..."]
        C["2. DB 迁移"] -->|models/db.go AutoMigrate| D["逐表显式注册迁移"]
        E["3. 归并 Hooks"] -->|services/hooks.go 注册泛型| F["RegisterTaskHook('xxx', handleCampaignHook[T])"]
        G["4. API 路由"] -->|main.go 显式注册| H["registerCampaignRoutes[T](api, 'path', 'name')"]
        I["5. 级联与工作台"] -->|handlers/task.go, workbench.go| J["逐表 switch-case / 逐表 Query Find"]
        K["6. 前端菜单与路由"] -->|menu.ts, App.tsx| L["写死菜单项、写死路由、写死 Header 映射"]
        M["7. 前端组件"] -->|pages/ 目录| N["新建 FloatAnalysis.tsx 等包装组件"]
    end
```

### 1.3 核心改造诉求
**彻底实现元数据驱动（Metadata-Driven）**：在管理员创建或编辑任务类型（`TaskType`）时，只需在后台勾选/配置专项属性，系统自动完成动态菜单挂载、通用路由解析、事件驱动归并与个人工作台汇总，**全程无需编写一行代码**。

---

## 2. 治理模式抽象：解耦两类专项的差异

通过对现有专项的深入分析，系统存在两类不同的治理模型：**缺陷攻关模式** 与 **全量实体评估模式（如 UT 有效性）**。我们将其抽象为统一的 `governance_mode`，使系统具备极强的泛化能力。

```mermaid
graph LR
    subgraph ModeA ["模式 A：缺陷攻关模式 (defect_tracking)"]
        A1["扫描目标: 发现 Bug（无 Bug 则 0 条）"]
        A2["度量维度: 缺陷总数 / 待处理 / 修复率 (Fix Rate)"]
        A3["归并机制: 代码 Hash + 行号近邻 + LLM 语义相似度"]
        A4["典型场景: 浮点数、多线程、内存泄露、Coredump、代码深检"]
    end

    subgraph ModeB ["模式 B：全量实体评估模式 (entity_assessment)"]
        B1["扫描目标: 全量用例/实体评级（包含合格用例）"]
        B2["度量维度: 实体总数 / 合格数 / 合格率 (Pass Rate)"]
        B3["归并机制: 实体名称(TestCaseName) + 文件路径 精确比对"]
        B4["典型场景: 测试用例有效性、API 文档覆盖率、架构合规性"]
    end
```

### 差异与统一规则对比表

| 维度 | 缺陷攻关模式 (`defect_tracking`) | 全量实体评估模式 (`entity_assessment`) |
| :--- | :--- | :--- |
| **严重等级（Severity）** | 致命、严重、一般、建议 | **合格 (Pass)**、致命、严重、一般、建议 |
| **初始状态流转** | 默认全部为 `open`（待治理） | 级别为【合格】初始化为 `closed`，其余初始化为 `open` |
| **实体标识字段** | 缺陷标题（`Title`） | 实体唯一名称（如 `TestCaseName` 映射至 `Title`） |
| **问题归并算法** | Phase 1 代码特征匹配 + Phase 2 LLM 语义比对 | 路径 + 实体名称 精确哈希比对（$O(1)$ 无需大模型） |
| **看板展示指标** | 总缺陷数、待治理数、**修复率（Fix Rate）** | 总实体数、合格实体数、**合格率（Pass Rate）** |

---

## 3. 总体架构设计

系统总体架构分为 **元数据配置层、通用存储层、治理引擎层、动态 API 层与自适应 UI 层**：

```mermaid
flowchart TB
    subgraph Layer1 ["1. 元数据配置层 (Admin Config)"]
        C1["TaskType 元数据: is_campaign / governance_mode / campaign_path / campaign_icon"]
    end

    subgraph Layer2 ["2. 通用存储层 (Unified Storage)"]
        D1["统一 campaign_findings 表 (替代原 7 张独立分表)"]
        D2["统一唯一约束: (task_type_id, repo_id, file_path, title)"]
    end

    subgraph Layer3 ["3. 治理引擎层 (Generic Engine)"]
        E1["事件触发型通用归并 Hook (handleGenericCampaignHook)"]
        E2["分支 A: entity_assessment 精确匹配与自动合格闭环"]
        E3["分支 B: defect_tracking 确定性规则 + LLM 语义归并"]
    end

    subgraph Layer4 ["4. 动态 API 层 (Dynamic RESTful API)"]
        A1["/api/analysis/:campaign/repos (代码仓看板)"]
        A2["/api/analysis/:campaign/findings (缺陷/用例列表及审计)"]
        A3["/api/analysis/:campaign/departments (部门统计)"]
        A4["/api/analysis/:campaign/trends (30天收敛趋势)"]
        A5["/api/analysis/:campaign/findings/export (Excel通用导出)"]
        A6["/api/me/findings (个人工作台全专项聚合)"]
    end

    subgraph Layer5 ["5. 自适应 UI 层 (Universal Frontend)"]
        U1["动态侧边栏: 根据 is_campaign 自动加载专项菜单"]
        U2["通用路由: /analysis/:campaignKey -> UniversalCampaignPage"]
        U3["自适应看板: 根据 governance_mode 切换 [合格率/修复率] 指标"]
        U4["个人工作台: 动态下拉选项与通用审计抽屉 (AuditingWorkspace)"]
    end

    Layer1 --> Layer2
    Layer2 --> Layer3
    Layer3 --> Layer4
    Layer4 --> Layer5
```

---

## 4. 详细模块设计

### 4.1 数据模型层设计

#### 1. `TaskType` 元数据扩展
```go
type TaskType struct {
    ID              uint           `gorm:"primaryKey" json:"id"`
    Name            string         `gorm:"uniqueIndex;not null" json:"name"`       // 唯一标识: "float_comparison"
    DisplayName     string         `gorm:"not null" json:"display_name"`           // 中文名: "Python浮点数专项"
    Description     string         `json:"description"`                            // 任务说明
    EngineMode      string         `gorm:"default:single" json:"engine_mode"`      // single / chunked
    
    // ── 专项分析元数据扩展 ──
    IsCampaign      bool           `gorm:"default:false;index" json:"is_campaign"` // 是否启用为专项分析
    CampaignPath    string         `gorm:"size:100;default:''" json:"campaign_path"` // 路由别名 (空则默认同 Name)
    GovernanceMode  string         `gorm:"size:50;default:'defect_tracking'" json:"governance_mode"` // defect_tracking / entity_assessment
    CampaignIcon    string         `gorm:"type:text" json:"campaign_icon"`         // SVG 图标路径或图标类名
    CampaignConfig  datatypes.JSON `json:"campaign_config"`                        // 高级配置 (如通知规则、严重度过滤)
    
    IsActive        bool           `gorm:"default:true" json:"is_active"`
    CreatedAt       time.Time      `json:"created_at"`
    UpdatedAt       time.Time      `json:"updated_at"`
}
```

#### 2. 统一专项缺陷表（`CampaignFinding`）
```go
type CampaignFinding struct {
    ID           uint           `gorm:"primaryKey" json:"id"`
    TaskTypeID   uint           `gorm:"uniqueIndex:idx_camp_finding_uniq,priority:1;index;not null" json:"task_type_id"`
    TaskType     TaskType       `gorm:"foreignKey:TaskTypeID" json:"task_type"`
    RepoID       uint           `gorm:"uniqueIndex:idx_camp_finding_uniq,priority:2;index;not null" json:"repo_id"`
    Repo         Repository     `gorm:"foreignKey:RepoID" json:"repo"`
    TaskReportID uint           `gorm:"index" json:"task_report_id"`
    
    // 缺陷本身属性
    FilePath     string         `gorm:"uniqueIndex:idx_camp_finding_uniq,priority:3;size:500;not null" json:"file_path"`
    LineNumber   string         `gorm:"size:255" json:"line_number"`
    Title        string         `gorm:"uniqueIndex:idx_camp_finding_uniq,priority:4;size:500;not null" json:"title"` // 普通专项存缺陷标题，UT存测试用例名称
    Detail       string         `gorm:"type:text" json:"detail"`
    Severity     string         `gorm:"size:100;not null;index" json:"severity"` // 致命/严重/一般/建议/合格
    Category     string         `gorm:"size:255;index" json:"category"`
    CodeSnippet  string         `gorm:"type:text" json:"code_snippet"`
    Suggestion   string         `gorm:"type:text" json:"suggestion"`
    
    // 治理状态与审计跟踪
    Status       string         `gorm:"default:'open';size:50;index" json:"status"` // open, analyzing, resolved, closed, invalid
    AssigneeID   *uint          `json:"assignee_id"`
    Assignee     *User          `gorm:"foreignKey:AssigneeID" json:"assignee,omitempty"`
    StatusLog    datatypes.JSON `json:"status_log"` // [{"status":"open","time":"...","user":"xxx","reason":"..."}]
    Feedback     string         `gorm:"type:text" json:"feedback"`
    CreatedAt    time.Time      `gorm:"index" json:"created_at"`
    UpdatedAt    time.Time      `json:"updated_at"`
}
```

---

### 4.2 统一归并引擎设计

任务执行完毕后，`task_runner` 检查如果 `ctx.taskType.IsCampaign == true`，自动触发通用归并引擎：

```go
func handleGenericCampaignHook(ctx *taskContext, findings []models.AnalysisFinding) error {
    isEntityMode := ctx.taskType.GovernanceMode == "entity_assessment"
    
    // 1. 查询该代码仓在当前专项下的所有存量记录
    var allOldFindings []models.CampaignFinding
    models.DB.Where("task_type_id = ? AND repo_id = ?", ctx.taskType.ID, ctx.repo.ID).Find(&allOldFindings)

    matchedOldIDs := make(map[uint]bool)
    matchedFindingsMap := make(map[int]*models.CampaignFinding)

    if isEntityMode {
        // ── 模式 B (实体评估): 确定性 O(1) 路径+用例名匹配 ──
        for idx, f := range findings {
            for i := range allOldFindings {
                oldF := &allOldFindings[i]
                if oldF.FilePath == f.FilePath && oldF.Title == f.Title {
                    matchedFindingsMap[idx] = oldF
                    matchedOldIDs[oldF.ID] = true
                    break
                }
            }
        }
    } else {
        // ── 模式 A (缺陷攻关): Phase 1 硬规则 + Phase 2 LLM 语义模糊比对 ──
        runDefectFuzzyAndLLMMatching(ctx, findings, allOldFindings, matchedOldIDs, matchedFindingsMap)
    }

    // ── Phase 3: 状态继承、新问题落库与消失问题自动置为已解决 ──
    syncFindingsToDatabase(ctx, findings, allOldFindings, matchedFindingsMap, matchedOldIDs, isEntityMode)
    return nil
}
```

---

### 4.3 动态 API 路由设计

统一使用 Gin 参数化动态路由替换原有的静态路由注册：

```go
func registerDynamicCampaignRoutes(rg *gin.RouterGroup) {
    campaignGroup := rg.Group("/analysis/:campaign")
    campaignGroup.Use(handlers.ResolveCampaignMiddleware()) // 中间件根据 :campaign 注入 TaskType 上下文
    {
        campaignGroup.GET("/repos", handlers.GetDynamicCampaignRepos)
        campaignGroup.GET("/findings", handlers.GetDynamicCampaignFindings)
        campaignGroup.GET("/findings/export", handlers.ExportDynamicCampaignFindings)
        campaignGroup.PATCH("/findings/:id", handlers.UpdateDynamicCampaignFinding)
        campaignGroup.GET("/departments", handlers.GetDynamicCampaignDepartments)
        campaignGroup.GET("/trends", handlers.GetDynamicCampaignTrends)
    }
}
```

*   **个人工作台查询极简化**：
    ```go
    func GetMyFindings(c *gin.Context) {
        uid := c.GetUint("userID")
        var list []models.CampaignFinding
        models.DB.Preload("Repo").Preload("TaskType").
            Where("assignee_id = ?", uid).
            Order("id desc").Find(&list)
        c.JSON(http.StatusOK, list)
    }
    ```

---

### 4.4 前端通用视图引擎设计

#### 1. 动态侧边栏菜单（`Sidebar.tsx`）
前端系统启动时请求 `/api/task-types?is_campaign=true`，动态将专项列表拼装进菜单项：
```ts
const campaignMenuItems: SubMenuItem[] = activeCampaignTypes.map(tt => ({
    path: `/analysis/${tt.campaign_path || tt.name}`,
    label: tt.display_name,
    icon: tt.campaign_icon || DEFAULT_CAMPAIGN_ICON,
}));
```

#### 2. 通用动态路由渲染（`App.tsx`）
```tsx
{/* 单一通用路由替代原有的多个静态路由 */}
<Route path="/analysis/:campaignKey" element={<PrivateRoute><UniversalCampaignPage /></PrivateRoute>} />
```

#### 3. 通用页面组件（`UniversalCampaignPage.tsx`）
内部直接复用并增强已有的 `<CampaignAnalysis />`，接收 URL 中的 `campaignKey`，自动获取任务类型的 `display_name`、`description` 和 `governance_mode`，自适应切换看板指标展示。

---

## 5. 数据迁移方案

### 5.1 迁移策略（开箱即用，自动幂等）
在服务启动时（`models/db.go`），自动执行迁移逻辑。整个迁移过程具备**幂等性、无损性、可回滚性**：

```mermaid
sequenceDiagram
    participant S as 服务启动 (InitDB)
    participant DB as PostgreSQL
    S->>DB: 1. AutoMigrate(CampaignFinding)
    S->>DB: 2. 检查是否存在旧分表 (test_case_findings 等)
    alt 存在旧表且尚未迁移
        S->>DB: 3. 批量 INSERT INTO campaign_findings ... ON CONFLICT DO NOTHING
        S->>DB: 4. 更新 task_types 的 is_campaign = TRUE 与 governance_mode
        S->>DB: 5. 重命名备份旧表为 _legacy_xxx
        S->>S: 记录迁移成功审计日志
    else 已迁移完成
        S->>S: 自动跳过，无需重复执行
    end
```

### 5.2 字段迁移映射关系

```sql
-- 示例：UT 有效性评估迁移 (test_case_name -> title)
INSERT INTO campaign_findings (
    task_type_id, repo_id, task_report_id, file_path, line_number,
    title, detail, severity, category, code_snippet, suggestion,
    status, assignee_id, status_log, created_at, updated_at
)
SELECT 
    (SELECT id FROM task_types WHERE name = 'ut_effectiveness' LIMIT 1),
    repo_id, task_report_id, file_path, line_number,
    test_case_name AS title, detail, severity, category, code_snippet, suggestion,
    status, assignee_id, status_log::jsonb, created_at, updated_at
FROM test_case_findings
ON CONFLICT (task_type_id, repo_id, file_path, title) DO NOTHING;
```

---

## 6. 改造收益与落地效果

| 维度 | 改造前（代码绑定模式） | 改造后（元数据驱动模式） |
| :--- | :--- | :--- |
| **新增专项成本** | 需要 1 名研发人员修改 Go 和 React 6~8 个文件并编译发版 | **零代码**：管理员在后台新建任务类型并勾选【启用专项治理】即刻生效 |
| **修改专项文案/图标** | 需要前后端改代码重新发版 | 后台修改即时生效 |
| **代码冗余度** | 存在 7 张雷同数据表、7 套重复的导出与查询逻辑 | 数据表与 API 彻底统一，精简代码千余行 |
| **系统泛化能力** | 仅支持既有专项 | 未来任何新扫描类型（如安全审计、API注释合规等）天然具备专项治理与看板能力 |

---

## 7. 实施演进路线图

```mermaid
gantt
    title Code-Shield 专项通用化实施路线
    dateFormat  YYYY-MM-DD
    section 阶段一：后端通用化
    设计并新增 CampaignFinding 模型与 TaskType 字段       :done, 2026-08-20, 1d
    实现通用归并 Hook (handleGenericCampaignHook)          :active, 2026-08-21, 2d
    实现参数化动态路由 /api/analysis/:campaign/*          :2026-08-23, 1d
    编写启动自动幂等数据迁移逻辑                          :2026-08-24, 1d
    section 阶段二：前端动态化
    TaskType 管理后台增加专项属性配置开关                 :2026-08-25, 1d
    改造 Sidebar 与 App.tsx 支持动态菜单与通配路由         :2026-08-26, 2d
    CampaignAnalysis 适配 governance_mode 模式切换        :2026-08-28, 1d
    section 阶段三：收敛与清理
    下线历史独立分表与重复的前端包装页面                   :2026-08-29, 1d
    全链路回归测试与上线交付                              :2026-08-30, 1d
```
