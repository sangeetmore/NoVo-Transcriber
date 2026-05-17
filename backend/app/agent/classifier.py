from __future__ import annotations

import json

from app.adapters import call_llm_json
from app.config import settings
from app.models import EventWindow


async def classify_session(first_window: EventWindow) -> dict:
    prompt = f"""
Classify educational content from this first window.

TRANSCRIPT:
{first_window.transcript_text or "(none)"}

VISUAL:
{first_window.visual_text or "(none)"}

Return JSON:
{{
  "content_type": "lecture|tutorial|code_walkthrough|conceptual_explainer",
  "topic": "short topic",
  "has_code": true,
  "has_formulas": false,
  "has_diagrams": true,
  "confidence": 0.0
}}
"""
    try:
        raw = await call_llm_json(
            system_prompt="You are a precise educational video classifier.",
            user_prompt=prompt,
            model=settings.tool_planner_model,
        )
        return json.loads(raw)
    except Exception:
        return {
            "content_type": "lecture",
            "topic": "educational session",
            "has_code": False,
            "has_formulas": False,
            "has_diagrams": False,
            "confidence": 0.5,
        }
