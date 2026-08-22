// 规范化严重级别定义 (Canonical Severity)
export type CanonicalSeverity = 'fatal' | 'critical' | 'major' | 'minor' | 'suggestion' | 'pass';

// 治理模式
export type GovernanceMode = 'defect_tracking' | 'entity_assessment';

// 严重级别元数据
export interface SeverityMeta {
  key: CanonicalSeverity;
  label: string;
  color: string;
  bg: string;
  weight: number;
}

// 任务元数据
export interface TaskReportMeta {
  id: number;
  repo_id: number;
  repo_name: string;
  repo_url: string;
  branch: string;
  task_type_id: number;
  task_type_name: string;
  task_type_display: string;
  engine_mode: 'single' | 'chunked';
  governance_mode: GovernanceMode;
  status: 'pending' | 'queued' | 'running' | 'cloning' | 'pre_processing' | 'analyzing' | 'synthesis' | 'post_processing' | 'merging' | 'success' | 'failed' | 'skipped';
  score: number;
  rating: string;
  total_chunks: number;
  processed_chunks: number;
  success_chunks: number;
  duration_seconds?: number;
  base_commit?: string;
  head_commit?: string;
  created_at: string;
}

// 统计指标
export interface KPIMetrics {
  total_findings: number;
  fatal_count: number;
  critical_count: number;
  major_count: number;
  minor_count: number;
  suggestion_count: number;
  pass_count: number;
  pass_rate?: number;
  category_stats?: Record<string, number>;
  status_stats?: Record<string, number>;
}

// 总结概览
export interface TaskReportSummary {
  meta: TaskReportMeta;
  markdown_content: string;
  metrics: KPIMetrics;
  key_recommendations?: string[];
}

// 详细清单项 (支持缺陷与实体评估两种语义)
export interface TaskFindingItem {
  id: number;
  task_report_id: number;
  task_type_id: number;
  repo_id: number;
  severity: CanonicalSeverity;
  severity_display: string;
  category: string;
  file_path: string;
  line_number: string;
  title: string;
  detail: string;
  code_snippet?: string;
  suggestion?: string; // 缺陷攻关模式下的修复建议
  status: string;      // open, analyzing, resolved, closed, pass, fail, invalid
  status_display: string;
  assignee_id?: number | null;
  assignee_name?: string;
  latest_comment?: string;
  created_at?: string;
}

// 清单分页响应
export interface FindingsPageResponse {
  items: TaskFindingItem[];
  total: number;
  page: number;
  pageSize: number;
  totalPages: number;
  metrics: KPIMetrics;
}

// 时序流单步
export interface PipelineStep {
  name: string;
  status: 'success' | 'failed' | 'running' | 'skipped';
  duration_seconds: number;
}

// 分片诊断
export interface ChunkDiagnosticDetail {
  chunk_name: string;
  status: 'success' | 'failed';
  duration_seconds: number;
  attempts: number;
  files_count: number;
  findings_count: number;
  error_message?: string;
  files?: string[];
}

// 运行轨迹与诊断
export interface TaskDiagnostics {
  meta: TaskReportMeta;
  pipeline_steps: PipelineStep[];
  total_duration: number;
  analysis_duration: number;
  chunks: ChunkDiagnosticDetail[];
  raw_output_log: string;
  log_truncated: boolean;
  total_log_lines: number;
  error_message?: string;
}

// 任务导航上下文 (上一任务/下一任务)
export interface TaskNavigationContext {
  prevTaskId?: number;
  nextTaskId?: number;
  currentIndex?: number;
  totalTasks?: number;
  onNavigate?: (taskId: number) => void;
}
