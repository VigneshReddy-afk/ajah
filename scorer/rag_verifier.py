import logging
import time
from dataclasses import dataclass

import nltk
import numpy as np
from sklearn.metrics.pairwise import cosine_similarity

logger = logging.getLogger(__name__)

_SUPPORT_THRESHOLD = 0.75
_UNCERTAIN_THRESHOLD = 0.40
_CONTRADICTION_THRESHOLD = 0.60
_WARN_SECONDS = 3.0


@dataclass
class RAGVerificationResult:
    supported_claims: list[str]
    unsupported_claims: list[str]
    contradicted_claims: list[str]
    grounding_score: float
    contradiction_score: float
    verdict: str
    confidence: float


class RAGVerifier:
    def __init__(self, model):
        self.model = model
        # nltk.download is idempotent — skips silently if already installed.
        # Calling directly avoids nltk.data.find()'s OSError on punkt/PY3_tab
        # in NLTK 3.8.1 when the punkt directory exists but lacks PY3_tab format.
        nltk.download("punkt", quiet=True)
        nltk.download("punkt_tab", quiet=True)

    def verify(self, source_context: str, response: str) -> RAGVerificationResult:
        start = time.perf_counter()
        try:
            result = self._run(source_context, response)
        except Exception:
            logger.exception("RAGVerifier: unhandled error; returning safe default")
            return RAGVerificationResult(
                supported_claims=[],
                unsupported_claims=[],
                contradicted_claims=[],
                grounding_score=0.5,
                contradiction_score=0.0,
                verdict="unavailable",
                confidence=0.5,
            )
        elapsed = time.perf_counter() - start
        if elapsed > _WARN_SECONDS:
            logger.warning(
                "RAGVerifier exceeded %.0fs threshold: %.3fs", _WARN_SECONDS, elapsed
            )
        return result

    def _run(self, source_context: str, response: str) -> RAGVerificationResult:
        # Empty response — nothing to verify
        if not response or not response.strip():
            return RAGVerificationResult(
                supported_claims=[],
                unsupported_claims=[],
                contradicted_claims=[],
                grounding_score=0.5,
                contradiction_score=0.0,
                verdict="unavailable",
                confidence=0.5,
            )

        # Empty source — nothing to ground against
        if not source_context or not source_context.strip():
            claims = [s for s in nltk.sent_tokenize(response) if len(s.strip()) >= 10]
            return RAGVerificationResult(
                supported_claims=[],
                unsupported_claims=claims,
                contradicted_claims=[],
                grounding_score=0.0,
                contradiction_score=0.0,
                verdict="unsupported",
                confidence=0.0,
            )

        # Step 1 — split response into claims
        claims = [s for s in nltk.sent_tokenize(response) if len(s.strip()) >= 10]
        if not claims:
            return RAGVerificationResult(
                supported_claims=[],
                unsupported_claims=[],
                contradicted_claims=[],
                grounding_score=0.5,
                contradiction_score=0.0,
                verdict="unavailable",
                confidence=0.5,
            )

        # Step 2 — overlapping chunks of 3 source sentences with 1-sentence overlap (step=2)
        src_sentences = nltk.sent_tokenize(source_context)
        chunks: list[str] = []
        for i in range(0, len(src_sentences), 2):
            group = src_sentences[i : i + 3]
            if group:
                chunks.append(" ".join(group))
        if not chunks:
            chunks = [source_context]

        # Step 3 — encode claims and evidence once
        claim_embs = self.model.encode(claims)
        evidence_embs = self.model.encode(chunks)

        # Steps 4 & 5 — classify each claim
        supported: list[str] = []
        unsupported: list[str] = []
        contradicted: list[str] = []

        for i, claim in enumerate(claims):
            sims = cosine_similarity(claim_embs[i : i + 1], evidence_embs)[0]
            best = float(np.max(sims))

            if best >= _SUPPORT_THRESHOLD:
                supported.append(claim)
            elif best >= _UNCERTAIN_THRESHOLD:
                # Uncertain — not clearly grounded; count as unsupported for scoring
                unsupported.append(claim)
            else:
                # Low similarity — test negation for contradiction signal
                neg_emb = self.model.encode([f"It is false that {claim}"])
                neg_sims = cosine_similarity(neg_emb, evidence_embs)[0]
                if float(np.max(neg_sims)) > _CONTRADICTION_THRESHOLD:
                    contradicted.append(claim)
                else:
                    unsupported.append(claim)

        # Step 6 — aggregate scores
        total = len(claims)
        grounding_score = len(supported) / total if total > 0 else 0.5
        contradiction_score = len(contradicted) / total if total > 0 else 0.0

        # Step 7 — verdict
        if contradiction_score > 0.3:
            verdict = "contradicted"
        elif grounding_score >= 0.7:
            verdict = "supported"
        elif grounding_score >= 0.4:
            verdict = "partially_supported"
        else:
            verdict = "unsupported"

        confidence = grounding_score * (1.0 - contradiction_score)

        return RAGVerificationResult(
            supported_claims=supported,
            unsupported_claims=unsupported,
            contradicted_claims=contradicted,
            grounding_score=grounding_score,
            contradiction_score=contradiction_score,
            verdict=verdict,
            confidence=confidence,
        )
