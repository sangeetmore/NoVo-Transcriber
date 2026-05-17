from __future__ import annotations

from enum import Enum

from pydantic import BaseModel, Field


class Modality(str, Enum):
    TRANSCRIPT = "transcript"
    VISUAL = "visual"
    AUDIO = "audio"
    ALERT = "alert"
    SYSTEM = "system"


class NormalizedEvent(BaseModel):
    timestamp_start: float
    timestamp_end: float
    modality: Modality
    text: str
    confidence: float = 1.0
    is_final: bool = True
    raw_channel: str = ""


class EventWindow(BaseModel):
    window_id: int
    start_time: float
    end_time: float
    events: list[NormalizedEvent] = Field(default_factory=list)

    @property
    def transcript_text(self) -> str:
        return "\n".join(e.text for e in self.events if e.modality == Modality.TRANSCRIPT)

    @property
    def visual_text(self) -> str:
        return "\n".join(e.text for e in self.events if e.modality == Modality.VISUAL)

    @property
    def audio_text(self) -> str:
        return "\n".join(e.text for e in self.events if e.modality == Modality.AUDIO)

    @property
    def has_content(self) -> bool:
        return bool(self.events)
