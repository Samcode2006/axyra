"""Idempotent schema upgrades for the reference database.

SQLAlchemy's create_all() only creates missing tables; it never adds a column to
a table that already exists. A reference database created before the schema
expansion therefore needs these ALTERs, which are safe to run on every start.

The column list mirrors docs/schema-expansion-plan.md section 1 and models.py.
Constant defaults make each ADD COLUMN a metadata-only change.
"""

from sqlalchemy import text
from sqlalchemy.ext.asyncio import AsyncConnection

ORDER_COLUMNS: list[tuple[str, str]] = [
    ("customer_id", "VARCHAR(64) NOT NULL DEFAULT ''"),
    ("customer_email", "VARCHAR(255) NOT NULL DEFAULT ''"),
    ("customer_tier", "VARCHAR(32) NOT NULL DEFAULT 'regular'"),
    ("payment_method", "VARCHAR(32) NOT NULL DEFAULT 'card'"),
    ("payment_status", "VARCHAR(32) NOT NULL DEFAULT 'paid'"),
    ("discount_code", "VARCHAR(64) NOT NULL DEFAULT ''"),
    ("discount_amount", "NUMERIC(10,2) NOT NULL DEFAULT 0"),
    ("tax_amount", "NUMERIC(10,2) NOT NULL DEFAULT 0"),
    ("shipping_cost", "NUMERIC(10,2) NOT NULL DEFAULT 0"),
    ("quantity", "INT NOT NULL DEFAULT 1"),
    ("sku", "VARCHAR(64) NOT NULL DEFAULT ''"),
    ("fulfillment_status", "VARCHAR(32) NOT NULL DEFAULT 'pending'"),
    ("return_reason", "VARCHAR(128) NOT NULL DEFAULT ''"),
    ("country", "CHAR(2) NOT NULL DEFAULT 'IN'"),
    ("state_province", "VARCHAR(64) NOT NULL DEFAULT ''"),
    ("city", "VARCHAR(64) NOT NULL DEFAULT ''"),
    ("channel", "VARCHAR(32) NOT NULL DEFAULT 'web'"),
    ("referrer_source", "VARCHAR(64) NOT NULL DEFAULT ''"),
]


async def upgrade_schema(conn: AsyncConnection) -> None:
    """Add any missing extended order columns. Safe to call repeatedly."""
    for name, ddl in ORDER_COLUMNS:
        # Identifiers come from the constant list above, never from input.
        await conn.execute(text(f"ALTER TABLE orders ADD COLUMN IF NOT EXISTS {name} {ddl}"))
