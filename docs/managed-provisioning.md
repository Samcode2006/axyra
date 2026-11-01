# Managed PostgreSQL provisioning (local Docker)

In Tenant Management, choose **Create a managed PostgreSQL database** when
registering a tenant. Existing-database registration remains the default.
The API returns immediately. The tenant displays provisioning progress, then
becomes active once logical replication is streaming and the agent has sent an
authenticated heartbeat. Empty databases need no synthetic orders to become ready.

This provider runs on Docker Desktop with Linux containers. It provisions the
project's reference `orders` schema, not an arbitrary customer's application schema.
It does not deploy a customer application or generate seed data.

## Enable on another installation

Set these values in the ignored `deploy/.env`:

```dotenv
MANAGED_PROVISIONING_ENABLED=true
PROVISIONING_KEY=<64 random hexadecimal characters>
```

Generate the key once with a cryptographically secure generator. Keep it with
encrypted backups of the platform database. Changing or losing the key makes
existing managed credentials unreadable. The current local installation has
already been configured; do not replace its key.

From the repository root in PowerShell:

```powershell
docker compose -f .\deploy\docker-compose.yaml --profile managed build axyra-agent
docker compose -f .\deploy\docker-compose.yaml --profile managed up -d --build
```

The worker needs the `axyra-managed-agent:local` image locally. Its only Docker
socket mount is on the provisioner service, not the web or control-plane service.
Docker socket access grants host-level container control: run this worker only
on a trusted local deployment. No socket or provisioning command is exposed via HTTP.

## What is created

- A tenant record and durable provisioning job in one transaction.
- PostgreSQL 16 container `axyra-managed-<tenant_id>` and named volume
  `axyra-managed-<tenant_id>-data`, both labeled with their tenant ownership.
- A dedicated application, agent, hashed agent token and tenant mapping.
- A dedicated agent container `axyra-managed-<tenant_id>-agent`.
- Separate random passwords for bootstrap administration, replication and
  application writes. Persisted secrets use AES-256-GCM bound to the tenant ID.
- A replication-only account with table read access, plus a non-superuser writer.

Containers use the Compose network and publish no database or agent host ports.
Each database is limited to 384 MiB and 0.5 CPU; its agent to 128 MiB and 0.25 CPU.
Creation is limited to five managed databases per organization and twenty per
deployment, including failed jobs with retained resources. These are local
resource limits, not disk quotas or billing enforcement.

## Connection for an application

The UI displays the private database host, port and database name, never passwords.
A local operator with Docker access can retrieve the application's writer DSN:

```powershell
docker compose -f .\deploy\docker-compose.yaml --profile managed exec -T provisioner /usr/local/bin/axyra-provisioner -connection tenant_REPLACE_ME
```

This command displays a secret. Store it in the application's secret configuration,
not in frontend code or logs. Retrieval is recorded in the audit log. The hostname
is reachable inside the Compose network; it is not a Windows localhost address.
The application must join that network or use an operator-managed secure connection.
Writer access covers SELECT, INSERT, UPDATE and DELETE on `orders`; it cannot
change schema or administer PostgreSQL.

## Recovery and lifecycle

Stages are `queued`, `creating_database`, `preparing_schema`, `registering_agent`,
`waiting_for_stream`, and `ready`. A failed job retains its resources and exposes a
Retry button. Requests are organization-scoped. Retries reuse credentials and
owned resources. A restarted worker resumes unfinished jobs. Only one worker
holds the provisioning leader lock. If the agent image has changed, retry replaces
the stateless agent container; it retains the PostgreSQL container and volume.

Ordinary tenant deletion is blocked for managed tenants. Suspension/deactivation
does not delete or stop Docker resources. Automated backup scheduling, credential
rotation propagation, managed upgrades and automated decommissioning are not
implemented in this local provider.

Before decommissioning, an operator must stop the agent, back up and verify the
database, decide on retention, and remove only resources whose ownership labels
match that tenant. The platform provisioning record intentionally prevents a
tenant, agent or organization deletion from silently abandoning its database.
Do not remove the provisioning record just to bypass that protection.

Managed resources are created outside Compose's static service list. They have
`unless-stopped` restart policies and persistent volumes; `compose down` does
not remove them. Back up both source data and platform metadata plus the key.

## API and checks

- `POST /v1/tenants` accepts `database_mode: "managed"` and returns 202.
  Omitted or `"existing"` keeps the previous registration behavior and returns 201.
- `GET /v1/tenants/{id}/provisioning` returns progress, safe connection metadata
  and an actionable error; external tenants return `status: "external"`.
- `POST /v1/tenants/{id}/provisioning/retry` retries only failed jobs, returning 202.
- `POST /v1/agent/heartbeat` requires an agent token and records connectivity
  independently of order activity.

Run Go tests in `services/control-plane`, `services/analytics` and `agent`.
For integration tests, point `TEST_PROVISION_DATABASE_URL` and
`TEST_PUBLICATION_DATABASE_URL` at a disposable PostgreSQL database, then run the
control-plane and agent tests respectively. Tests create fixtures and migrations.
Never point these variables at a production database.
