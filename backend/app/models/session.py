from __future__ import annotations

from enum import Enum

from pydantic import BaseModel, Field


class SessionStatus(str, Enum):
    CREATED = "created"
    CAPTURING = "capturing"
    STOPPED = "stopped"
    FAILED = "failed"


class SessionState(BaseModel):
    session_id: str = ""
    status: SessionStatus = SessionStatus.CREATED

    capture_session_id: str = ""
    sandbox_id: str = ""
    display_rtstream_id: str = ""
    audio_rtstream_id: str = ""
    ws_connection_id: str = ""

    notion_page_id: str = ""
    notion_page_url: str = ""

    windows_processed: int = 0
    windows_written: int = 0
    windows_skipped: int = 0
    screenshots_captured: int = 0
    screenshot_failures: int = 0
    events_received: int = 0

    last_concept_title: str = ""
    last_skip_reason: str = ""

    classifier_result: str = ""
    events_designed: list[str] = Field(default_factory=list)
    sandbox_status: str = ""
    visual_model_in_use: str = ""
    audio_model_in_use: str = ""
