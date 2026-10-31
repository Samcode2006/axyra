from fastapi import APIRouter, Depends, HTTPException, Query
from sqlalchemy import select, func
from sqlalchemy.ext.asyncio import AsyncSession

from ..database import get_db
from ..models import Order
from ..schemas import OrderCreate, OrderUpdate, OrderOut, OrderListOut

router = APIRouter(prefix="/orders", tags=["orders"])


@router.post("", response_model=OrderOut, status_code=201)
async def create_order(body: OrderCreate, db: AsyncSession = Depends(get_db)):
    """
    Create a new demo order. No Axyra SDK call — the Axyra Agent reads this
    INSERT automatically from the PostgreSQL WAL via logical replication.
    """
    order = Order(**body.model_dump())
    db.add(order)
    await db.commit()
    await db.refresh(order)
    return order


@router.get("", response_model=OrderListOut)
async def list_orders(
    store_id: str | None = Query(None),
    limit: int = Query(20, le=100),
    offset: int = Query(0),
    db: AsyncSession = Depends(get_db),
):
    q = select(Order).order_by(Order.created_at.desc())
    if store_id:
        q = q.where(Order.store_id == store_id)

    total_q = select(func.count()).select_from(q.subquery())
    total = (await db.execute(total_q)).scalar_one()

    orders = (await db.execute(q.offset(offset).limit(limit))).scalars().all()
    return {"data": orders, "total": total}


@router.get("/{order_id}", response_model=OrderOut)
async def get_order(order_id: str, db: AsyncSession = Depends(get_db)):
    order = await db.get(Order, order_id)
    if not order:
        raise HTTPException(404, "Order not found")
    return order


@router.patch("/{order_id}", response_model=OrderOut)
async def update_order(order_id: str, body: OrderUpdate, db: AsyncSession = Depends(get_db)):
    """
    Update order status (e.g. placed → cancelled).
    The Axyra Agent captures this UPDATE from WAL automatically.
    """
    order = await db.get(Order, order_id)
    if not order:
        raise HTTPException(404, "Order not found")
    order.status = body.status
    await db.commit()
    await db.refresh(order)
    return order

