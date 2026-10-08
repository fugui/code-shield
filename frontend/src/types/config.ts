export interface ResourceEndpoint {
  name: string;
  base_url: string;
  api_key: string;
  model: string;
  concurrent: number;
}

export interface ComputeResource {
  id: string;
  driver: 'native' | 'agy' | 'opencode' | 'claude' | 'codex' | string;
  model: string;
  concurrent: number;
  base_url?: string;
  api_key?: string;
  response_format_json?: boolean;
  enable_thinking?: boolean;
  max_tokens?: number;
  max_retries?: number;
  retry_backoff_ms?: number;
  attempt_timeout_seconds?: number;
  first_byte_timeout_seconds?: number;
  idle_timeout_seconds?: number;
  max_output_bytes?: number;
  endpoints?: ResourceEndpoint[];
}

export interface LLMConfig {
  default_resource: string;
  debug_logs: boolean;
  resources: ComputeResource[];
}

export interface WorkHoursConfig {
  enabled: boolean;
  workdays: number[];
  start_time: string;
  end_time: string;
  scale: number;
}

export interface TierBinding {
  resource?: string;
  resources?: string[];
  timeout_seconds: number;
  attempt_timeout_seconds?: number;
  first_byte_timeout_seconds?: number;
  idle_timeout_seconds?: number;
  max_output_bytes?: number;
}

export interface DebateTiers {
  tier1_hunter: TierBinding;
  tier2_challenger?: TierBinding;
  tier3_judge?: TierBinding;
  tier4_synthesis?: TierBinding;
}

export interface DebateConfig {
  enabled: boolean;
  fast_pass_enabled: boolean;
  max_candidates_per_chunk: number;
  stage_timeout_seconds: number;
  log_retention_days: number;
  backpressure_threshold: number;
  backpressure_timeout_seconds: number;
  tiers: DebateTiers;
}

export interface ToolsConfig {
  default_resource: string;
  overrides: Record<string, string>;
}

export interface ScannerConfig {
  worker_count: number;
  chunk_concurrency: number;
  max_queue_size: number;
  mock_on_missing_cli: boolean;
  analysis?: {
    max_retries?: number;
    retry_backoff_ms?: number;
    max_backoff_seconds?: number;
    retryable_errors?: string[];
  };
  artifact?: {
    normalization_enabled?: boolean;
    max_schema_repair_attempts?: number;
    schema_repair_timeout_seconds?: number;
    schema_repair_resource?: string;
    allow_candidate_salvage?: boolean;
    max_quarantined_candidate_ratio?: number;
  };
  resume?: {
    legacy_v2_policy?: string;
  };
  throttling: {
    work_hours: WorkHoursConfig;
  };
  opencode?: {
    continuation: {
      enabled: boolean;
      max_seconds: number;
    };
  };
  debate: DebateConfig;
  tools: ToolsConfig;
  determinism: {
    enabled: boolean;
    temperature: number;
    bind_model_per_task: boolean;
    prefer_seed: boolean;
    seed_policy: string;
  };
}

export interface GovernancePolicyConfig {
  identity?: {
    algorithm_version?: string;
    max_candidates_per_observation?: number;
    max_candidate_edges_per_report?: number;
    max_assignment_width?: number;
    strong_same_threshold?: number;
    assign_band?: number;
    reject_below?: number;
    auto_resolve_gray_zone?: boolean;
    ai_arbitration_confidence?: number;
    gray_zone_fallback_merge_score?: number;
  };
  arbitration?: {
    enabled?: boolean;
    max_calls_per_report: number;
    context_lines: number;
    timeout_seconds: number;
  };
  lifecycle?: {
    high_risk_severities?: string[];
    resolved_rounds?: number;
    dormant_rounds?: number;
    obsolete_after_dormant_rounds?: number;
    require_coverage?: boolean;
    require_change?: boolean;
  };
}

export interface NotificationConfig {
  webhook: string;
}

export interface FullConfigResponse {
  llm: LLMConfig;
  scanner: ScannerConfig;
  governance: GovernancePolicyConfig;
  notification: NotificationConfig;
}

export interface PingResult {
  success: boolean;
  latency_ms?: number;
  status_code?: number;
  message: string;
}
