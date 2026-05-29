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
  hallucination_risk?: number
  grounding_score?: number
  risk_level?: string
  should_warn?: boolean
  rag_verdict?: string
  rag_grounding_score?: number
  rag_contradiction_score?: number
  rag_supported_claims?: string[]
  rag_unsupported_claims?: string[]
  rag_contradicted_claims?: string[]
  cross_model_verdict?: string
  cross_model_agreement?: number
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

export interface WarningItem {
  request_id: string
  session_id: string
  feature_name: string
  risk_level: string
  hallucination_risk: number
  grounding_score: number
  reasons: string[]
  timestamp: string
  rag_verdict?: string
}

export interface WarningsResponse {
  warnings: WarningItem[]
  total_today: number
}

export interface FeatureSetting {
  feature_name: string
  cost_alert_threshold_usd: number
  pii_masking_enabled: boolean
  webhook_url: string
  cross_model_enabled?: boolean
  cross_model_provider_url?: string
  cross_model_api_key?: string
  cross_model_model?: string
}

export interface ProviderKey {
  provider: string
  api_key: string
}

export interface Settings {
  feature_settings: FeatureSetting[]
  provider_keys: ProviderKey[]
}
