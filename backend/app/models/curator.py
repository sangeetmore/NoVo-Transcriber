from __future__ import annotations

from typing import Literal

from pydantic import BaseModel, Field


class VisualEvidence(BaseModel):
    should_capture: bool = False
    reason: str | None = None
    target_time_hint: Literal["start", "middle", "end"] = "middle"


class AudioSignal(BaseModel):
    used: bool = False
    reason: str | None = None


class SourceGrounding(BaseModel):
    transcript_used: bool = False
    visual_used: bool = False
    audio_used: bool = False


class CuratorDecision(BaseModel):
    should_write: bool = True
    skip_reason: str | None = None

    concept_title: str = ""
    summary: str = ""
    key_points: list[str] = Field(default_factory=list)
    takeaway: str = ""
    confidence: float = 0.0

    visual_evidence: VisualEvidence = Field(default_factory=VisualEvidence)
    audio_signal: AudioSignal = Field(default_factory=AudioSignal)
    source_grounding: SourceGrounding = Field(default_factory=SourceGrounding)

    window_id: int = 0
    window_start: float = 0.0
    window_end: float = 0.0
