import base64
import logging
import re
import time

import numpy as np
from sentence_transformers import SentenceTransformer
from sklearn.metrics.pairwise import cosine_similarity
from transformers import pipeline

from models import ScoreRequest, ScoreResult
from rag_verifier import RAGVerifier

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
        self.rag_verifier = RAGVerifier(self.embedder)

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

        result = ScoreResult(
            request_id=request.request_id,
            hallucination_score=hallucination_score,
            factual_consistency_score=factual_consistency_score,
            toxicity_score=toxicity_score,
            overall_quality_score=overall,
            flags=flags,
            processing_ms=processing_ms,
        )

        if request.source_context:
            try:
                decoded = base64.b64decode(request.source_context).decode("utf-8")
                rag = self.rag_verifier.verify(
                    source_context=decoded,
                    response=request.response,
                )
                result.rag_verdict = rag.verdict
                result.rag_grounding_score = rag.grounding_score
                result.rag_contradiction_score = rag.contradiction_score
                result.rag_supported_claims = rag.supported_claims
                result.rag_unsupported_claims = rag.unsupported_claims
                result.rag_contradicted_claims = rag.contradicted_claims
                if rag.verdict == "contradicted":
                    result.overall_quality_score *= 0.3
                elif rag.verdict == "unsupported":
                    result.overall_quality_score *= 0.6
            except Exception as e:
                logger.warning("RAG verification failed: %s", e)
                result.rag_verdict = "unavailable"

        # Claim density scoring
        has_ctx = bool(request.source_context)
        claim_risk = self._claim_density_score(
            request.prompt,
            request.response,
            has_ctx,
        )
        result.claim_density_risk = claim_risk

        if claim_risk > 0.6:
            if result.hallucination_score < claim_risk * 0.6:
                result.hallucination_score = claim_risk * 0.6
            if "high_claim_density" not in result.flags:
                result.flags.append("high_claim_density")

        return result

    def _hallucination(self, response: str, similarity: float, is_refusal: bool) -> float:
        if is_refusal:
            return 0.1
        has_claims = bool(_CLAIM_PATTERN.search(response))
        if not has_claims:
            return 0.2
        # Claims present but not grounded in prompt → higher hallucination risk.
        return max(0.0, min(1.0, 1.0 - similarity))

    def _claim_density_score(
        self,
        prompt: str,
        response: str,
        has_source_context: bool,
    ) -> float:
        import re

        response_words = response.split()
        claim_count = 0

        claim_count += len(re.findall(r'\b(1[89]\d{2}|20\d{2})\b', response))
        claim_count += len(re.findall(r'\b\d+\.?\d*\s*%', response))
        claim_count += len(re.findall(r'\b(?!1[89]\d{2}|20\d{2})\d{2,}\b', response))

        sentences = re.split(r'[.!?]\s+', response)
        for sentence in sentences:
            words = sentence.split()
            for word in words[1:]:
                clean = re.sub(r'[^A-Za-z]', '', word)
                if len(clean) >= 4 and clean[0].isupper():
                    claim_count += 1

        absolute_terms = [
            'always', 'never', 'definitely',
            'certainly', 'guaranteed', 'proven',
            'definitively', 'undoubtedly',
        ]
        response_lower = response.lower()
        for term in absolute_terms:
            if term in response_lower:
                claim_count += 1

        context_words = len(prompt.split())
        if has_source_context or context_words > 200:
            context_multiplier = 0.2
        elif context_words >= 50:
            context_multiplier = 0.6
        else:
            context_multiplier = 1.0

        response_word_count = max(len(response_words), 1)
        raw_score = (claim_count / response_word_count) * 10
        claim_density_risk = min(raw_score * context_multiplier, 1.0)
        return round(claim_density_risk, 4)

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
