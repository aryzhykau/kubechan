"""llm-gateway — multi-provider LLM analysis gateway (Bedrock + Copilot)."""
from __future__ import annotations

import logging

from fastapi import FastAPI

from app.routes import router

logging.basicConfig(level=logging.INFO)

app = FastAPI(title="llm-gateway", version="0.2.0")
app.include_router(router)
