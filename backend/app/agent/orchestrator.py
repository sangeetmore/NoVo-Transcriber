from __future__ import annotations

import asyncio
from typing import Any, Callable

from app.agent.classifier import classify_session
from app.agent.curator import curate_window
from app.agent.event_designer import design_events
from app.agent.normalizer import normalize_event
from app.adapters import NotionWriter, capture_and_upload
from app.config import settings
from app.models import EventWindow, SessionState


class StudyLensAgent:
    def __init__(
        self,
        session: SessionState,
        notion_writer: NotionWriter,
        activity_callback: Callable[..., None] | None = None,
    ) -> None:
        self.session = session
        self.notion = notion_writer
        self.emit = activity_callback or (lambda **_: None)
        self._buffer = []
        self._window_start = 0.0
        self._window_id = 0
        self._classified = False
        self._events_designed = False

    def ingest_raw_event(self, raw_event: dict) -> None:
        normalized = normalize_event(raw_event)
        if normalized is None:
            return
        self._buffer.append(normalized)
        self.session.events_received += 1

    def _window_ready(self) -> bool:
        if not self._buffer:
            return False
        if self._window_start == 0.0:
            self._window_start = min(e.timestamp_start for e in self._buffer)
        now = max(e.timestamp_end for e in self._buffer)
        threshold = (
            settings.curator_initial_window_seconds
            if self._window_id == 0
            else settings.curator_window_seconds
        )
        return (now - self._window_start) >= threshold

    def _close_window(self) -> EventWindow | None:
        if not self._window_ready():
            return None
        end_time = max(e.timestamp_end for e in self._buffer)
        window = EventWindow(
            window_id=self._window_id,
            start_time=self._window_start,
            end_time=end_time,
            events=list(self._buffer),
        )
        self._buffer = []
        self._window_start = 0.0
        self._window_id += 1
        return window

    async def process_if_ready(
        self,
        conn: Any | None = None,
        scene_index: Any | None = None,
        ws_connection_id: str = "",
    ) -> None:
        window = self._close_window()
        if window is None:
            return

        decision = await curate_window(window, previous_title=self.session.last_concept_title)
        self.session.windows_processed += 1

        if not decision.should_write:
            self.session.windows_skipped += 1
            self.session.last_skip_reason = decision.skip_reason or "skipped"
            self.emit(
                category="skip",
                icon="⏭️",
                label=f"Skipped window {window.window_id}: {self.session.last_skip_reason}",
                detail=(decision.summary or "")[:180],
            )
            return

        await self.notion.write_decision(decision)
        # Flush first section immediately to reduce visible startup lag in Notion.
        if window.window_id == 0:
            await self.notion.flush()
        self.session.windows_written += 1
        self.session.last_concept_title = decision.concept_title
        self.emit(category="section", icon="✓", label=f"Section: {decision.concept_title}")

        # Always attempt screenshot per written window for demo reliability.
        if self.session.display_rtstream_id:
            asyncio.create_task(self._attach_screenshot(decision))

        if not self._classified:
            result = await classify_session(window)
            content_type = str(result.get("content_type", "lecture"))
            self.session.classifier_result = content_type
            self.emit(category="classification", icon="🧠", label=f"Classified: {content_type}")
            self._classified = True
            if conn is not None and scene_index is not None and ws_connection_id:
                designed = await design_events(conn, scene_index, ws_connection_id, content_type)
                self.session.events_designed = [e.get("label", "") for e in designed if e.get("label")]
                for ev in designed:
                    self.emit(
                        category="tool_call",
                        icon="🔧",
                        label=f"Agent registered: {ev.get('label', 'unknown')}",
                        detail=f"-> {ev.get('product_action', '')}",
                    )
                self._events_designed = True

    async def _attach_screenshot(self, decision) -> None:
        try:
            upload_id = await asyncio.to_thread(
                capture_and_upload,
                self.session.display_rtstream_id,
                decision.window_start,
                decision.window_end,
                decision.visual_evidence.target_time_hint,
            )
            if upload_id:
                await self.notion.append_image_file_upload(
                    upload_id,
                    caption=decision.visual_evidence.reason or "",
                )
                self.session.screenshots_captured += 1
                self.emit(category="screenshot", icon="📷", label="Screenshot attached")
        except Exception:
            self.session.screenshot_failures += 1

    async def flush_on_stop(self) -> None:
        if self._buffer:
            end_time = max(e.timestamp_end for e in self._buffer)
            window = EventWindow(
                window_id=self._window_id,
                start_time=self._window_start or end_time,
                end_time=end_time,
                events=list(self._buffer),
            )
            self._buffer = []
            decision = await curate_window(window, previous_title=self.session.last_concept_title)
            if decision.should_write:
                await self.notion.write_decision(decision)

    async def answer_question(self, question: str) -> str:
        return f"Q&A placeholder. Received: {question[:120]}"
