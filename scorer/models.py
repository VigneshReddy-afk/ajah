from pydantic import BaseModel


class ScoreRequest(BaseModel):
    request_id: str
    prompt: str
    response: str
    model: str
    feature_name: str


class ScoreResult(BaseModel):
    request_id: str
    hallucination_score: float
    factual_consistency_score: float
    toxicity_score: float
    overall_quality_score: float
    flags: list[str]
    processing_ms: int
