"""Event Designer — LLM dynamically designs detection rules using tool calls."""

from __future__ import annotations

import asyncio
import json
from typing import Any

from loguru import logger
from openai import OpenAI

from app.agent.tools import EVENT_DESIGNER_TOOLS
from app.config import settings

EVENT_DESIGNER_SYSTEM_PROMPT = """
You are the Event Designer for StudyLens, an educational video learning agent.

The user is watching an educational video or coding tutorial. Your job is to design
3-5 custom visual detection rules for VideoDB.

Each rule must map to a product action:
- capture screenshot
- start new section
- append evidence
- suppress screenshot

Design rules that are:
- specific
- useful for study notes
- mutually distinguishable
- written with inclusion and exclusion criteria

Call register_detection_rule once for each rule.
Do not write prose. Only call tools.
"""

DEFAULT_RULES = [
    {
        "label": "visual_teaching_aid_visible",
        "natural_language_prompt": (
            "Detect when the screen contains a learning-relevant visual aid, including "
            "diagrams, charts, equations, slides, whiteboards, tables, or visual examples. "
            "Exclude plain talking-head frames."
        ),
        "product_action": "capture screenshot",
    },
    {
        "label": "code_or_terminal_visible",
        "natural_language_prompt": (
            "Detect when source code, a code editor, terminal output, console output, or "
            "command-line interface is clearly visible. Exclude plain text slides and "
            "talking-head-only frames."
        ),
        "product_action": "capture screenshot",
    },
    {
        "label": "concept_transition",
        "natural_language_prompt": (
            "Detect when the educational content moves to a new topic, section, or concept. "
            "Look for new slide titles, headings, layout changes, or explicit visual "
            "transitions. Exclude minor cursor movement."
        ),
        "product_action": "start new section",
    },
    {
        "label": "talking_head_only",
        "natural_language_prompt": (
            "Detect when the screen is mostly the instructor speaking without meaningful "
            "learning visuals such as code, diagrams, formulas, charts, slides, or whiteboards."
        ),
        "product_action": "suppress screenshot",
    },
]


def _event_id(event_obj: Any) -> str:
    if isinstance(event_obj, str):
        return event_obj
    value = getattr(event_obj, "id", "")
    return str(value)


def _call_event_designer_sync(user_prompt: str, model_name: str) -> list[dict[str, str]]:
    kwargs: dict[str, str] = {"api_key": settings.llm_api_key}
    if settings.llm_base_url:
        kwargs["base_url"] = settings.llm_base_url
    client = OpenAI(**kwargs)

    response = client.chat.completions.create(
        model=model_name,
        messages=[
            {"role": "system", "content": EVENT_DESIGNER_SYSTEM_PROMPT},
            {"role": "user", "content": user_prompt},
        ],
        tools=EVENT_DESIGNER_TOOLS,
        tool_choice="required",
        parallel_tool_calls=True,
        temperature=0.2,
    )

    message = response.choices[0].message
    if not message.tool_calls:
        return []

    results: list[dict[str, str]] = []
    for tc in message.tool_calls:
        if tc.function.name != "register_detection_rule":
            continue
        try:
            args = json.loads(tc.function.arguments)
            if isinstance(args, dict):
                results.append(args)
        except Exception as exc:
            logger.warning(f"Failed to parse Event Designer tool args: {exc}")
    return results


def _create_alert_compat(scene_index: Any, event_id: str, ws_connection_id: str) -> Any:
    """
    Handle SDK signature drift:
    - create_alert(event_id=..., ws_connection_id=...)
    - create_alert(event_id=..., callback_url=..., ws_connection_id=...)
    - create_alert(event_id, callback_url, ws_connection_id)
    """
    try:
        return scene_index.create_alert(event_id=event_id, ws_connection_id=ws_connection_id)
    except TypeError as exc:
        if "callback_url" not in str(exc):
            raise
    try:
        return scene_index.create_alert(
            event_id=event_id,
            callback_url=None,
            ws_connection_id=ws_connection_id,
        )
    except TypeError:
        return scene_index.create_alert(event_id, None, ws_connection_id)


async def design_events(
    conn: Any,
    scene_index: Any,
    ws_connection_id: str,
    content_type: str,
) -> list[dict[str, str]]:
    """Use LLM tool calling to design and register VideoDB detection rules."""
    if scene_index is None:
        logger.warning("No scene index available for creating alerts.")
        return []

    safe_content_type = content_type.strip() if content_type else ""
    if not safe_content_type or safe_content_type.lower() == "unknown":
        safe_content_type = "general educational video"

    user_prompt = (
        f"The user is watching: {safe_content_type}\n\n"
        "Design 3-5 detection rules appropriate for this content type. "
        "Call register_detection_rule for each one."
    )

    try:
        primary_model = settings.tool_planner_model or settings.curator_model
        tool_calls = await asyncio.to_thread(_call_event_designer_sync, user_prompt, primary_model)
    except Exception as exc:
        fallback_model = settings.curator_fallback_model or settings.curator_model
        if fallback_model and fallback_model != (settings.tool_planner_model or settings.curator_model):
            try:
                logger.warning(
                    f"Event Designer primary model failed ({settings.tool_planner_model}): {exc}. "
                    f"Retrying with fallback model {fallback_model}."
                )
                tool_calls = await asyncio.to_thread(_call_event_designer_sync, user_prompt, fallback_model)
            except Exception as fallback_exc:
                logger.warning(f"Event Designer fallback model failed, using default rules: {fallback_exc}")
                tool_calls = DEFAULT_RULES
        else:
            logger.warning(f"Event Designer LLM failed, using default rules: {exc}")
            tool_calls = DEFAULT_RULES

    if not tool_calls:
        logger.warning("Event Designer returned no tool calls, using default rules.")
        tool_calls = DEFAULT_RULES

    created_events: list[dict[str, str]] = []
    for tc in tool_calls:
        try:
            label = str(tc.get("label", "")).strip()
            prompt = str(tc.get("natural_language_prompt", "")).strip()
            action = str(tc.get("product_action", "")).strip()

            if not label or not prompt:
                logger.warning(f"Skipping invalid detection rule: {tc}")
                continue

            event = conn.create_event(event_prompt=prompt, label=label)
            event_id = _event_id(event)
            alert = _create_alert_compat(scene_index, event_id, ws_connection_id)

            created_events.append(
                {
                    "label": label,
                    "event_id": event_id,
                    "product_action": action,
                    "prompt_preview": prompt[:160],
                    "alert": str(alert),
                }
            )
            logger.info(f"Event Designer registered: {label} -> {event_id}")
        except Exception as exc:
            logger.warning(f"Failed to register detection rule {tc.get('label')}: {exc}")

    return created_events
