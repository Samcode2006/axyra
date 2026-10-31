#!/usr/bin/env python3
"""
Seed script for the reference client database.
Creates realistic demo orders across two tenants (store_001, store_002)
to generate WAL traffic for the Axyra Agent.

Usage:
  python scripts/seed/seed_reference_db.py [--count 200]
"""
import argparse
import random
import uuid
import asyncio
from datetime import datetime, timedelta

import httpx

API_URL = "http://localhost:8000/api/v1"

STORES = ["store_001", "store_002"]
PRODUCTS = [
    {"id": "prod_001", "name": "Wireless Headphones", "category": "Electronics"},
    {"id": "prod_002", "name": "Running Shoes", "category": "Footwear"},
    {"id": "prod_003", "name": "Coffee Maker", "category": "Appliances"},
    {"id": "prod_004", "name": "Yoga Mat", "category": "Fitness"},
    {"id": "prod_005", "name": "Backpack", "category": "Accessories"},
    {"id": "prod_006", "name": "Smart Watch", "category": "Electronics"},
    {"id": "prod_007", "name": "Desk Lamp", "category": "Home"},
    {"id": "prod_008", "name": "Water Bottle", "category": "Fitness"},
]

# store_001 gets ~70% of orders (skewed tenant distribution)
STORE_WEIGHTS = [0.70, 0.30]
CANCEL_RATE = 0.15


async def seed(count: int):
    async with httpx.AsyncClient(base_url=API_URL, timeout=30) as client:
        created = 0
        cancelled = 0
        for i in range(count):
            store = random.choices(STORES, weights=STORE_WEIGHTS)[0]
            product = random.choice(PRODUCTS)
            amount = round(random.uniform(199, 9999), 2)

            resp = await client.post("/orders", json={
                "store_id": store,
                "amount": amount,
                "currency": "INR",
                "product_id": product["id"],
                "product_name": product["name"],
                "category": product["category"],
            })
            resp.raise_for_status()
            order = resp.json()
            created += 1

            # Randomly cancel some to generate UPDATE WAL events
            if random.random() < CANCEL_RATE:
                await client.patch(f"/orders/{order['id']}", json={"status": "cancelled"})
                cancelled += 1

            if (i + 1) % 50 == 0:
                print(f"  seeded {i + 1}/{count}...")

        print(f"\n✅ Done. Created {created} orders, cancelled {cancelled}.")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Seed reference DB with demo orders")
    parser.add_argument("--count", type=int, default=200, help="Number of orders to create")
    args = parser.parse_args()
    print(f"Seeding {args.count} orders against {API_URL}...")
    asyncio.run(seed(args.count))

