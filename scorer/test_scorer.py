import numpy as np
import pytest
from unittest.mock import MagicMock, patch

from models import ScoreRequest, ScoreResult
from scorer import QualityScorer

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

def _similar_embs() -> np.ndarray:
    """Two identical unit vectors → cosine similarity = 1.0."""
    return np.array([[1.0, 0.0, 0.0], [1.0, 0.0, 0.0]])


def _orthogonal_embs() -> np.ndarray:
    """Two orthogonal unit vectors → cosine similarity = 0.0."""
    return np.array([[1.0, 0.0, 0.0], [0.0, 1.0, 0.0]])


def _make_scorer(embeddings: np.ndarray, toxicity_score: float = 0.05) -> QualityScorer:
    mock_embedder = MagicMock()
    mock_embedder.encode.return_value = embeddings

    mock_classifier = MagicMock()
    mock_classifier.return_value = [[
        {"label": "non-toxic", "score": round(1.0 - toxicity_score, 4)},
        {"label": "toxic", "score": round(toxicity_score, 4)},
    ]]

    return QualityScorer(embedder=mock_embedder, classifier=mock_classifier)


_BASE_REQUEST = ScoreRequest(
    request_id="req-test",
    prompt="What is the capital of France?",
    response="The capital of France is Paris, located in northern France.",
    model="gpt-4o",
    feature_name="chat",
)

# ---------------------------------------------------------------------------
# Flag tests
# ---------------------------------------------------------------------------

class TestFlags:
    def test_empty_response_flag_on_blank(self):
        s = _make_scorer(_similar_embs())
        req = ScoreRequest(request_id="r", prompt="hi", response="", model="m", feature_name="f")
        assert "empty_response" in s.score(req).flags

    def test_empty_response_flag_on_short(self):
        s = _make_scorer(_similar_embs())
        req = ScoreRequest(request_id="r", prompt="hi", response="ok", model="m", feature_name="f")
        assert "empty_response" in s.score(req).flags

    def test_refusal_flag_i_cannot(self):
        s = _make_scorer(_similar_embs())
        req = ScoreRequest(
            request_id="r", prompt="hi",
            response="I cannot help with that request at all.",
            model="m", feature_name="f",
        )
        assert "refusal" in s.score(req).flags

    def test_refusal_flag_as_an_ai(self):
        s = _make_scorer(_similar_embs())
        req = ScoreRequest(
            request_id="r", prompt="hi",
            response="as an AI I am not permitted to answer that question.",
            model="m", feature_name="f",
        )
        assert "refusal" in s.score(req).flags

    def test_hallucination_risk_when_low_similarity(self):
        # Orthogonal → similarity=0 → hallucination=1.0 > 0.7
        s = _make_scorer(_orthogonal_embs())
        req = ScoreRequest(
            request_id="r", prompt="Tell me about history.",
            response="The battle happened in 1492 and the general was Napoleon.",
            model="m", feature_name="f",
        )
        assert "hallucination_risk" in s.score(req).flags

    def test_no_hallucination_risk_when_high_similarity(self):
        # Identical → similarity=1.0 → hallucination=0.0
        s = _make_scorer(_similar_embs())
        req = ScoreRequest(
            request_id="r", prompt="Tell me about the year 1492.",
            response="The battle happened in 1492 and the general was Napoleon.",
            model="m", feature_name="f",
        )
        assert "hallucination_risk" not in s.score(req).flags

    def test_low_factual_consistency_flag(self):
        # Orthogonal → similarity=0.0 < 0.4
        s = _make_scorer(_orthogonal_embs())
        assert "low_factual_consistency" in s.score(_BASE_REQUEST).flags

    def test_no_low_factual_consistency_when_similar(self):
        s = _make_scorer(_similar_embs())
        assert "low_factual_consistency" not in s.score(_BASE_REQUEST).flags

    def test_toxicity_detected_flag(self):
        s = _make_scorer(_similar_embs(), toxicity_score=0.8)
        assert "toxicity_detected" in s.score(_BASE_REQUEST).flags

    def test_no_toxicity_flag_when_clean(self):
        s = _make_scorer(_similar_embs(), toxicity_score=0.1)
        assert "toxicity_detected" not in s.score(_BASE_REQUEST).flags

# ---------------------------------------------------------------------------
# Overall quality formula
# ---------------------------------------------------------------------------

class TestOverallQuality:
    def test_formula_matches_components(self):
        s = _make_scorer(_similar_embs(), toxicity_score=0.1)
        result = s.score(_BASE_REQUEST)
        expected = (
            (1 - result.hallucination_score) * 0.4
            + result.factual_consistency_score * 0.4
            + (1 - result.toxicity_score) * 0.2
        )
        assert abs(result.overall_quality_score - expected) < 1e-6

    def test_overall_score_bounded_between_0_and_1(self):
        s = _make_scorer(_orthogonal_embs(), toxicity_score=0.99)
        result = s.score(_BASE_REQUEST)
        assert 0.0 <= result.overall_quality_score <= 1.0

    def test_high_quality_when_similar_and_clean(self):
        s = _make_scorer(_similar_embs(), toxicity_score=0.01)
        result = s.score(_BASE_REQUEST)
        # similarity=1.0: hallucination=0, factual=1.0, toxicity≈0 → overall≈1.0
        assert result.overall_quality_score > 0.9

# ---------------------------------------------------------------------------
# Edge cases
# ---------------------------------------------------------------------------

class TestEdgeCases:
    def test_empty_response_returns_all_zero_scores(self):
        s = _make_scorer(_similar_embs())
        req = ScoreRequest(request_id="r", prompt="hi", response="", model="m", feature_name="f")
        result = s.score(req)
        assert result.hallucination_score == 0.0
        assert result.factual_consistency_score == 0.0
        assert result.toxicity_score == 0.0
        assert result.overall_quality_score == 0.0

    def test_very_long_response_does_not_crash(self):
        s = _make_scorer(_similar_embs())
        long_response = "This is a sentence about facts and the year 2000. " * 500
        req = ScoreRequest(
            request_id="r", prompt="Tell me something.",
            response=long_response, model="m", feature_name="f",
        )
        result = s.score(req)
        assert isinstance(result.overall_quality_score, float)
        assert 0.0 <= result.overall_quality_score <= 1.0

    def test_no_prompt_does_not_crash(self):
        mock_embedder = MagicMock()
        mock_embedder.encode.return_value = np.array([[1.0, 0.0, 0.0]])
        mock_classifier = MagicMock()
        mock_classifier.return_value = [[{"label": "non-toxic", "score": 0.95}, {"label": "toxic", "score": 0.05}]]
        s = QualityScorer(embedder=mock_embedder, classifier=mock_classifier)
        req = ScoreRequest(request_id="r", prompt="", response="Some valid response text here.", model="m", feature_name="f")
        result = s.score(req)
        assert isinstance(result.overall_quality_score, float)

    def test_request_id_preserved_in_result(self):
        s = _make_scorer(_similar_embs())
        result = s.score(_BASE_REQUEST)
        assert result.request_id == _BASE_REQUEST.request_id

    def test_processing_ms_is_non_negative(self):
        s = _make_scorer(_similar_embs())
        result = s.score(_BASE_REQUEST)
        assert result.processing_ms >= 0

# ---------------------------------------------------------------------------
# Health endpoint
# ---------------------------------------------------------------------------

class TestHealthEndpoint:
    def test_health_returns_models_loaded_true(self):
        from fastapi.testclient import TestClient
        from main import app

        mock_scorer = MagicMock()
        with patch("main.QualityScorer", return_value=mock_scorer):
            with TestClient(app) as client:
                resp = client.get("/health")
        assert resp.status_code == 200
        data = resp.json()
        assert data["status"] == "ok"
        assert data["models_loaded"] is True
