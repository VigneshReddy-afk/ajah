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

        # Linguistic hedge detection
        hedge = self._hedge_detection_score(
            request.prompt,
            request.response,
        )
        result.hedge_risk = hedge

        if hedge > 0.5:
            if "overconfident_response" not in result.flags:
                result.flags.append("overconfident_response")

        # Narrative drift detection
        if request.session_history:
            drift_score, drift_verdict = self._narrative_drift_score(
                request.response,
                request.session_history,
            )
            result.drift_risk = drift_score
            result.drift_verdict = drift_verdict

            if drift_score > 0.5:
                if "narrative_drift" not in result.flags:
                    result.flags.append("narrative_drift")

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

    def _hedge_detection_score(
        self,
        prompt: str,
        response: str,
    ) -> float:
        import re

        # STEP 1 — Question complexity score
        prompt_words = prompt.split()
        prompt_lower = prompt.lower()

        complexity_count = 0

        # Conditional and uncertainty markers in question
        complexity_terms = [
            'if', 'whether', 'could', 'would', 'might',
            'should', 'unsure', 'wondering', 'think',
            'suppose', 'assume', 'hypothetically',
            'potentially', 'possibly', 'maybe'
        ]
        for term in complexity_terms:
            if re.search(r'\b' + term + r'\b', prompt_lower):
                complexity_count += 1

        # Domain markers — high stakes domains
        domain_terms = [
            'medical', 'medicine', 'diagnosis', 'symptoms',
            'treatment', 'drug', 'medication', 'dosage',
            'legal', 'lawsuit', 'liability', 'contract',
            'financial', 'investment', 'stock', 'trading',
            'safety', 'risk', 'dangerous', 'hazardous',
            'scientific', 'research', 'study', 'evidence'
        ]
        for term in domain_terms:
            if re.search(r'\b' + term + r'\b', prompt_lower):
                complexity_count += 1

        # Multi-part question — multiple question marks
        # or complex conjunctions
        question_marks = prompt.count('?')
        if question_marks > 1:
            complexity_count += question_marks - 1

        # STEP 2 — Response certainty score
        response_words = response.split()
        response_lower = response.lower()

        certainty_count = 0

        # Absolute certainty terms
        absolute_terms = [
            'definitely', 'certainly', 'absolutely',
            'guaranteed', 'undoubtedly', 'without question',
            '100%', 'always', 'never', 'proven',
            'unquestionably', 'indisputably', 'clearly',
            'obviously', 'of course', 'there is no doubt',
            'it is certain', 'it is a fact'
        ]
        for term in absolute_terms:
            if term in response_lower:
                certainty_count += 1

        # Absence of hedging language reduces risk
        # Check if response contains appropriate hedges
        hedge_terms = [
            'may', 'might', 'could', 'possibly', 'perhaps',
            'generally', 'typically', 'in most cases',
            'it depends', 'usually', 'often', 'sometimes',
            'consult', 'recommend', 'suggest', 'consider',
            'in general', 'as a rule', 'tend to'
        ]
        hedge_present = any(
            term in response_lower for term in hedge_terms
        )

        # If hedging language is present, reduce certainty
        if hedge_present:
            certainty_count = max(0, certainty_count - 1)

        # STEP 3 — Formula
        # Normalize both scores by word count
        prompt_word_count = max(len(prompt_words), 1)
        response_word_count = max(len(response_words), 1)

        complexity_score = min(
            (complexity_count / prompt_word_count) * 10, 1.0
        )
        certainty_score = min(
            (certainty_count / response_word_count) * 10, 1.0
        )

        # hedge_risk = certainty * complexity
        # High certainty on complex question = high risk
        hedge_risk = round(
            min(certainty_score * complexity_score * 10, 1.0),
            4
        )

        return hedge_risk

    def _extract_claims(self, text: str) -> list[str]:
        import re
        claims = []
        sentences = re.split(r'[.!?]+', text)

        for sentence in sentences:
            sentence = sentence.strip()
            if len(sentence) < 10:
                continue

            if sentence.endswith('?'):
                continue

            words = sentence.split()
            has_proper_noun = any(
                len(w) >= 4 and w[0].isupper() and i > 0
                for i, w in enumerate(words)
            )
            has_number = bool(re.search(r'\b\d+\b', sentence))
            has_absolute = any(
                term in sentence.lower()
                for term in [
                    'always', 'never', 'definitely',
                    'certainly', 'best', 'worst',
                    'superior', 'inferior', 'faster',
                    'slower', 'better', 'worse',
                ]
            )

            if has_proper_noun or has_number or has_absolute:
                claims.append(sentence[:200])

        return claims[:10]

    def _narrative_drift_score(
        self,
        current_response: str,
        session_history: list,
    ) -> tuple[float, str]:
        if not session_history or len(session_history) < 2:
            return 0.0, "not_applicable"

        earlier_turns = [
            t for t in session_history
            if t.turn_index < len(session_history) - 1
        ]
        if not earlier_turns:
            return 0.0, "not_applicable"

        current_claims = self._extract_claims(current_response)
        if not current_claims:
            return 0.0, "not_applicable"

        max_contradiction_score = 0.0

        for turn in earlier_turns:
            earlier_claims = self._extract_claims(turn.response_text)
            if not earlier_claims:
                continue

            try:
                current_embeddings = self.embedder.encode(
                    current_claims, convert_to_tensor=True)
                earlier_embeddings = self.embedder.encode(
                    earlier_claims, convert_to_tensor=True)

                from sentence_transformers import util as st_util
                similarity_matrix = st_util.cos_sim(
                    current_embeddings, earlier_embeddings)

                negation_terms = [
                    'not', 'never', 'no ', 'cannot',
                    'wrong', 'incorrect', 'false',
                    'actually', 'however', 'but ',
                    'contrary', 'opposite', 'instead',
                ]

                for i, curr_claim in enumerate(current_claims):
                    for j in range(len(earlier_claims)):
                        sim = float(similarity_matrix[i][j])
                        if sim > 0.6:
                            curr_lower = curr_claim.lower()
                            has_negation = any(
                                t in curr_lower for t in negation_terms)
                            if has_negation:
                                contradiction_score = sim * 0.8
                                max_contradiction_score = max(
                                    max_contradiction_score,
                                    contradiction_score,
                                )
            except Exception:
                continue

        if max_contradiction_score > 0.5:
            return round(max_contradiction_score, 4), "drift_detected"
        elif max_contradiction_score > 0.3:
            return round(max_contradiction_score, 4), "possible_drift"
        else:
            return round(max_contradiction_score, 4), "stable"

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
