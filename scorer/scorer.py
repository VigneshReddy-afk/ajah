import logging
import re
import time

import numpy as np
from sentence_transformers import SentenceTransformer
from sklearn.metrics.pairwise import cosine_similarity
from transformers import pipeline

from models import ScoreRequest, ScoreResult

logger = logging.getLogger(__name__)

_REFUSAL_PHRASES = ("I cannot", "I am not able", "as an AI", "I don't know")
# Matches numbers or "Name is word" patterns — indicators of specific factual claims.
_CLAIM_PATTERN = re.compile(r'\b\d+\b|[A-Z][a-z]+\s+is\s+\w+')


class QualityScorer:
    def __init__(self, embedder=None, classifier=None):
        self.embedder = embedder or SentenceTransformer("sentence-transformers/all-MiniLM-L6-v2")
        self.toxicity_classifier = classifier or pipeline(
            "text-classification",
            model="unitary/toxic-bert",
            top_k=None,
        )

    def score(self, request: ScoreRequest) -> ScoreResult:
        start = time.time()
        flags: list[str] = []

        if not request.response or len(request.response.strip()) < 10:
            flags.append("empty_response")
            return ScoreResult(
                request_id=request.request_id,
                hallucination_score=0.0,
                factual_consistency_score=0.0,
                toxicity_score=0.0,
                overall_quality_score=0.0,
                flags=flags,
                processing_ms=int((time.time() - start) * 1000),
            )

        is_refusal = any(p in request.response for p in _REFUSAL_PHRASES)
        if is_refusal:
            flags.append("refusal")

        # Compute embeddings once; reused for hallucination and factual consistency.
        if request.prompt:
            embs = self.embedder.encode([request.prompt, request.response])
            similarity = float(cosine_similarity(embs[0:1], embs[1:2])[0][0])
            similarity = max(0.0, min(1.0, similarity))
        else:
            similarity = 0.5

        hallucination_score = self._hallucination(request.response, similarity, is_refusal)
        factual_consistency_score = similarity if request.prompt else 0.5

        if hallucination_score > 0.7:
            flags.append("hallucination_risk")
        if factual_consistency_score < 0.4:
            flags.append("low_factual_consistency")

        toxicity_score = self._toxicity(request.response)
        if toxicity_score > 0.5:
            flags.append("toxicity_detected")

        overall = (
            (1.0 - hallucination_score) * 0.4
            + factual_consistency_score * 0.4
            + (1.0 - toxicity_score) * 0.2
        )
        overall = max(0.0, min(1.0, overall))

        processing_ms = int((time.time() - start) * 1000)
        if processing_ms > 2000:
            logger.warning(
                "scoring exceeded 2s threshold: %dms for request %s",
                processing_ms,
                request.request_id,
            )

        return ScoreResult(
            request_id=request.request_id,
            hallucination_score=hallucination_score,
            factual_consistency_score=factual_consistency_score,
            toxicity_score=toxicity_score,
            overall_quality_score=overall,
            flags=flags,
            processing_ms=processing_ms,
        )

    def _hallucination(self, response: str, similarity: float, is_refusal: bool) -> float:
        if is_refusal:
            return 0.1
        has_claims = bool(_CLAIM_PATTERN.search(response))
        if not has_claims:
            return 0.2
        # Claims present but not grounded in prompt → higher hallucination risk.
        return max(0.0, min(1.0, 1.0 - similarity))

    def _toxicity(self, response: str) -> float:
        # Truncate to stay within model input limits.
        results = self.toxicity_classifier(response[:512])
        # Pipeline returns [[{label, score}, ...]] for single string input.
        predictions = results[0] if isinstance(results[0], list) else results
        # unitary/toxic-bert: prefer the "toxic" label; fall back to max score.
        for item in predictions:
            if item["label"].lower() == "toxic":
                return float(item["score"])
        if predictions:
            return float(max(item["score"] for item in predictions))
        return 0.0
