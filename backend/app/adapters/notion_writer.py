from __future__ import annotations

import asyncio
import hashlib
from datetime import datetime
from typing import Any

from loguru import logger
from notion_client import AsyncClient
from notion_client.errors import APIResponseError

from app.config import settings
from app.models import CuratorDecision

MAX_TEXT_LEN = 1800
DEFAULT_NOTE_TOPIC = "Video Notes"
BAD_IMAGE_CAPTION_PHRASES = (
    "no visual content",
    "no visual information",
    "nothing to screenshot",
    "nothing useful to capture",
    "nothing useful to screenshot",
    "nothing to capture",
    "transcript alone is sufficient",
    "audio alone is sufficient",
)


def _split_for_notion(text: str, max_len: int = MAX_TEXT_LEN) -> list[str]:
    clean = " ".join((text or "").split()).strip()
    if not clean:
        return []
    if len(clean) <= max_len:
        return [clean]
    out: list[str] = []
    rest = clean
    while len(rest) > max_len:
        cut = rest.rfind(" ", 0, max_len)
        if cut <= 0:
            cut = max_len
        out.append(rest[:cut].strip())
        rest = rest[cut:].strip()
    if rest:
        out.append(rest)
    return out


def _rt(text: str) -> list[dict[str, Any]]:
    return [{"type": "text", "text": {"content": text}}]


def _clean_image_caption(caption: str) -> str:
    clean = " ".join((caption or "").split()).strip()
    if not clean:
        return ""
    lowered = clean.lower()
    if any(phrase in lowered for phrase in BAD_IMAGE_CAPTION_PHRASES):
        return ""
    return clean[:180]


def _clean_topic(topic: str) -> str:
    clean = " ".join((topic or "").split()).strip()
    if not clean:
        return DEFAULT_NOTE_TOPIC
    bad_prefixes = ("tutorial:", "lecture:", "video about", "introduction to")
    lowered = clean.lower()
    for prefix in bad_prefixes:
        if lowered.startswith(prefix):
            clean = clean[len(prefix) :].strip(" :-")
            break
    return clean[:80] or DEFAULT_NOTE_TOPIC


def format_note_title(topic: str = "") -> str:
    now = datetime.now()
    month_day = f"{now.strftime('%b')} {now.day}"
    return f"Note It · {_clean_topic(topic)} ({month_day} · {now:%H:%M})"


class NotionWriter:
    def __init__(self) -> None:
        self._client = AsyncClient(auth=settings.notion_token)
        self.page_id: str = ""
        self.page_url: str = ""
        self._buffer: list[dict[str, Any]] = []
        self._dedupe: set[str] = set()
        self._lock = asyncio.Lock()
        self._flush_task: asyncio.Task | None = None
        self._running = False

    async def start(self) -> None:
        if self._running:
            return
        self._running = True
        self._flush_task = asyncio.create_task(self._flush_loop())

    async def stop(self) -> None:
        self._running = False
        if self._flush_task:
            self._flush_task.cancel()
            try:
                await self._flush_task
            except asyncio.CancelledError:
                pass
        await self.flush()

    async def create_page(self, topic: str = DEFAULT_NOTE_TOPIC) -> tuple[str, str]:
        title = format_note_title(topic)
        page = await self._client.pages.create(
            parent={"page_id": settings.notion_parent_page_id},
            properties={"title": {"title": _rt(title)}},
        )
        self.page_id = page["id"]
        self.page_url = f"https://notion.so/{self.page_id.replace('-', '')}"
        self._dedupe.clear()
        return self.page_id, self.page_url

    async def rename_page_for_topic(self, topic: str) -> str:
        if not self.page_id:
            return ""
        title = format_note_title(topic)
        try:
            await self._client.pages.update(
                page_id=self.page_id,
                properties={"title": {"title": _rt(title)}},
            )
            return title
        except APIResponseError as exc:
            logger.warning(f"Notion page rename failed: {exc}")
            return ""

    async def queue_blocks(self, blocks: list[dict[str, Any]]) -> None:
        if not blocks or not self.page_id:
            return
        deduped: list[dict[str, Any]] = []
        for block in blocks:
            key = hashlib.sha1(repr(block).encode("utf-8")).hexdigest()
            if key in self._dedupe:
                continue
            self._dedupe.add(key)
            deduped.append(block)
        if not deduped:
            return
        async with self._lock:
            self._buffer.extend(deduped)

    async def write_decision(self, decision: CuratorDecision) -> None:
        if not decision.should_write:
            return
        blocks: list[dict[str, Any]] = []
        title = decision.concept_title or "Untitled Concept"
        blocks.append({"type": "heading_2", "heading_2": {"rich_text": _rt(title[:100])}})
        if decision.summary:
            for chunk in _split_for_notion(decision.summary):
                blocks.append({"type": "paragraph", "paragraph": {"rich_text": _rt(chunk)}})
        for point in decision.key_points[:5]:
            for chunk in _split_for_notion(point, 200):
                blocks.append({"type": "bulleted_list_item", "bulleted_list_item": {"rich_text": _rt(chunk)}})
        if decision.takeaway:
            blocks.append(
                {
                    "type": "callout",
                    "callout": {"icon": {"type": "emoji", "emoji": "💡"}, "rich_text": _rt(decision.takeaway[:300])},
                }
            )
        blocks.append({"type": "divider", "divider": {}})
        await self.queue_blocks(blocks)

    async def append_image_file_upload(self, file_upload_id: str, caption: str = "") -> None:
        if not file_upload_id:
            return
        clean_caption = _clean_image_caption(caption)
        block = {
            "type": "image",
            "image": {
                "type": "file_upload",
                "file_upload": {"id": file_upload_id},
                "caption": _rt(clean_caption) if clean_caption else [],
            },
        }
        await self.queue_blocks([block])
        await self.flush()

    async def _flush_loop(self) -> None:
        while self._running:
            await asyncio.sleep(settings.notion_batch_interval_s)
            await self.flush()

    async def flush(self) -> None:
        if not self.page_id:
            return
        async with self._lock:
            if not self._buffer:
                return
            chunk = self._buffer[:100]
            self._buffer = self._buffer[100:]
        try:
            await self._client.blocks.children.append(block_id=self.page_id, children=chunk)
        except APIResponseError as exc:
            logger.warning(f"Notion append failed for {len(chunk)} block(s): {exc}")
