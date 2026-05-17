from __future__ import annotations

import math
import subprocess
import time
from pathlib import Path

import requests

from app.config import settings

FRAME_DIR = Path("extracted_frames")


def _choose_ts(start_ts: float, end_ts: float, hint: str | None) -> float:
    if hint == "start":
        return start_ts
    if hint == "end":
        return end_ts
    return (start_ts + end_ts) / 2.0


def _get_rtstream_playback_url(rtstream_id: str, start_ts: float, end_ts: float) -> str:
    url = f"{settings.video_db_base_url}/rtstream/{rtstream_id}/stream"
    headers = {"x-access-token": settings.video_db_api_key}
    params = {
        "start": int(math.floor(start_ts)),
        "end": int(math.ceil(end_ts)),
        "frame_rate": 10,
    }
    response = requests.get(url, params=params, headers=headers, timeout=30)
    response.raise_for_status()
    payload = response.json()
    if not payload.get("success"):
        raise RuntimeError(f"Playback failed: {payload}")
    return payload["data"]["stream_url"]


def _extract_frame(stream_url: str, offset_seconds: float, output: Path) -> Path:
    output.parent.mkdir(parents=True, exist_ok=True)
    cmd = [
        "ffmpeg",
        "-y",
        "-loglevel",
        "error",
        "-i",
        stream_url,
        "-ss",
        f"{offset_seconds:.3f}",
        "-frames:v",
        "1",
        "-q:v",
        "2",
        str(output),
    ]
    subprocess.run(cmd, check=True, capture_output=True, text=True, timeout=30)
    if not output.exists() or output.stat().st_size <= 0:
        raise RuntimeError("ffmpeg produced no frame")
    return output


def _extract_local(output: Path) -> Path:
    output.parent.mkdir(parents=True, exist_ok=True)
    subprocess.run(["screencapture", "-x", str(output)], check=True, capture_output=True, text=True, timeout=15)
    if not output.exists() or output.stat().st_size <= 0:
        raise RuntimeError("screencapture produced no file")
    return output


def upload_to_notion(file_path: Path) -> str:
    create_headers = {
        "Authorization": f"Bearer {settings.notion_token}",
        "Notion-Version": settings.notion_version,
        "Content-Type": "application/json",
    }
    create_response = requests.post(
        "https://api.notion.com/v1/file_uploads",
        headers=create_headers,
        json={"mode": "single_part", "filename": file_path.name, "content_type": "image/jpeg"},
        timeout=30,
    )
    create_response.raise_for_status()
    upload_id = create_response.json().get("id")
    if not upload_id:
        raise RuntimeError("Notion file upload id missing")

    send_headers = {
        "Authorization": f"Bearer {settings.notion_token}",
        "Notion-Version": settings.notion_version,
    }
    with file_path.open("rb") as fh:
        files = {"file": (file_path.name, fh, "image/jpeg")}
        send_response = requests.post(
            f"https://api.notion.com/v1/file_uploads/{upload_id}/send",
            headers=send_headers,
            files=files,
            timeout=60,
        )
    send_response.raise_for_status()
    return upload_id


def capture_and_upload(
    rtstream_id: str,
    start_ts: float,
    end_ts: float,
    target_hint: str | None,
) -> str | None:
    target_ts = _choose_ts(start_ts, end_ts, target_hint)
    output_path = FRAME_DIR / f"window_{int(time.time())}.jpg"
    if settings.screenshot_local_first:
        try:
            frame = _extract_local(output_path)
            return upload_to_notion(frame)
        except Exception:
            pass

    try:
        stream_url = _get_rtstream_playback_url(rtstream_id, start_ts - 1.0, end_ts + 1.0)
        frame = _extract_frame(stream_url, max(0.0, target_ts - (start_ts - 1.0)), output_path)
    except Exception:
        if not settings.screenshot_local_fallback:
            return None
        frame = _extract_local(output_path)
    return upload_to_notion(frame)
