from __future__ import annotations

import asyncio
import json
import re

from app.adapters import call_llm_json
from app.config import settings
from app.models import CuratorDecision, EventWindow
from openai import OpenAI

SYSTEM_PROMPT = """
You are StudyLens curator.
Turn one educational window into structured study notes.

Rules:
- Use transcript as the main content backbone.
- Use visual/audio only to improve quality and screenshot decisions.
- Always produce a concise learner-friendly note if there is any useful signal.
- Never copy-paste raw transcript chunks as the final summary.
- Keep summary to 1-2 concise sentences.
- Keep 3-5 key points, short and non-redundant.
- Return strict JSON with fields:
  should_write, skip_reason, concept_title, summary, key_points, takeaway, confidence,
  visual_evidence{should_capture,reason,target_time_hint},
  audio_signal{used,reason},
  source_grounding{transcript_used,visual_used,audio_used}
"""


def _clip(text: str, max_chars: int) -> str:
    clean = re.sub(r"\s+", " ", (text or "")).strip()
    if len(clean) <= max_chars:
        return clean
    return clean[: max_chars - 3].rstrip() + "..."


def _clip_sentences(text: str, max_sentences: int) -> str:
    parts = re.split(r"(?<=[.!?])\s+", (text or "").strip())
    parts = [p for p in parts if p]
    if not parts:
        return ""
    return " ".join(parts[:max_sentences]).strip()


def _normalize_title(value: str) -> str:
    title = _clip(value, 56)
    if not title:
        return "Untitled Concept"
    return title


def _looks_generic_point(text: str) -> bool:
    low = text.lower().strip()
    bad = (
        "the instructor explains",
        "this section discusses",
        "the speaker talks about",
        "the content is about",
    )
    return any(low.startswith(p) for p in bad)


def _deterministic_fallback(window: EventWindow) -> CuratorDecision:
    transcript_lines = [line.strip() for line in window.transcript_text.split("\n") if line.strip()]
    visual_lines = [line.strip() for line in window.visual_text.split("\n") if line.strip()]
    audio_lines = [line.strip() for line in window.audio_text.split("\n") if line.strip()]

    # De-duplicate while preserving order.
    transcript_lines = list(dict.fromkeys(transcript_lines))
    visual_lines = list(dict.fromkeys(visual_lines))
    audio_lines = list(dict.fromkeys(audio_lines))

    if not transcript_lines and not visual_lines and not audio_lines:
        return CuratorDecision(should_write=False, skip_reason="empty_window", confidence=0.0)

    seed = transcript_lines[0] if transcript_lines else (visual_lines[0] if visual_lines else audio_lines[0])
    title_words = [w for w in _clip(seed, 80).split() if w]
    concept_title = _normalize_title(" ".join(title_words[:8]))

    # Keep fallback abstractive and short (not transcript copy-paste).
    summary = _clip(
        f"This segment explains {concept_title.lower()} and why it matters in the current lesson.",
        220,
    )

    key_points: list[str] = []
    if transcript_lines:
        key_points.append(_clip(f"Core explanation focuses on {concept_title.lower()}.", 110))
        key_points.append(_clip("The instructor gives an iterative or step-wise reasoning path.", 110))
    if visual_lines:
        key_points.append(_clip("Visual material reinforces the explanation with on-screen evidence.", 110))
    if audio_lines and len(key_points) < 3:
        key_points.append(_clip("Audio cues indicate emphasis on practical understanding.", 110))
    if len(key_points) < 3:
        key_points.append(_clip("Key details are presented as concise learning points in this window.", 110))

    takeaway = _clip(
        f"Takeaway: {concept_title.lower()} is presented as a practical concept to apply, not just memorize.",
        180,
    )

    return CuratorDecision(
        should_write=True,
        skip_reason=None,
        concept_title=concept_title,
        summary=summary,
        key_points=key_points,
        takeaway=takeaway,
        confidence=0.52,
        visual_evidence={
            "should_capture": bool(visual_lines),
            "reason": _clip(visual_lines[0], 180) if visual_lines else None,
            "target_time_hint": "middle",
        },
        audio_signal={
            "used": bool(audio_lines),
            "reason": _clip(audio_lines[0], 180) if audio_lines else None,
        },
        source_grounding={
            "transcript_used": bool(transcript_lines),
            "visual_used": bool(visual_lines),
            "audio_used": bool(audio_lines),
        },
    )


def _polish_decision(decision: CuratorDecision, window: EventWindow, previous_title: str) -> CuratorDecision:
    # Product mode: always write when any signal exists.
    decision.should_write = True
    decision.skip_reason = None
    if decision.confidence < 0.35:
        decision.confidence = 0.35

    decision.concept_title = _normalize_title(decision.concept_title)

    decision.summary = _clip(_clip_sentences(decision.summary, max_sentences=2), 320)
    decision.takeaway = _clip(_clip_sentences(decision.takeaway, max_sentences=1), 220)

    cleaned_points: list[str] = []
    seen = set()
    for point in decision.key_points:
        p = _clip(point, 120)
        if not p or _looks_generic_point(p):
            continue
        key = p.lower()
        if key in seen:
            continue
        seen.add(key)
        cleaned_points.append(p)
        if len(cleaned_points) >= 4:
            break
    decision.key_points = cleaned_points

    visual_text_present = bool(window.visual_text.strip())
    if visual_text_present and not decision.visual_evidence.should_capture:
        # Deterministic upgrade from test findings: if strong visual exists, capture.
        decision.visual_evidence.should_capture = True
        if not decision.visual_evidence.reason:
            decision.visual_evidence.reason = "Window contains relevant visual learning material."

    if decision.visual_evidence.reason:
        decision.visual_evidence.reason = _clip(_clip_sentences(decision.visual_evidence.reason, 1), 180)
    if decision.audio_signal.reason:
        decision.audio_signal.reason = _clip(_clip_sentences(decision.audio_signal.reason, 1), 180)

    if not decision.summary and not decision.key_points and not decision.takeaway:
        # Let caller use deterministic fallback for empty/invalid model output.
        decision.should_write = False
        decision.skip_reason = "empty_window"

    return decision


def _build_prompt(window: EventWindow, previous_title: str) -> str:
    return f"""
WINDOW: {window.start_time:.0f}s-{window.end_time:.0f}s
PREVIOUS_TITLE: {previous_title or "(none)"}

TRANSCRIPT:
{window.transcript_text or "(none)"}

VISUAL:
{window.visual_text or "(none)"}

AUDIO:
{window.audio_text or "(none)"}
"""


def _call_structured_curator_sync(prompt: str, model: str) -> CuratorDecision:
    kwargs: dict[str, str] = {"api_key": settings.llm_api_key}
    if settings.llm_base_url:
        kwargs["base_url"] = settings.llm_base_url
    client = OpenAI(**kwargs)
    response = client.responses.parse(
        model=model,
        input=[
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": prompt},
        ],
        text_format=CuratorDecision,
        temperature=0.1,
    )
    parsed = response.output_parsed
    if parsed is None:
        raise ValueError("No structured curator output returned")
    return parsed


def _curator_models() -> list[str]:
    models = [settings.curator_model, settings.curator_fallback_model]
    # Qwen/Gemma ids only work via sandbox gateway; without sandbox use OpenAI models.
    if not settings.use_sandbox:
        for model in ("gpt-4.1-mini", "gpt-4o-mini"):
            if model not in models:
                models.append(model)
    # De-dupe while preserving order.
    seen: set[str] = set()
    out: list[str] = []
    for model in models:
        if model and model not in seen:
            seen.add(model)
            out.append(model)
    return out


async def curate_window(window: EventWindow, previous_title: str = "") -> CuratorDecision:
    prompt = _build_prompt(window, previous_title)
    llm_errors: list[str] = []
    for model in _curator_models():
        try:
            decision = await asyncio.to_thread(_call_structured_curator_sync, prompt, model)
            decision.window_id = window.window_id
            decision.window_start = window.start_time
            decision.window_end = window.end_time
            polished = _polish_decision(decision, window, previous_title)
            if polished.should_write:
                return polished
            break
        except Exception as structured_exc:
            llm_errors.append(f"{model} structured: {structured_exc}")

        try:
            raw = await call_llm_json(SYSTEM_PROMPT, prompt, model=model)
            payload = json.loads(raw)
            decision = CuratorDecision.model_validate(payload)
            decision.window_id = window.window_id
            decision.window_start = window.start_time
            decision.window_end = window.end_time
            polished = _polish_decision(decision, window, previous_title)
            if polished.should_write:
                return polished
            break
        except Exception as json_exc:
            llm_errors.append(f"{model} json: {json_exc}")
            continue

    # LLM unavailable/invalid: fall back to deterministic structured notes.
    fallback = _deterministic_fallback(window)
    fallback.window_id = window.window_id
    fallback.window_start = window.start_time
    fallback.window_end = window.end_time
    if not fallback.should_write and llm_errors:
        fallback.summary = f"Curator unavailable: {' | '.join(llm_errors)[:220]}"
    return fallback
