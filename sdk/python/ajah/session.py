"""
AjahSession — multi-turn session tracking.
"""

from __future__ import annotations

import uuid
from typing import Any, Dict, List, Optional, TYPE_CHECKING

if TYPE_CHECKING:
    from .client import AjahClient


class AjahSession:
    """
    Tracks a multi-turn LLM session through
    Ajah with automatic step numbering and
    session ID injection.

    Use as a context manager:
        with client.session() as session:
            r = session.chat(
                model="...",
                messages=[...],
                step_name="step-1",
            )
            print(session.dashboard_url)
    """

    def __init__(
        self,
        client: AjahClient,
        session_id: Optional[str] = None,
    ) -> None:
        self._client = client
        self.session_id = session_id or str(
            uuid.uuid4())
        self._step_counter = 0

    def _next_step(
        self,
        step_name: Optional[str] = None,
    ) -> str:
        self._step_counter += 1
        return step_name or (
            f"step-{self._step_counter}")

    def chat(
        self,
        model: str,
        messages: List[Dict[str, str]],
        step_name: Optional[str] = None,
        **kwargs: Any,
    ) -> Any:
        """
        Send a chat completion within
        this session.
        """
        return self._client.chat(
            model=model,
            messages=messages,
            session_id=self.session_id,
            step_name=self._next_step(
                step_name),
            **kwargs,
        )

    @property
    def dashboard_url(self) -> str:
        """
        URL to view this session in the
        Ajah dashboard.
        """
        base = self._client.gateway_url.replace(
            ":8080", ":3000")
        return f"{base}/sessions/{self.session_id}"

    def __enter__(self) -> AjahSession:
        return self

    def __exit__(self, *args: Any) -> None:
        pass
