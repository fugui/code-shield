export interface ResourceEndpoint {
  name: string;
  base_url: string;
  api_key: string;
  model: string;
  concurrent: number;
  weight: number;
  temperature: number;
}

export interface ComputeResource {
  id: string;
  driver: 'native' | 'agy' | 'opencode' | 'claude' | 'codex' | string;
  model: string;
  concurrent: number;
  base_url?: string;
  api_key?: string;
  response_format_json?: boolean;
  max_retries?: number;
  retry_backoff_ms?: number;
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
}

export interface DebateTiers {
  tier1_hunter: TierBinding;
  tier2_reasoning: TierBinding;
  tier3_synthesis: TierBinding;
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
  max_queue_size: number;
  mock_on_missing_cli: boolean;
  throttling: {
    work_hours: WorkHoursConfig;
  };
  debate: DebateConfig;
  tools: ToolsConfig;
}

export interface GovernancePolicyConfig {
  fingerprint: {
    enabled: boolean;
    similarity_threshold: number;
  };
  lifecycle: {
    scope_guard_enabled: boolean;
    auto_resolve_missing: boolean;
    diff_gate_strict: boolean;
  };
  feedback_memory: {
    injection_enabled: boolean;
    max_rules_injected: number;
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
