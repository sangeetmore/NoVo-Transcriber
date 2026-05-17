from __future__ import annotations

from app.models import Modality, NormalizedEvent

AUDIO_NOISE = (
    "cannot perform this analysis",
    "based on provided text",
    "i do not have access",
    "no access to the audio",
)


def _compact(text: str, max_len: int) -> str:
    clean = " ".join((text or "").split()).strip()
    if len(clean) <= max_len:
        return clean
    return clean[: max_len - 3] + "..."


def normalize_event(raw_event: dict) -> NormalizedEvent | None:
    channel = str(raw_event.get("channel") or "")
    data = raw_event.get("data") or {}

    if channel == "transcript":
        if not data.get("is_final"):
            return None
        text = _compact(str(data.get("text") or ""), 260)
        if not text:
            return None
        return NormalizedEvent(
            timestamp_start=float(data.get("start", 0.0)),
            timestamp_end=float(data.get("end", data.get("start", 0.0))),
            modality=Modality.TRANSCRIPT,
            text=text,
            is_final=True,
            raw_channel=channel,
        )

    if channel in {"scene_index", "visual_index"}:
        text = _compact(str(data.get("text") or ""), 380)
        if not text:
            return None
        return NormalizedEvent(
            timestamp_start=float(data.get("start", 0.0)),
            timestamp_end=float(data.get("end", data.get("start", 0.0))),
            modality=Modality.VISUAL,
            text=text,
            raw_channel=channel,
        )

    if channel == "audio_index":
        text = _compact(str(data.get("text") or ""), 280)
        raw_text = _compact(str(data.get("raw_text") or ""), 260)
        if not text:
            if raw_text:
                # Test-14 fallback: use audio raw transcript when transcript channel is sparse.
                return NormalizedEvent(
                    timestamp_start=float(data.get("start", 0.0)),
                    timestamp_end=float(data.get("end", data.get("start", 0.0))),
                    modality=Modality.TRANSCRIPT,
                    text=raw_text,
                    raw_channel="audio_index_raw_text",
                )
            return None
        low = text.lower()
        if any(p in low for p in AUDIO_NOISE):
            if raw_text:
                return NormalizedEvent(
                    timestamp_start=float(data.get("start", 0.0)),
                    timestamp_end=float(data.get("end", data.get("start", 0.0))),
                    modality=Modality.TRANSCRIPT,
                    text=raw_text,
                    raw_channel="audio_index_raw_text",
                )
            return None
        return NormalizedEvent(
            timestamp_start=float(data.get("start", 0.0)),
            timestamp_end=float(data.get("end", data.get("start", 0.0))),
            modality=Modality.AUDIO,
            text=text,
            raw_channel=channel,
        )

    if channel == "alert":
        label = _compact(str(data.get("label") or ""), 100)
        if not label:
            return None
        return NormalizedEvent(
            timestamp_start=float(data.get("start", 0.0)),
            timestamp_end=float(data.get("end", data.get("start", 0.0))),
            modality=Modality.ALERT,
            text=label,
            confidence=float(data.get("confidence", 1.0)),
            raw_channel=channel,
        )

    return None
