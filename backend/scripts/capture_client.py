import asyncio
import os
import sys
import time
from typing import Any

from videodb.capture import CaptureClient


def _read_inputs() -> tuple[str, str]:
    cap_id = os.getenv("CAPTURE_SESSION_ID") or (sys.argv[1] if len(sys.argv) > 1 else None)
    token = os.getenv("CLIENT_TOKEN") or (sys.argv[2] if len(sys.argv) > 2 else None)
    if not cap_id or not token:
        raise RuntimeError(
            "Missing CAPTURE_SESSION_ID / CLIENT_TOKEN.\n"
            "Usage:\n"
            "  python scripts/capture_client.py <CAPTURE_SESSION_ID> <CLIENT_TOKEN>"
        )
    return cap_id, token


def _safe_name(channel: Any) -> str:
    return getattr(channel, "name", str(channel))


async def _request_permissions(client: CaptureClient) -> None:
    await client.request_permission("microphone")
    await client.request_permission("screen_capture")


async def main() -> None:
    capture_session_id, client_token = _read_inputs()
    listen_seconds = int(os.getenv("CAPTURE_CLIENT_LISTEN_SECONDS", "900"))

    client = CaptureClient(client_token=client_token)
    started = False

    try:
        await _request_permissions(client)
        channels = await client.list_channels()

        mics = getattr(channels, "mics", None)
        displays = getattr(channels, "displays", []) or []
        system_audio_group = getattr(channels, "system_audio", None)

        mic = getattr(mics, "default", None) if mics else None
        system_audio = getattr(system_audio_group, "default", None) if system_audio_group else None
        display = next((d for d in displays if getattr(d, "is_primary", False)), None)
        if not display and displays:
            display = displays[0]
        if display is not None:
            try:
                setattr(display, "is_primary", True)
            except Exception:
                pass

        selected_channels = [c for c in [mic, display, system_audio] if c is not None]
        if not selected_channels:
            raise RuntimeError("No capture channels available to start session.")

        print(f"Starting session with channels: {[ _safe_name(c) for c in selected_channels ]}")
        await client.start_session(capture_session_id=capture_session_id, channels=selected_channels)
        started = True
        print(f"Session started. Keeping capture alive for {listen_seconds}s...")
        print("Play educational content now.")

        deadline = time.time() + listen_seconds
        event_iter = client.events().__aiter__()
        while time.time() < deadline:
            timeout_s = max(0.2, min(2.0, deadline - time.time()))
            if event_iter is None:
                await asyncio.sleep(timeout_s)
                continue
            try:
                _ = await asyncio.wait_for(anext(event_iter), timeout=timeout_s)
            except TimeoutError:
                continue
            except StopAsyncIteration:
                event_iter = None
                continue
    finally:
        if started:
            print("Stopping session...")
            try:
                await client.stop_session()
            except Exception as exc:
                print(f"stop_session warning: {exc}")
        await client.shutdown()
        print("Client shutdown complete.")


if __name__ == "__main__":
    asyncio.run(main())
