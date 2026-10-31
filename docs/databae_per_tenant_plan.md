# Architectural & Implementation Plan: Database-per-Tenant Isolation

## 1. Problem & Architecture Overview

Enterprise clients demand **physical data isolation** (Silo Model). Instead of multiple tenants sharing one database and partitioning rows via `store_id`, each tenant has their own completely dedicated PostgreSQL database instance.

### Target Architecture:
- **N Tenant PostgreSQL Databases**: `reference-db-1` (`store_001`), `reference-db-2` (`store_002`), ..., `reference-db-N` (`store_00N`).
- **Single Axyra Agent**: Listens to WAL replication streams from all N databases concurrently via Go worker routines.
- **Dynamic Tenant Scaling via `.env`**: Set `TENANT_COUNT=5` (or any number $N$) to spawn $N$ databases and automatically monitor them.
- **Axyra Data & Control Plane**: Aggregates tenant events into:
  - **Per-Tenant View**: Isolated metrics, rate limits, latency, and reporting per `tenant_id`.
  - **Platform Operator View**: Aggregated cross-tenant GMV, total throughput, agent health, and global system stats.

```mermaid
graph TD
    subgraph Isolated Tenant Tier
        DB1[("Tenant DB 1<br>(store_001)")]
        DB2[("Tenant DB 2<br>(store_002)")]
        DBN[("Tenant DB N<br>(store_00N)")]
    end

    subgraph Axyra Agent Layer
        Agent["Axyra Agent<br>(Multi-Connector Engine)"]
        W1["Worker 1 (WAL Slot 1)"]
        W2["Worker 2 (WAL Slot 2)"]
        WN["Worker N (WAL Slot N)"]
        Agent --> W1 & W2 & WN
    end

    DB1 -.->|Logical Replication| W1
    DB2 -.->|Logical Replication| W2
    DBN -.->|Logical Replication| WN

    subgraph Axyra Platform
        Analytics["Analytics Service (:8082)"]
        CP["Control Plane (:8081)"]
        PlatformDB[("Axyra Platform DB")]
        Web["Web Console (:5173)"]
    end

    Agent == "Buffered Event Batches (with external_tenant_id)" ==> Analytics
    Analytics --> PlatformDB
    CP --> PlatformDB
    Web --> CP & Analytics
```

---

## 2. Technical Design & Key Components

### A. Dynamic Tenant Database Provisioning
- Instead of hardcoding 5 or 10 static container definitions in `docker-compose.yaml`, we can support both:
  1. Standard `docker-compose.yaml` with the default 5 tenant databases (`reference-db-1` .. `reference-db-5`).
  2. A compose override / generation script or parameterized entrypoint to spin up $N$ database containers according to `TENANT_COUNT` in `.env`.
- In tenant databases, the `orders` table no longer strictly needs `store_id` (since the database itself is owned by the tenant), but the agent connector tags each event with the tenant's identifier (`default_tenant_id` or database identifier).

### B. Multi-Source Agent Connector
1. **Config Enhancement (`agent/internal/config/config.go`)**:
   - Add support for multiple database sources:
     ```yaml
     sources:
       - name: "store_001"
         postgres_dsn: "postgres://postgres:postgres@reference-db-1:5432/reference_db?sslmode=disable"
         external_tenant_id: "store_001"
         replication_slot: "axyra_slot_store_001"
       - name: "store_002"
         ...
     ```
   - Also support an environment variable pattern like `POSTGRES_DSNS="store_001=postgres://...;store_002=postgres://..."` or `TENANT_COUNT=5` auto-discovery (`reference-db-{i}`).

2. **Agent Engine (`agent/cmd/agent/main.go`)**:
   - Run a PostgreSQL logical replication connector per source database in its own goroutine.
   - All connectors stream their `ChangeEvent`s into the common `changeEvents` channel.
   - In `ChangeEvent`, attach `SourceTenantID` or have `mapper` assign `external_tenant_id` from the source context if omitted in the row.

3. **Per-Source LSN Cursors**:
   - Each tenant database has its own WAL timeline and LSN sequence.
   - The agent maintains LSN cursor tracking per source connector (`agent_id + external_tenant_id`), preventing cross-database LSN collisions.

### C. Multi-Database Seeding Tool
- Update `scripts/seed/bulk_seed.go` and `run_seed.sh`:
  - Read `TENANT_COUNT` or loop through the $N$ tenant database ports (`5433`, `5434`, etc.).
  - Distribute seed data across all $N$ tenant databases concurrently.

---

## 3. Step-by-Step Execution Plan

| Step | Action | Files Touched |
| :--- | :--- | :--- |
| **Step 1** | **Multi-Source Agent Configuration**<br>Add `sources` support in agent config, supporting YAML and `TENANT_COUNT` env auto-generation. | `agent/internal/config/config.go`<br>`configs/agent-example.yaml` |
| **Step 2** | **Multi-Source Connector Runner**<br>Update `main.go` and connector to spawn a replication routine per tenant database with its own slot & LSN tracking. | `agent/cmd/agent/main.go`<br>`agent/internal/connector/postgres/connector.go`<br>`agent/internal/mapper/mapper.go` |
| **Step 3** | **Dynamic Multi-DB Compose Setup**<br>Configure 5 tenant databases in `docker-compose.yaml` (ports 5433–5437) wired to `.env`. Provide dynamic scaling capability via `TENANT_COUNT`. | `deploy/docker-compose.yaml`<br>`deploy/.env` |
| **Step 4** | **Multi-DB Seeding Script**<br>Update seeding script to seed all 5 tenant databases in parallel. | `scripts/seed/bulk_seed.go`<br>`scripts/seed/run_seed.sh` |
| **Step 5** | **Platform Aggregation & Tenant Verification**<br>Start containers, seed 5 databases, and verify both per-tenant metrics and global platform operator dashboard. | Verification via Web UI / API |

---

## 4. Verification & Demo Criteria
1. Run `docker compose up -d` with `TENANT_COUNT=5`.
2. Verify all 5 database containers are healthy and Axyra Agent connects to all 5 logical replication slots.
3. Run bulk seeder to populate all 5 tenant databases.
4. Verify WAL events from all 5 databases stream into Axyra Analytics.
5. In Axyra Console:
   - Check **Tenant View**: Each tenant shows isolated order counts, revenue, and latencies.
   - Check **Platform View**: Global metrics aggregate GMV and orders across all 5 databases.
6. Verify changing `TENANT_COUNT` in `.env` seamlessly scales the number of monitored tenant databases.
