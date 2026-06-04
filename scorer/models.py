from pydantic import BaseModel, Field


class ScoreRequest(BaseModel):
    request_id: str
    prompt: str
    response: str
    model: str
    feature_name: str
    source_context: str | None = None  # base64-encoded source document; None = no RAG verification


class ScoreResult(BaseModel):
    request_id: str
    hallucination_score: float
    factual_consistency_score: float
    toxicity_score: float
    overall_quality_score: float
    flags: list[str]
    processing_ms: int
    claim_density_risk: float = 0.0
    hedge_risk: float = 0.0
    # RAG verification fields — populated when source_context is provided
    rag_verdict: str = "not_applicable"
    rag_grounding_score: float = 0.0
    rag_contradiction_score: float = 0.0
    rag_supported_claims: list[str] = Field(default_factory=list)
    rag_unsupported_claims: list[str] = Field(default_factory=list)
    rag_contradicted_claims: list[str] = Field(default_factory=list)
