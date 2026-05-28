from __future__ import annotations

import asyncio
from typing import Any

import videodb

from app.config import settings


class VideoDBClient:
    def __init__(self) -> None:
        self.conn = videodb.connect(api_key=settings.video_db_api_key)
        self.sandbox: Any = None
        self._sandbox_task: asyncio.Task[str] | None = None

    # prewarm to avoid delay when user clicks 'start session' button in the app...
    def prewarm_sandbox(self) -> None:
        if settings.use_sandbox and self._sandbox_task is None:
            self._sandbox_task = asyncio.create_task(self.create_sandbox())

    async def ensure_sandbox(self) -> str:
        if not settings.use_sandbox:
            return ""
        if self._sandbox_task is not None:
            try:
                return await self._sandbox_task
            except Exception:
                self._sandbox_task = None
                raise
        return await self.create_sandbox()

    async def create_sandbox(self) -> str:
        if not settings.use_sandbox:
            self.sandbox = None
            return ""
        existing_id = getattr(self.sandbox, "id", "")
        if existing_id:
            return str(existing_id)

        tier_name = "small" if settings.sandbox_tier.lower() == "small" else "medium"
        last_error: Exception | None = None
        # Some API calls can return a sandbox that immediately enters terminal state.
        # Retry a few times before failing the session start.
        max_attempts = 3

        for _ in range(max_attempts):
            self.sandbox = None
            attempt_error: Exception | None = None

            # Try enum tier first (if available), then string tier.
            # For each, try with idle_timeout, then retry without idle_timeout
            # for SDK builds that don't accept this argument.
            tier_values: list[Any] = []
            try:
                from videodb import SandboxTier  # type: ignore

                tier_values.append(SandboxTier.small if tier_name == "small" else SandboxTier.medium)
            except Exception:
                pass
            tier_values.append(tier_name)

            for tier in tier_values:
                try:
                    self.sandbox = self.conn.create_sandbox(
                        tier=tier,
                        idle_timeout=settings.sandbox_idle_timeout,
                    )
                    break
                except Exception as exc:
                    attempt_error = exc
                    self.sandbox = None
                    try:
                        self.sandbox = self.conn.create_sandbox(tier=tier)
                        break
                    except Exception as exc_no_idle:
                        attempt_error = exc_no_idle
                        self.sandbox = None

            if self.sandbox is None:
                last_error = attempt_error
                await asyncio.sleep(1)
                continue

            try:
                self.sandbox.wait_for_ready(timeout=300, interval=5)
                sandbox_id = getattr(self.sandbox, "id", "")
                if not sandbox_id:
                    raise RuntimeError("Sandbox was created but no sandbox id was returned")
                return str(sandbox_id)
            except Exception as ready_exc:
                last_error = ready_exc
                try:
                    self.sandbox.stop()
                except Exception:
                    pass
                self.sandbox = None
                await asyncio.sleep(2)

        raise RuntimeError(
            f"Sandbox creation failed with USE_SANDBOX=true after {max_attempts} attempts. "
            f"tier={tier_name}. error={last_error}"
        )

    async def stop_sandbox(self) -> None:
        if self.sandbox is not None:
            try:
                self.sandbox.stop()
                wait_for_stop = getattr(self.sandbox, "wait_for_stop", None)
                if callable(wait_for_stop):
                    wait_for_stop(timeout=15)
            except Exception:
                pass
            finally:
                self.sandbox = None

    def create_capture_session(self) -> dict[str, str]:
        cap = self.conn.create_capture_session(
            end_user_id="noteit_user",
            metadata={"app": "novo-transcriber", "phase": "hackathon"},
        )
        token = self.conn.generate_client_token(expires_in=settings.capture_client_token_ttl_seconds)
        return {"capture_session_id": cap.id, "client_token": token}

    def get_capture_session(self, capture_session_id: str) -> Any:
        return self.conn.get_capture_session(capture_session_id)

    def connect_websocket(self) -> Any:
        return self.conn.connect_websocket()

    def pick_streams(self, cap: Any) -> tuple[Any, Any]:
        def _pick_first(value: Any) -> Any:
            if not value:
                return None
            if isinstance(value, list):
                return value[0] if value else None
            return value

        display = _pick_first(cap.get_rtstream("screen") or cap.get_rtstream("display") or cap.get_rtstream("video"))
        audio = _pick_first(cap.get_rtstream("system_audio") or cap.get_rtstream("mic") or cap.get_rtstream("audio"))
        return display, audio

    def _visual_prompts(self) -> list[str]:
        prompts: list[str] = []
        raw = (settings.visual_index_prompts or "").strip()
        if raw:
            # Support "||" separated prompts in env.
            prompts = [p.strip() for p in raw.split("||") if p.strip()]
        if not prompts:
            prompts = [
                (
                    "Describe learning-relevant visual content only: code, diagrams, charts, formulas, "
                    "slides, whiteboard, or tutorial UI steps."
                )
            ]
        return prompts

    def start_default_indexes(self, display_rts: Any, audio_rts: Any, ws_connection_id: str) -> dict[str, Any]:
        visual_indexes: list[Any] = []
        audio_index = None
        if display_rts:
            for prompt in self._visual_prompts():
                visual_kwargs = {
                    "prompt": prompt,
                    "ws_connection_id": ws_connection_id,
                    "model_name": settings.visual_index_model,
                }
                if self.sandbox is not None:
                    visual_kwargs["sandbox_id"] = self.sandbox.id
                visual_indexes.append(display_rts.index_visuals(**visual_kwargs))
        if audio_rts:
            audio_rts.start_transcript(ws_connection_id=ws_connection_id)
            audio_kwargs = {
                "prompt": (
                    "Analyze educational audio flow: topic shift, explanation quality, examples, "
                    "summary signals, and emphasis."
                ),
                "ws_connection_id": ws_connection_id,
                "model_name": settings.audio_index_model,
            }
            if self.sandbox is not None:
                audio_kwargs["sandbox_id"] = self.sandbox.id
            audio_index = audio_rts.index_audio(**audio_kwargs)
        return {
            "visual_index": visual_indexes[0] if visual_indexes else None,
            "visual_indexes": visual_indexes,
            "audio_index": audio_index,
        }

    def create_event(self, event_prompt: str, label: str) -> Any:
        return self.conn.create_event(event_prompt=event_prompt, label=label)
