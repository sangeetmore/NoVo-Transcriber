from __future__ import annotations

import asyncio
import time
from collections import deque

from fastapi import APIRouter, WebSocket, WebSocketDisconnect

activity_router = APIRouter()

_activity_log: deque[dict] = deque(maxlen=200)
_clients: list[WebSocket] = []


def emit_activity(
    category: str = "system",
    icon: str = "ℹ️",
    label: str = "",
    detail: str = "",
    metadata: dict | None = None,
) -> None:
    event = {
        "type": "agent_event",
        "timestamp": time.time(),
        "category": category,
        "icon": icon,
        "label": label,
        "detail": detail,
        "metadata": metadata or {},
    }
    _activity_log.append(event)
    for ws in list(_clients):
        try:
            asyncio.create_task(ws.send_json(event))
        except Exception:
            continue


@activity_router.websocket("/ws/activity")
async def ws_activity(websocket: WebSocket) -> None:
    await websocket.accept()
    _clients.append(websocket)
    try:
        for event in list(_activity_log):
            await websocket.send_json(event)
        while True:
            payload = await websocket.receive_text()
            if payload == "ping":
                await websocket.send_json({"type": "pong"})
    except WebSocketDisconnect:
        pass
    finally:
        if websocket in _clients:
            _clients.remove(websocket)
