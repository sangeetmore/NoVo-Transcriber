from __future__ import annotations

from openai import AsyncOpenAI

from app.config import settings

_client: AsyncOpenAI | None = None


def _get_client() -> AsyncOpenAI:
    global _client
    if _client is None:
        if not settings.llm_api_key:
            raise RuntimeError("OPENAI_API_KEY (or LLM_API_KEY) is missing")
        kwargs = {"api_key": settings.llm_api_key}
        if settings.llm_base_url:
            kwargs["base_url"] = settings.llm_base_url
        _client = AsyncOpenAI(**kwargs)
    return _client


async def call_llm_json(system_prompt: str, user_prompt: str, model: str) -> str:
    client = _get_client()
    response = await client.chat.completions.create(
        model=model,
        messages=[
            {"role": "system", "content": system_prompt},
            {"role": "user", "content": user_prompt},
        ],
        response_format={"type": "json_object"},
        temperature=0.1,
        max_tokens=1200,
    )
    return response.choices[0].message.content or "{}"
