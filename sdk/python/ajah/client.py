"""
AjahClient — main entry point for the
Ajah Python SDK.
"""

from __future__ import annotations

import uuid
from typing import Any, Dict, List, Optional
from openai import OpenAI
from .session import AjahSession


class AjahClient:
    """
    Python client for the Ajah LLM
    observability gateway.

    Wraps the OpenAI SDK and automatically
    routes all calls through your Ajah
    gateway with full observability.

    Example:
        client = AjahClient(
            gateway_url="http://localhost:8080",
            api_key="your-groq-key",
            feature_name="my-app",
        )
        response = client.chat(
            model="llama-3.3-70b-versatile",
            messages=[{"role": "user",
                       "content": "Hello"}],
        )
    """

    def __init__(
        self,
        gateway_url: str = "http://localhost:8080",
        api_key: str = "",
        feature_name: str = "default",
        user_id: str = "anonymous",
    ) -> None:
        self.gateway_url = gateway_url.rstrip("/")
        self.api_key = api_key
        self.feature_name = feature_name
        self.user_id = user_id
        self._openai = OpenAI(
            base_url=f"{self.gateway_url}/v1",
            api_key=api_key,
        )

    def _base_headers(
        self,
        session_id: Optional[str] = None,
        step_name: Optional[str] = None,
    ) -> Dict[str, str]:
        headers = {
            "X-Feature-Name": self.feature_name,
            "X-User-ID": self.user_id,
        }
        if session_id:
            headers["X-Session-ID"] = session_id
        if step_name:
            headers["X-Agent-Step"] = step_name
        return headers

    def chat(
        self,
        model: str,
        messages: List[Dict[str, str]],
        step_name: Optional[str] = None,
        session_id: Optional[str] = None,
        **kwargs: Any,
    ) -> Any:
        """
        Send a chat completion request
        through the Ajah gateway.
        """
        return self._openai.chat.completions.create(
            model=model,
            messages=messages,
            extra_headers=self._base_headers(
                session_id=session_id,
                step_name=step_name,
            ),
            **kwargs,
        )

    def session(
        self,
        session_id: Optional[str] = None,
    ) -> AjahSession:
        """
        Create a new session for multi-turn
        agent tracking.
        """
        return AjahSession(
            client=self,
            session_id=session_id or
                str(uuid.uuid4()),
        )

    @property
    def dashboard_url(self) -> str:
        """URL to the Ajah dashboard."""
        return self.gateway_url.replace(
            ":8080", ":3000")
