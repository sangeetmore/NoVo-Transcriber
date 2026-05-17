from __future__ import annotations

from fastapi import APIRouter, HTTPException
from pydantic import BaseModel

from app.routes.session import app_state

qa_router = APIRouter(prefix="/api/qa", tags=["qa"])


class AskRequest(BaseModel):
    question: str


@qa_router.post("/ask")
async def ask(req: AskRequest) -> dict:
    agent = app_state.get("agent")
    if not agent:
        raise HTTPException(status_code=404, detail="No active session")
    answer = await agent.answer_question(req.question)
    return {"answer": answer}
