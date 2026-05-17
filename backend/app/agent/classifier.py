from __future__ import annotations

import json

from app.adapters import call_llm_json
from app.config import settings
from app.models import EventWindow


async def classify_session(first_window: EventWindow) -> dict:
    prompt = f"""
You receive the first educational video window. Identify:
1. The content type
2. The specific topic being taught

TRANSCRIPT:
{first_window.transcript_text or "(none)"}

VISUAL:
{first_window.visual_text or "(none)"}

Return strict JSON:
{{
  "content_type": "lecture|tutorial|coding_walkthrough|explainer|demo|other",
  "topic": "2 to 6 words"
}}

Rules:
- topic becomes the Notion page title, so make it specific and useful.
- Good: "Neural Networks Chapter 1", "Python Sets and Methods", "React useState Hook".
- Bad: "Programming Tutorial", "Math Video", "Educational Content", "Tech Tutorial".
- If unclear, return topic as an empty string.
- Do not include filler prefixes like "Tutorial:", "Lecture:", "Video about", or "Introduction to".
- Output JSON only.
"""
    try:
        raw = await call_llm_json(
            system_prompt="You are the Note It classifier. Output JSON only.",
            user_prompt=prompt,
            model=settings.tool_planner_model,
        )
        result = json.loads(raw)
        content_type = str(result.get("content_type", "other")).strip()
        if content_type not in {"lecture", "tutorial", "coding_walkthrough", "explainer", "demo", "other"}:
            content_type = "other"
        topic = str(result.get("topic", "")).strip()
        return {"content_type": content_type, "topic": topic}
    except Exception:
        return {
            "content_type": "lecture",
            "topic": "",
        }
