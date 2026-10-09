// 规范化严重级别定义 (Canonical Severity)
export type CanonicalSeverity = 'fatal' | 'critical' | 'major' | 'minor' | 'suggestion' | 'pass';

// 治理模式
export type GovernanceMode = 'entity_assessment' | 'full_ledger' | 'change_focus';

export interface ScopeDecision {
  decision: 'proceed' | 'proceed_degraded' | 'skipped' | 'failed';
  reason: string;
  scope_profile: string;
  primary_unit: string;
  candidate_files: number;
  primary_units: number;
  entity_units?: number;
  keyword_occurrence_units?: number;
  failed_files?: number;
  unknown_files?: number;
  failed_units?: number;
  unknown_units?: number;
  change_hunks?: number;
  plan_manifest_hash: string;
  change_overview?: ChangeOverview;
  message?: string;
}

export interface ChangeOverview {
  added_files: number;
  modified_files: number;
  deleted_files: number;
  renamed_files: number;
  binary_files: number;
  changed_hunks: number;
  files: ChangeOverviewFile[];
}

export interface ChangeOverviewFile {
  path: string;
  old_path?: string;
  new_path?: string;
  change_kind: 'added' | 'modified' | 'deleted' | 'renamed';
  language: string;
  binary?: boolean;
  hunk_ranges?: string[];
}

export interface PlanReconciliation {
  planned_units: number;
  matched_units: number;
  missing_units?: string[];
  unmatched_units?: string[];
}

export interface CoverageFile {
  path: string;
  status: string;
  error?: string;
  diff_touched: boolean;
  deleted?: boolean;
  hunk_ranges?: string[];
}

export interface AssessmentIssue {
  category: string;
  severity: string;
  detail: string;
  suggestion: string;
}

export interface EntityAssessmentArtifact {
  entity_id: string;
  status: 'valid' | 'invalid' | 'needs_human';
  purpose?: string;
  evidence?: Record<string, unknown>;
  issues?: AssessmentIssue[];
}

export interface KeywordAssessmentArtifact {
  occurrence_id: string;
  is_thread_creation: boolean;
  status: 'valid' | 'issue' | 'not_thread_creation' | 'needs_human';
  evidence?: Record<string, unknown>;
  issues?: AssessmentIssue[];
}

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
  engine_mode: 'debate_full';
  scope_decision?: ScopeDecision;
  plan_reconciliation?: PlanReconciliation;
  coverage_summary?: CoverageExecutionSummary;
  scan_profile?: Record<string, unknown>;
  scan_profile_hash?: string;
  prompt_version?: string;
  engine_config_hash?: string;
  planner_version?: string;
  assessment_config_hash?: string;
  prompt_content_hash?: string;
  category_schema_hash?: string;
  taxonomy_schema_version?: number;
  taxonomy_hash?: string;
  domain_family?: string;
  defense_dimensions?: Array<{ key?: string; name?: string; description?: string; dimension?: string }>;
  execution_snapshot_version?: number;
  execution_snapshot_state?: 'complete' | 'legacy_readonly' | string;
  governance_mode: GovernanceMode;
  status: 'pending' | 'queued' | 'running' | 'cloning' | 'pre_processing' | 'analyzing' | 'synthesis' | 'post_processing' | 'merging' | 'success' | 'degraded' | 'failed' | 'skipped';
  score: number;
  rating: string;
  total_chunks: number;
  processed_chunks: number;
  success_chunks: number;
  coverage_not_applicable?: boolean;
  coverage_complete?: boolean;
  coverage_degraded?: boolean;
  coverage_reasons?: string[];
  changed_coverage_files?: CoverageFile[];
  duration_seconds?: number;
  base_commit?: string;
  head_commit?: string;

  // ── Token 统计 ──
  tier1_tokens?: number;
  tier2_tokens?: number;

  created_at: string;
}

export interface CoverageExecutionFailure {
  chunk_uid?: string;
  chunk_name?: string;
  primary_unit_id?: string;
  file_path?: string;
  stage?: string;
  error_class?: string;
  error_message?: string;
}

export interface CoverageExecutionSummary {
  coverage_state: 'COMPLETE' | 'PARTIAL' | 'FAILED' | 'NOT_APPLICABLE' | 'UNKNOWN';
  total_chunks: number;
  processed_chunks: number;
  success_chunks: number;
  failed_chunks: number;
  missing_assessments: string[];
  failed_files: CoverageExecutionFailure[];
  failed_chunk_items: CoverageExecutionFailure[];
  coverage_complete: boolean;
  coverage_degraded: boolean;
  coverage_not_applicable: boolean;
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
  assessment_stats?: Record<string, number>;
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
  category_code?: string;
  category_source?: string;
  category_status?: string;
  classification_rationale?: string;
  taxonomy_hash?: string;
  file_path: string;
  line_number: string;
  title: string;
  detail: string;
  code_snippet?: string;
  suggestion?: string; // 修复建议
  status: string;      // open, analyzing, resolved, closed, pass, fail, invalid
  status_display: string;
  defect_id?: number | null;
  assignee_id?: number | null;
  assignee_name?: string;
  latest_comment?: string;

  // ── 物理定位与辩论证据 ──
  fingerprint?: string;
  trigger_line?: string;
  scope_symbol?: string;
  hunter_claim?: string;
  challenger_arg?: string;
  judge_verdict?: string;
  observation_group_uid?: string;
  anchor_confidence?: 'HIGH' | 'MEDIUM' | 'LOW';
  primary_unit_id?: string;
  assessment_status?: string;
  assessment_artifact?: EntityAssessmentArtifact | KeywordAssessmentArtifact;

  created_at?: string;
}

// 智能体三方对抗辩论轨迹
export interface TaskDebateLog {
  id: number;
  task_report_id: number;
  chunk_name: string;
  candidate_id: string;
  trigger_line: string;
  hunter_output: Record<string, unknown>;
  challenger_output?: Record<string, unknown>;
  judge_output: Record<string, unknown>;
  verdict: 'CONFIRMED' | 'REJECTED' | 'CONDITIONAL';
  duration_ms: number;
  token_usage?: {
    hunter_tokens?: number;
    challenger_tokens?: number;
    judge_tokens?: number;
  };
  created_at: string;
}

// 代码仓人机反馈例外规则
export interface RepoFeedbackRule {
  id: number;
  repo_id: number;
  task_type_id: number;
  scope_type: 'FILE' | 'REPO' | 'GLOBAL';
  pattern: string;
  rule_action: 'IGNORE' | 'DOWNGRADE';
  reason: string;
  created_by: string;
  created_at: string;
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

// 跨轮对账只读投影，所有字段由 ledger 表即时计算，不在报告页复制事实
export type ReconciliationVerdict =
  | 'NEW'
  | 'EXISTED'
  | 'REOPENED'
  | 'PROBABLE'
  | 'RESOLVED'
  | 'COVERAGE_GAP';

export interface ReconciliationQuality {
  candidate_budget_exceeded: number;
  ai_unavailable: number;
  identity_moved: number;
  gray_zone_auto_resolved: number;
}

export interface ObservationProjection {
  observation_group_uid: string;
  defect_id?: number | null;
  verdict: ReconciliationVerdict | string;
  match_tier: string;
  confidence: number;
  score_detail?: Record<string, number>;
  reason?: string;
  source_finding_ids: number[];
  source_findings?: ObservationSourceFinding[];
}

export interface ObservationSourceFinding {
  id: number;
  title: string;
  file_path: string;
  line_number: string;
  scope_symbol?: string;
  severity?: string;
  category?: string;
  detail?: string;
  code_snippet?: string;
  suggestion?: string;
}

export interface ReconciliationEvent {
  id: number;
  defect_id: number;
  report_id?: number | null;
  event_type: string;
  from_status?: string;
  to_status?: string;
  actor_type: string;
  actor_id?: number | null;
  reason?: string;
  evidence?: Record<string, unknown>;
  created_at: string;
}

export interface DefectDetailData {
  id: number | string;
  title?: string;
  detail?: string;
  category?: string;
  code_snippet?: string;
  suggestion?: string;
  severity?: string;
  norm_path?: string;
  file_path?: string;
  line_start?: number | null;
  line_end?: number | null;
  status?: string;
  status_label?: string;
  status_reason?: string;
  missed_count?: number;
  dormant_rounds?: number;
  last_matched_report_id?: number;
}

export interface ReconciliationUnmatchedDefect extends DefectDetailData {
  id: number;
  severity: string;
  norm_path: string;
  status: string;
  missed_count: number;
  dormant_rounds: number;
  last_matched_report_id: number;
}

export interface ReconciliationSummary {
  report_id: number;
  coverage_state: string;
  worktree_clean: boolean;
  ledger_ready: boolean;
  algorithm_version: string;
  stats: Record<string, number>;
  quality: ReconciliationQuality;
  observations: ObservationProjection[];
  events: ReconciliationEvent[];
  unmatched_open_defects: ReconciliationUnmatchedDefect[];
}

export interface DefectCandidate {
  defect_id: number;
  status: string;
  norm_path: string;
  line_start?: number | null;
  line_end?: number | null;
  match_tier: string;
  score: number;
  score_detail?: Record<string, number>;
  sources?: string[];
  title?: string;
  detail?: string;
  category?: string;
  code_snippet?: string;
  suggestion?: string;
  severity?: string;
  symbol_path?: string;
  status_reason?: string;
  first_report_id?: number;
  last_seen_report_id?: number;
  last_matched_report_id?: number;
  missed_count?: number;
  dormant_rounds?: number;
  human_locked?: boolean;
  human_decision?: string;
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
  retries?: number;
  contract_repairs: number;
  resource_failovers?: number;
  driver_failovers: number;
  resource_chain?: string[];
  error_classes?: string[];
  queue_wait_ms?: number[];
  attempt_duration_seconds?: number[];
  split_depth: number;
  split_count: number;
  resumed?: boolean;
  artifact_complete?: boolean;
  artifact_state?: string;
  artifact_quality_degraded?: boolean;
  unresolved_issue_count?: number;
  normalized_issue_count?: number;
  schema_repair_attempts?: number;
  schema_repair_successes?: number;
  candidate_quarantine_count?: number;
  schema_repair_issues?: string[];
  degraded_reasons?: string[];
  files_count: number;
  findings_count: number;
  error_message?: string;
  error_class?: string;
  files?: string[];
}

export interface SynthesisDiagnosticSummary {
  status?: string;
  attempts: number;
  resource_id?: string;
  resource_failovers: number;
  driver_failovers: number;
  resource_chain?: string[];
  error_classes?: string[];
  queue_wait_ms?: number[];
  attempt_duration_seconds?: number[];
  duration_seconds: number;
  error_message?: string;
}

// 运行轨迹与诊断
export interface TaskDiagnostics {
  meta: TaskReportMeta;
  pipeline_steps: PipelineStep[];
  total_duration: number;
  analysis_duration: number;
  attempts: number;
  retries: number;
  contract_repairs: number;
  resource_failovers?: number;
  driver_failovers: number;
  split_invocations: number;
  recovered_chunks: number;
  artifact_complete?: boolean;
  artifact_state?: string;
  artifact_quality_degraded?: boolean;
  unresolved_issue_count?: number;
  normalized_issue_count?: number;
  schema_repair_attempts?: number;
  schema_repair_successes?: number;
  candidate_quarantine_count?: number;
  category?: {
    hunter: {
      model_candidates: number;
      model_code_valid: number;
      model_code_invalid: number;
      model_code_absent: number;
    deterministic_repairs: number;
    llm_repair_attempts: number;
    llm_repairs: number;
    review_required: number;
    category_only_quarantined: number;
    extra_fields_ignored?: number;
  };
    judge: {
      model_candidates: number;
      model_code_valid: number;
      model_code_invalid: number;
      model_code_absent: number;
      review_required: number;
      rejected?: number;
      llm_repair_attempts?: number;
      llm_repairs?: number;
      category_only_quarantined: number;
      extra_fields_ignored?: number;
    };
  };
  degraded_reasons?: string[];
  chunks: ChunkDiagnosticDetail[];
  synthesis?: SynthesisDiagnosticSummary;
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
