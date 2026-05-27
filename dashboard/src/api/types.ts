export interface CostMetrics {
  date: string
  by_user: Record<string, number>
  by_feature: Record<string, number>
  by_model: Record<string, number>
  total_traces: number
  pii_masked_count: number
}

export interface TraceRecord {
  trace_id: string
  request_id: string
  user_id: string
  session_id: string
  feature_name: string
  agent_step: string
  parent_step_id: string
  step_name: string
  tool_name: string
  provider: string
  model: string
  input_tokens: number
  output_tokens: number
  cost_usd: number
  latency_ms: number
  status_code: number
  masked_prompt: string
  was_pii_masked: boolean
  quality_score: number
  timestamp: string
}

export interface SessionRecord {
  session_id: string
  start_time: string
  end_time: string
  total_cost: number
  total_tokens: number
  step_count: number
  avg_quality: number
  status: string
  feature_name: string
  user_id: string
}

export interface SessionDetailRecord extends SessionRecord {
  traces: TraceRecord[]
}

export interface SessionsResponse {
  sessions: SessionRecord[]
}

export interface Alert {
  timestamp: string
  type: string
  feature: string
  threshold: number
  actual: number
}

export interface FeatureSetting {
  feature_name: string
  cost_alert_threshold_usd: number
  pii_masking_enabled: boolean
}

export interface ProviderKey {
  provider: string
  api_key: string
}

export interface Settings {
  feature_settings: FeatureSetting[]
  provider_keys: ProviderKey[]
}
