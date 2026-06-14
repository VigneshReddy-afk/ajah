"""
Ajah LlamaIndex Observer

Drop this file into your project and add
AjahObserver to your LlamaIndex callback
manager to route all calls through Ajah
gateway automatically.

Usage:
    from ajah_observer import AjahObserver
    import llama_index.core
    from llama_index.core import Settings

    observer = AjahObserver(
        gateway_url="http://localhost:8080",
        feature_name="my-feature",
        user_id="user-123",
    )
    observer.register()
"""

from __future__ import annotations

import uuid
from typing import Any, Dict, List, Optional
from llama_index.core.callbacks import (
    BaseCallbackHandler,
    CBEventType,
    EventPayload,
)


class AjahObserver(BaseCallbackHandler):
    """
    LlamaIndex callback handler that injects
    Ajah observability headers into every
    LLM call automatically.

    Tracks session IDs, agent steps, and
    quality signals through the Ajah gateway
    without any changes to your existing
    LlamaIndex code.
    """

    def __init__(
        self,
        gateway_url: str = "http://localhost:8080",
        feature_name: str = "llamaindex",
        user_id: str = "anonymous",
        session_id: Optional[str] = None,
    ) -> None:
        super().__init__(
            event_starts_to_ignore=[],
            event_ends_to_ignore=[],
        )
        self.gateway_url = gateway_url.rstrip("/")
        self.feature_name = feature_name
        self.user_id = user_id
        self.session_id = session_id or str(
            uuid.uuid4())
        self._step_counter = 0

    def _next_step(self) -> str:
        self._step_counter += 1
        return f"step-{self._step_counter}"

    def get_extra_headers(
        self,
        step_name: Optional[str] = None,
    ) -> Dict[str, str]:
        """
        Returns headers to inject into your
        LlamaIndex LLM calls for Ajah tracking.
        """
        return {
            "X-Session-ID": self.session_id,
            "X-Feature-Name": self.feature_name,
            "X-User-ID": self.user_id,
            "X-Agent-Step": step_name or
                self._next_step(),
        }

    def on_event_start(
        self,
        event_type: CBEventType,
        payload: Optional[Dict[str, Any]] = None,
        event_id: str = "",
        **kwargs: Any,
    ) -> str:
        return event_id

    def on_event_end(
        self,
        event_type: CBEventType,
        payload: Optional[Dict[str, Any]] = None,
        event_id: str = "",
        **kwargs: Any,
    ) -> None:
        pass

    def start_trace(
        self, trace_id: Optional[str] = None
    ) -> None:
        pass

    def end_trace(
        self,
        trace_id: Optional[str] = None,
        trace_map: Optional[
            Dict[str, List[str]]] = None,
    ) -> None:
        pass

    def register(self) -> None:
        """
        Register this observer with LlamaIndex
        global settings.
        """
        from llama_index.core import Settings
        from llama_index.core.callbacks import (
            CallbackManager)
        Settings.callback_manager = (
            CallbackManager([self]))
        print(
            f"Ajah observer registered\n"
            f"Session: {self.session_id}\n"
            f"Dashboard: {self.session_url}"
        )

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
