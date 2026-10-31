import uuid
from datetime import datetime
from sqlalchemy import CHAR, Integer, String, Numeric, DateTime, func
from sqlalchemy.orm import Mapped, mapped_column
from .database import Base


class Order(Base):
    __tablename__ = "orders"

    id: Mapped[str] = mapped_column(String, primary_key=True, default=lambda: f"order_{uuid.uuid4().hex[:8]}")
    store_id: Mapped[str] = mapped_column(String, nullable=False, index=True)   # ← Axyra Agent tenant_id_column
    amount: Mapped[float] = mapped_column(Numeric(12, 2), nullable=False)
    currency: Mapped[str] = mapped_column(String(3), default="INR")
    status: Mapped[str] = mapped_column(String(32), default="placed")
    product_id: Mapped[str] = mapped_column(String, nullable=False)
    product_name: Mapped[str] = mapped_column(String, nullable=False)
    category: Mapped[str] = mapped_column(String, nullable=False)

    # ── Customer identity ────────────────────────────────────────────────────
    customer_id: Mapped[str] = mapped_column(String(64), nullable=False, default="", server_default="")
    # Never sent to Axyra (not in the agent's column list) and never shown on analytics pages.
    customer_email: Mapped[str] = mapped_column(String(255), nullable=False, default="", server_default="")
    customer_tier: Mapped[str] = mapped_column(String(32), nullable=False, default="regular", server_default="regular")

    # ── Transaction detail ───────────────────────────────────────────────────
    payment_method: Mapped[str] = mapped_column(String(32), nullable=False, default="card", server_default="card")
    payment_status: Mapped[str] = mapped_column(String(32), nullable=False, default="paid", server_default="paid")
    discount_code: Mapped[str] = mapped_column(String(64), nullable=False, default="", server_default="")
    discount_amount: Mapped[float] = mapped_column(Numeric(10, 2), nullable=False, default=0, server_default="0")
    tax_amount: Mapped[float] = mapped_column(Numeric(10, 2), nullable=False, default=0, server_default="0")
    shipping_cost: Mapped[float] = mapped_column(Numeric(10, 2), nullable=False, default=0, server_default="0")

    # ── Item detail ──────────────────────────────────────────────────────────
    quantity: Mapped[int] = mapped_column(Integer, nullable=False, default=1, server_default="1")
    sku: Mapped[str] = mapped_column(String(64), nullable=False, default="", server_default="")

    # ── Fulfilment ───────────────────────────────────────────────────────────
    fulfillment_status: Mapped[str] = mapped_column(String(32), nullable=False, default="pending", server_default="pending")
    return_reason: Mapped[str] = mapped_column(String(128), nullable=False, default="", server_default="")

    # ── Geography ────────────────────────────────────────────────────────────
    country: Mapped[str] = mapped_column(CHAR(2), nullable=False, default="IN", server_default="IN")
    state_province: Mapped[str] = mapped_column(String(64), nullable=False, default="", server_default="")
    city: Mapped[str] = mapped_column(String(64), nullable=False, default="", server_default="")

    # ── Attribution ──────────────────────────────────────────────────────────
    channel: Mapped[str] = mapped_column(String(32), nullable=False, default="web", server_default="web")
    referrer_source: Mapped[str] = mapped_column(String(64), nullable=False, default="", server_default="")

    created_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), server_default=func.now())
    updated_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), server_default=func.now(), onupdate=func.now())
