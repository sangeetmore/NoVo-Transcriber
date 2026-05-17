from __future__ import annotations

import asyncio
import time
from typing import Any

from fastapi import APIRouter, HTTPException
from loguru import logger
from pydantic import BaseModel

from app.agent import NoteItAgent
from app.config import settings
from app.models import SessionState, SessionStatus
from app.routes.activity import emit_activity

session_router = APIRouter(prefix="/api/session", tags=["session"])
app_state: dict[str, Any] = {}


class StartResponse(BaseModel):
    session_id: str
    capture_session_id: str
    client_token: str
    sandbox_id: str
    notion_page_url: str


def _to_dict(event: Any) -> dict:
    if isinstance(event, dict):
        return event
    if hasattr(event, "model_dump"):
        try:
            return event.model_dump()
        except Exception:
            pass
    if hasattr(event, "dict"):
        try:
            return event.dict()
        except Exception:
            pass
    return {"raw": str(event)}


async def _consumer_loop() -> None:
    try:
        app_state["consumer_error"] = ""
        vdb = app_state["videodb_client"]
        session: SessionState = app_state["session"]
        agent: NoteItAgent = app_state["agent"]

        # Wait for capture client to publish RTStreams (do not fail hard on timing race).
        cap = None
        display_rts = None
        audio_rts = None
        attempt = 0
        while True:
            attempt += 1
            cap = vdb.get_capture_session(session.capture_session_id)
            display_rts, audio_rts = vdb.pick_streams(cap)
            if display_rts or audio_rts:
                break
            if attempt % 10 == 0:
                emit_activity(
                    category="system",
                    icon="⌛",
                    label="Waiting for RTStreams from capture client...",
                )
            await asyncio.sleep(2)

        ws = vdb.connect_websocket()
        await ws.connect()
        session.ws_connection_id = ws.connection_id

        session.display_rtstream_id = str(getattr(display_rts, "id", "")) if display_rts else ""
        session.audio_rtstream_id = str(getattr(audio_rts, "id", "")) if audio_rts else ""
        emit_activity(category="system", icon="📡", label="RTStreams discovered")

        indexes = await asyncio.to_thread(vdb.start_default_indexes, display_rts, audio_rts, ws.connection_id)
        scene_index = indexes.get("visual_index")
        visual_indexes = indexes.get("visual_indexes") or []
        emit_activity(
            category="system",
            icon="👁️",
            label=f"Visual indexing: {settings.visual_index_model}",
        )
        emit_activity(
            category="system",
            icon="🎤",
            label=f"Audio indexing: {settings.audio_index_model}",
        )
        if len(visual_indexes) > 1:
            emit_activity(
                category="system",
                icon="🧠",
                label=f"Multiple visual indexes started: {len(visual_indexes)}",
            )
        emit_activity(category="system", icon="🧩", label="Visual/audio pipelines started")

        async for raw in ws.receive():
            payload = _to_dict(raw)
            agent.ingest_raw_event(payload)
            await agent.process_if_ready(
                conn=vdb.conn,
                scene_index=scene_index,
                ws_connection_id=ws.connection_id,
            )
    except Exception as exc:
        app_state["consumer_error"] = str(exc)
        emit_activity(category="system", icon="❌", label=f"Consumer crashed: {exc}")
        session = app_state.get("session")
        if session:
            session.status = SessionStatus.FAILED
        raise


@session_router.post("/start", response_model=StartResponse)
async def start_session() -> StartResponse:
    if app_state.get("consumer_task") and not app_state["consumer_task"].done():
        raise HTTPException(status_code=409, detail="Session already running")

    vdb = app_state["videodb_client"]
    notion = app_state["notion_writer"]

    start_t = time.perf_counter()
    step_t = start_t

    def mark_step(name: str) -> None:
        nonlocal step_t
        now = time.perf_counter()
        logger.info(f"start_session timing: {name} step={now - step_t:.2f}s total={now - start_t:.2f}s")
        step_t = now

    emit_activity(category="system", icon="⏳", label="Starting sandbox...")
    sandbox_id = await vdb.create_sandbox()
    mark_step("sandbox_ready")
    emit_activity(
        category="system",
        icon="🔧",
        label=f"Sandbox active: {sandbox_id[:12]}…" if sandbox_id else "Sandbox disabled",
    )
    emit_activity(category="system", icon="📹", label="Creating capture session...")
    capture = vdb.create_capture_session()
    mark_step("capture_session_created")
    emit_activity(category="system", icon="📹", label="Capture session created")
    emit_activity(category="system", icon="📝", label="Creating Notion page...")
    _, page_url = await notion.create_page()
    mark_step("notion_page_created")
    emit_activity(category="system", icon="📝", label="Notion page created")
    await notion.start()
    mark_step("notion_writer_started")

    session = SessionState(
        session_id=capture["capture_session_id"],
        status=SessionStatus.CAPTURING,
        capture_session_id=capture["capture_session_id"],
        sandbox_id=sandbox_id,
        sandbox_status="active" if sandbox_id else "disabled",
        visual_model_in_use=settings.visual_index_model,
        audio_model_in_use=settings.audio_index_model,
        notion_page_id=notion.page_id,
        notion_page_url=page_url,
        notion_page_title="Note It · Video Notes",
    )
    app_state["session"] = session

    agent = NoteItAgent(session=session, notion_writer=notion, activity_callback=emit_activity)
    app_state["agent"] = agent
    app_state["consumer_task"] = asyncio.create_task(_consumer_loop())
    mark_step("consumer_task_started")

    emit_activity(category="system", icon="🟢", label="Session started")
    return StartResponse(
        session_id=session.session_id,
        capture_session_id=session.capture_session_id,
        client_token=capture["client_token"],
        sandbox_id=session.sandbox_id,
        notion_page_url=session.notion_page_url,
    )


@session_router.post("/stop")
async def stop_session() -> dict:
    task = app_state.get("consumer_task")
    if task and not task.done():
        task.cancel()
        try:
            await task
        except asyncio.CancelledError:
            pass

    agent = app_state.get("agent")
    if agent:
        await agent.flush_on_stop()

    notion = app_state.get("notion_writer")
    if notion:
        await notion.stop()

    vdb = app_state.get("videodb_client")
    if vdb:
        await vdb.stop_sandbox()

    session: SessionState | None = app_state.get("session")
    if session:
        session.status = SessionStatus.STOPPED
    emit_activity(category="system", icon="⏹️", label="Session stopped")
    return {"ok": True, "session": session.model_dump() if session else None}


@session_router.get("/status")
async def status() -> dict:
    session: SessionState | None = app_state.get("session")
    if not session:
        return {"status": "no_session"}
    payload = session.model_dump()
    payload["consumer_error"] = app_state.get("consumer_error", "")
    payload["consumer_task_done"] = bool(
        app_state.get("consumer_task") and app_state["consumer_task"].done()
    )
    return payload
