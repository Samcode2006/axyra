from contextlib import asynccontextmanager
from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from .database import engine, Base
from .migrations import upgrade_schema
from .routers.orders import router as orders_router
from .config import settings


@asynccontextmanager
async def lifespan(app: FastAPI):
    # Create tables on startup (for dev; prod uses Alembic migrations)
    async with engine.begin() as conn:
        await conn.run_sync(Base.metadata.create_all)
        # create_all never alters an existing table, so add any missing columns.
        await upgrade_schema(conn)
    yield


app = FastAPI(
    title="Axyra Reference Client API",
    description=(
        "Demo e-commerce backend. "
        "No Axyra event-publishing code — the Axyra Agent captures all changes "
        "from the PostgreSQL WAL via logical replication automatically."
    ),
    version="0.1.0",
    lifespan=lifespan,
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)

app.include_router(orders_router, prefix="/api/v1")


@app.get("/health", tags=["health"])
async def health():
    return {"status": "ok", "service": "reference-client-backend"}

