import logging
import time
from contextlib import asynccontextmanager

from fastapi import FastAPI

from models import ScoreRequest, ScoreResult
from scorer import QualityScorer

logging.basicConfig(level=logging.INFO)
logger = logging.getLogger(__name__)

scorer_instance: QualityScorer | None = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    global scorer_instance
    logger.info("loading quality scorer models...")
    scorer_instance = QualityScorer()
    logger.info("scorer models loaded")
    yield
    scorer_instance = None


app = FastAPI(lifespan=lifespan)


@app.post("/score", response_model=ScoreResult)
def score(request: ScoreRequest) -> ScoreResult:
    start = time.time()
    try:
        result = scorer_instance.score(request)
    except Exception as exc:
        processing_ms = int((time.time() - start) * 1000)
        logger.error("scoring failed for request %s: %s", request.request_id, exc)
        result = ScoreResult(
            request_id=request.request_id,
            hallucination_score=0.0,
            factual_consistency_score=0.0,
            toxicity_score=0.0,
            overall_quality_score=0.0,
            flags=["scoring_error"],
            processing_ms=processing_ms,
        )
    logger.info(
        "scored request %s in %dms quality=%.3f flags=%s",
        result.request_id,
        result.processing_ms,
        result.overall_quality_score,
        result.flags,
    )
    return result


@app.get("/health")
def health():
    return {"status": "ok", "models_loaded": scorer_instance is not None}
