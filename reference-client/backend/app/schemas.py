from datetime import datetime
from typing import Literal, Optional
from pydantic import BaseModel, Field


class OrderCreate(BaseModel):
    store_id: str = Field(..., description="Tenant identifier — maps to Axyra external_tenant_id via WAL")
    amount: float = Field(..., gt=0)
    currency: str = Field("INR", max_length=3)
    product_id: str
    product_name: str
    category: str


class OrderUpdate(BaseModel):
    status: Literal["placed", "confirmed", "shipped", "delivered", "cancelled"]


class OrderOut(BaseModel):
    id: str
    store_id: str
    amount: float
    currency: str
    status: str
    product_id: str
    product_name: str
    category: str
    created_at: datetime
    updated_at: datetime

    model_config = {"from_attributes": True}


class OrderListOut(BaseModel):
    data: list[OrderOut]
    total: int

