from datetime import datetime
from typing import Literal, Optional
from pydantic import BaseModel, Field, field_validator

# Allowed values (docs/schema-expansion-plan.md section 1). Validating here keeps
# junk out of the source database, which Axyra would otherwise aggregate faithfully.
CustomerTier = Literal["free", "regular", "silver", "gold", "platinum"]
PaymentMethod = Literal["card", "upi", "wallet", "cod", "bnpl", "netbanking"]
PaymentStatus = Literal["pending", "paid", "failed", "refunded", "chargeback"]
FulfillmentStatus = Literal["pending", "processing", "shipped", "delivered", "returned", "cancelled"]
Channel = Literal["web", "mobile_app", "pos", "marketplace", "api"]
OrderStatus = Literal["placed", "confirmed", "shipped", "delivered", "cancelled", "returned"]

# The fulfilment stage that normally follows from an order status.
FULFILLMENT_FOR_STATUS: dict[str, str] = {
    "placed": "pending",
    "confirmed": "processing",
    "shipped": "shipped",
    "delivered": "delivered",
    "cancelled": "cancelled",
    "returned": "returned",
}


class OrderCreate(BaseModel):
    store_id: str = Field(..., description="Tenant identifier — maps to Axyra external_tenant_id via WAL")
    amount: float = Field(..., gt=0, lt=100_000_000)
    currency: str = Field("INR", max_length=3)
    product_id: str
    product_name: str
    category: str
    created_at: Optional[datetime] = None

    # Item detail
    sku: str = Field("", max_length=64)
    quantity: int = Field(1, ge=1, le=10_000)

    # Customer
    customer_id: str = Field("", max_length=64)
    # Stored in the source database only. It is not in the agent's column list,
    # so it never reaches Axyra, and it is not returned by this API.
    customer_email: str = Field("", max_length=255)
    customer_tier: CustomerTier = "regular"

    # Transaction detail
    payment_method: PaymentMethod = "card"
    discount_code: str = Field("", max_length=64)
    discount_amount: float = Field(0.0, ge=0, lt=100_000_000)
    tax_amount: float = Field(0.0, ge=0, lt=100_000_000)
    shipping_cost: float = Field(0.0, ge=0, lt=100_000_000)

    # Attribution
    channel: Channel = "web"
    referrer_source: str = Field("", max_length=64)

    # Geography
    country: str = Field("IN", min_length=2, max_length=2, description="ISO 3166-1 alpha-2")
    state_province: str = Field("", max_length=64)
    city: str = Field("", max_length=64)

    @field_validator("country")
    @classmethod
    def _country_upper_alpha(cls, v: str) -> str:
        v = v.upper()
        if not (v.isascii() and v.isalpha()):
            raise ValueError("country must be a 2-letter ISO 3166-1 alpha-2 code")
        return v


class OrderUpdate(BaseModel):
    status: OrderStatus
    # When omitted, the fulfilment stage follows the status (see FULFILLMENT_FOR_STATUS).
    fulfillment_status: Optional[FulfillmentStatus] = None
    return_reason: Optional[str] = Field(None, max_length=128)
    payment_status: Optional[PaymentStatus] = None


class OrderOut(BaseModel):
    id: str
    store_id: str
    amount: float
    currency: str
    status: str
    product_id: str
    product_name: str
    category: str

    sku: str
    quantity: int
    customer_id: str
    customer_tier: str
    payment_method: str
    payment_status: str
    discount_code: str
    discount_amount: float
    tax_amount: float
    shipping_cost: float
    fulfillment_status: str
    return_reason: str
    channel: str
    referrer_source: str
    country: str
    state_province: str
    city: str

    created_at: datetime
    updated_at: datetime

    model_config = {"from_attributes": True}


class OrderListOut(BaseModel):
    data: list[OrderOut]
    total: int
