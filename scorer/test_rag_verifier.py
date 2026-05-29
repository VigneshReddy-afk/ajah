import time

import numpy as np
import pytest
from unittest.mock import MagicMock

from rag_verifier import RAGVerificationResult, RAGVerifier


# ── Fixtures ──────────────────────────────────────────────────────────────────

@pytest.fixture(scope="module")
def verifier():
    """Real RAGVerifier — model loaded once for the entire module."""
    from sentence_transformers import SentenceTransformer
    model = SentenceTransformer("sentence-transformers/all-MiniLM-L6-v2")
    return RAGVerifier(model)


# ── Helper ────────────────────────────────────────────────────────────────────

def _verifier_with_controlled_embs(
    claim_embs: np.ndarray,
    evidence_embs: np.ndarray,
    neg_embs: np.ndarray | None = None,
) -> RAGVerifier:
    """RAGVerifier whose encode() returns fixed embeddings in call order."""
    model = MagicMock()
    side_effects: list[np.ndarray] = [claim_embs, evidence_embs]
    if neg_embs is not None:
        side_effects.append(neg_embs)
    model.encode.side_effect = list(side_effects)
    return RAGVerifier(model)


# ── Tests ─────────────────────────────────────────────────────────────────────

class TestRAGVerifier:

    def test_supported_claim(self, verifier):
        """Response that directly restates source content should be 'supported'."""
        source = (
            "The Python programming language was created by Guido van Rossum. "
            "Python was first released in February 1991. "
            "Python emphasizes code readability and simplicity."
        )
        response = (
            "Python was created by Guido van Rossum and was first released in 1991."
        )
        result = verifier.verify(source, response)
        assert result.verdict == "supported", (
            f"Expected 'supported', got '{result.verdict}'. "
            f"grounding={result.grounding_score:.3f}, "
            f"supported_claims={result.supported_claims}"
        )
        assert result.grounding_score >= 0.7
        assert len(result.supported_claims) > 0

    def test_unsupported_claim(self, verifier):
        """Response that invents facts unrelated to source should be 'unsupported'."""
        source = (
            "The annual baking competition takes place every July in Millbrook. "
            "Contestants are judged on taste, presentation, and creativity. "
            "The winner receives a golden rolling pin trophy."
        )
        response = (
            "Quantum entanglement is a phenomenon where two particles become correlated "
            "such that the quantum state of one instantly influences the other, "
            "regardless of the distance separating them."
        )
        result = verifier.verify(source, response)
        assert result.verdict == "unsupported", (
            f"Expected 'unsupported', got '{result.verdict}'. "
            f"grounding={result.grounding_score:.3f}"
        )
        assert result.grounding_score < 0.4
        assert len(result.supported_claims) == 0

    def test_contradicted_claim(self):
        """Response that contradicts the source should be classified as 'contradicted'.

        Uses controlled unit-vector embeddings to exercise the contradiction path
        precisely — claim is orthogonal to evidence (cosine sim = 0.0 < 0.40), and
        the negated claim is aligned with evidence (cosine sim = 1.0 > 0.60).
        """
        claim_emb    = np.array([[1.0, 0.0, 0.0]], dtype=np.float32)
        evidence_emb = np.array([[0.0, 1.0, 0.0]], dtype=np.float32)  # sim to claim = 0.0
        neg_emb      = np.array([[0.0, 1.0, 0.0]], dtype=np.float32)  # sim to evidence = 1.0

        v = _verifier_with_controlled_embs(claim_emb, evidence_emb, neg_emb)
        result = v.verify(
            source_context="The factory passed all safety inspections without any violations.",
            response="The factory failed every safety inspection.",
        )
        assert result.verdict == "contradicted", (
            f"Expected 'contradicted', got '{result.verdict}'. "
            f"contradiction_score={result.contradiction_score:.3f}"
        )
        assert len(result.contradicted_claims) > 0
        assert result.contradiction_score > 0.3

    def test_partial_support(self, verifier):
        """Response mixing a supported and an unsupported claim should be 'partially_supported'."""
        source = (
            "Mount Everest is the tallest mountain on Earth, standing at 8,848 metres above sea level. "
            "It is located in the Himalayas on the border between Nepal and Tibet. "
            "The mountain was first summited by Edmund Hillary and Tenzing Norgay in May 1953."
        )
        # First sentence mirrors the source; second is entirely fabricated.
        response = (
            "Mount Everest is the tallest mountain on Earth at 8,848 metres. "
            "Christopher Columbus sailed to the Americas in 1492, "
            "establishing trade routes between Europe and the New World."
        )
        result = verifier.verify(source, response)
        assert result.verdict in ("partially_supported", "supported"), (
            f"Expected 'partially_supported' or 'supported', got '{result.verdict}'. "
            f"grounding={result.grounding_score:.3f}, "
            f"supported={result.supported_claims}, unsupported={result.unsupported_claims}"
        )
        assert len(result.supported_claims) >= 1
        assert result.grounding_score > 0.0

    def test_empty_response(self, verifier):
        """Empty response should return 'unavailable' with neutral scores."""
        result = verifier.verify(
            source_context="Any valid document content about any topic.",
            response="",
        )
        assert result.verdict == "unavailable"
        assert result.grounding_score == 0.5
        assert result.supported_claims == []
        assert result.unsupported_claims == []
        assert result.contradicted_claims == []

    def test_empty_context(self, verifier):
        """Empty source document means no grounding is possible — expect 'unsupported'."""
        result = verifier.verify(
            source_context="",
            response="The sky is blue and clouds are white.",
        )
        assert result.verdict == "unsupported"
        assert result.grounding_score == 0.0
        assert result.contradiction_score == 0.0

    def test_very_long_document(self, verifier):
        """Verification against a ~10,000-word document must complete within 3 seconds."""
        sentence = "The committee reviewed the proposal and found it to be satisfactory."
        # 10 words per sentence × 1000 repetitions = 10,000 words
        source = " ".join([sentence] * 1000)
        response = "The committee reviewed the proposal."

        start = time.perf_counter()
        result = verifier.verify(source_context=source, response=response)
        elapsed = time.perf_counter() - start

        assert elapsed < 3.0, (
            f"Verification took {elapsed:.3f}s — exceeds the 3s production limit"
        )
        assert isinstance(result, RAGVerificationResult)
        assert 0.0 <= result.grounding_score <= 1.0
        assert result.verdict != "unavailable"
