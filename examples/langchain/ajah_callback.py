"""
Ajah LangChain Callback Handler

Drop this file into your project and wrap
your LangChain LLM with AjahCallbackHandler
to route all calls through Ajah gateway
automatically.

Usage:
    from ajah_callback import AjahCallbackHandler

    handler = AjahCallbackHandler(
        gateway_url="http://localhost:8080",
        feature_name="my-feature",
        user_id="user-123",
    )

    llm = ChatOpenAI(
        callbacks=[handler],
        base_url="http://localhost:8080/v1",
        api_key="your-groq-key",
        model="llama-3.3-70b-versatile",
    )
"""

from __future__ import annotations

import uuid
import time
from typing import Any, Dict, List, Optional, Union
from langchain_core.callbacks import BaseCallbackHandler
from langchain_core.messages import BaseMessage
from langchain_core.outputs import LLMResult


class AjahCallbackHandler(BaseCallbackHandler):
    """
    LangChain callback handler that injects
    Ajah observability headers into every
    LLM call automatically.

    Tracks session IDs, agent steps, costs,
    and quality signals through the Ajah
    gateway without any changes to your
    existing LangChain code.
    """

    def __init__(
        self,
        gateway_url: str = "http://localhost:8080",
        feature_name: str = "langchain",
        user_id: str = "anonymous",
        session_id: Optional[str] = None,
    ) -> None:
        super().__init__()
        self.gateway_url = gateway_url.rstrip("/")
        self.feature_name = feature_name
        self.user_id = user_id
        self.session_id = session_id or str(
            uuid.uuid4())
        self._step_counter = 0
        self._start_times: Dict[str, float] = {}

    def _next_step(self) -> str:
        self._step_counter += 1
        return f"step-{self._step_counter}"

    def on_llm_start(
        self,
        serialized: Dict[str, Any],
        prompts: List[str],
        *,
        run_id: uuid.UUID,
        **kwargs: Any,
    ) -> None:
        """Called when LLM starts running."""
        self._start_times[str(run_id)] = time.time()

    def on_chat_model_start(
        self,
        serialized: Dict[str, Any],
        messages: List[List[BaseMessage]],
        *,
        run_id: uuid.UUID,
        **kwargs: Any,
    ) -> None:
        """Called when chat model starts."""
        self._start_times[str(run_id)] = time.time()

    def on_llm_end(
        self,
        response: LLMResult,
        *,
        run_id: uuid.UUID,
        **kwargs: Any,
    ) -> None:
        """Called when LLM finishes."""
        self._start_times.pop(str(run_id), None)

    def on_llm_error(
        self,
        error: Union[Exception, KeyboardInterrupt],
        *,
        run_id: uuid.UUID,
        **kwargs: Any,
    ) -> None:
        """Called when LLM errors."""
        self._start_times.pop(str(run_id), None)

    def get_extra_headers(
        self,
        step_name: Optional[str] = None,
    ) -> Dict[str, str]:
        """
        Returns headers to inject into your
        LangChain LLM calls for Ajah tracking.

        Use with ChatOpenAI model_kwargs or
        extra_headers parameter.
        """
        return {
            "X-Session-ID": self.session_id,
            "X-Feature-Name": self.feature_name,
            "X-User-ID": self.user_id,
            "X-Agent-Step": step_name or
                self._next_step(),
        }

    @property
    def session_url(self) -> str:
        """
        Returns the Ajah dashboard URL
        for this session.
        """
        return (
            f"{self.gateway_url.replace(':8080', ':3000')}"
            f"/sessions/{self.session_id}"
        )
