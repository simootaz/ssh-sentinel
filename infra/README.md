# infra

Two ways to run the backend. Both use the same code, the same environment variables and the same migrations.

| Folder | What | When to use |
|---|---|---|
| `scaleway/` | Terraform, Scaleway provider: functions namespace, the `api` function, Serverless SQL, Secret Manager, IAM key for the database. The reference deployment, the one we run | managed, pay per use, no server to keep |
| `docker-compose/` | `docker-compose.yml` with the backend container, PostgreSQL 16 and an optional Caddy reverse proxy for TLS. Any VM, any cloud, bare metal | full control, one box |

Other providers (AWS, GCP, Azure, Hetzner, ...) are additional folders here later, each next to a matching adapter under `backend/adapters/` when the provider needs one. The contract and the backend core do not change for that.

## scaleway/

Layout:

| Path | Creates |
|---|---|
| `modules/functions/` | functions namespace, the `api` function built from `backend/adapters/scaleway/api`, environment variables and secret references, 60 s timeout, memory, min/max scale |
| `modules/database/` | Serverless SQL (PostgreSQL) database, IAM application and API key used as database credentials |
| `modules/secrets/` | Secret Manager entries: database URL, admin token, FCM service account. Values supplied at apply time, never stored in the repo |
| `environments/dev/`, `environments/prod/` | root modules, one state each, in a Scaleway Object Storage bucket (S3-compatible backend) created once by hand |

Prerequisites: Terraform 1.6 or newer, a Scaleway project, an IAM API key with rights on Functions, Serverless SQL, Secret Manager, IAM and Object Storage. Credentials as environment variables, never in files (PowerShell):

```powershell
$env:SCW_ACCESS_KEY = "..."
$env:SCW_SECRET_KEY = "..."
$env:SCW_DEFAULT_ORGANIZATION_ID = "..."
$env:SCW_DEFAULT_PROJECT_ID = "..."
$env:SCW_DEFAULT_REGION = "fr-par"
$env:SCW_DEFAULT_ZONE = "fr-par-1"
```

Secret values (admin token, FCM service account JSON) are passed as `TF_VAR_...` variables or typed at the prompt. `*.tfvars` files are gitignored; only `*.tfvars.example` is committed.

Plan and apply:

```powershell
cd infra\scaleway\environments\dev
terraform init
terraform plan -out tfplan
terraform apply tfplan
```

Outputs include the function URL: this is the base URL for agents and phones. Terraform builds the function zip, so a backend code change is deployed with `terraform apply`. `min_scale = 1` on the function avoids cold starts in the login path; it is a per-environment variable.

Test: `terraform fmt -check -recursive`, `terraform validate`, then `terraform plan` against dev, which must show only the changes you expect. Apply to prod only from `main`, after dev works end to end.

## docker-compose/

Files:

| File | Purpose |
|---|---|
| `docker-compose.yml` | services `backend` (image built from `backend/Dockerfile`), `db` (PostgreSQL 16 with a named volume), `caddy` (optional, TLS with automatic certificates) |
| `.env.example` | every variable the stack needs, with comments; copy to `.env`, which is gitignored |
| `Caddyfile` | reverse proxy to the backend, used only when `caddy` is enabled |

Run:

```powershell
cd infra\docker-compose
copy .env.example .env         # fill in ADMIN_TOKEN, FCM values, DOMAIN
docker compose up -d
docker compose logs -f backend
```

The backend applies migrations on start, then serves on port 8080 inside the compose network. Agents and phones require HTTPS: expose the backend through Caddy (`docker compose --profile tls up -d`, with `DOMAIN` set and ports 80/443 open), or through Traefik, nginx or the cloud load balancer you already have.

Any PostgreSQL 14+ works instead of the bundled `db` service: set `DATABASE_URL` to the managed instance and drop the service.

Operations:

```powershell
docker compose exec db pg_dump -U sentinel sentinel > backup.sql   # backup
docker compose pull; docker compose up -d                           # upgrade
```

Test: `docker compose config` validates the file, `curl http://localhost:8080/healthz` checks the stack, then the smoke test from `backend/README.md`.
