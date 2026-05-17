from __future__ import annotations

from contextlib import asynccontextmanager

import uvicorn
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from app.adapters import NotionWriter, VideoDBClient
from app.config import settings
from app.routes import activity_router, qa_router, session_router
from app.routes.session import app_state


@asynccontextmanager
async def lifespan(_: FastAPI):
    app_state["videodb_client"] = VideoDBClient()
    app_state["notion_writer"] = NotionWriter()
    yield
    vdb = app_state.get("videodb_client")
    if vdb:
        await vdb.stop_sandbox()


app = FastAPI(
    title="VideoDB Hackathon Backend",
    version="0.1.0",
    lifespan=lifespan,
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

app.include_router(session_router)
app.include_router(activity_router)
app.include_router(qa_router)


@app.get("/health")
async def health() -> dict:
    return {"status": "ok"}


if __name__ == "__main__":
    uvicorn.run("app.main:app", host=settings.backend_host, port=settings.backend_port, reload=True)
