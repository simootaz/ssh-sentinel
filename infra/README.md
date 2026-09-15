# infra

Two ways to run the backend. Both use the same code, the same environment variables and the same migrations.

| Folder | What | When to use |
|---|---|---|
| `scaleway/` | Terraform, Scaleway provider: functions namespace, the `api` function, Serverless SQL, Secret Manager, IAM identity for the backend. The reference deployment, the one we run | managed, pay per use, no server to keep |
| `docker-compose/` | `docker-compose.yml` with the backend container, PostgreSQL 16 and an optional Caddy reverse proxy for TLS. Any VM, any cloud, bare metal | full control, one box |

Other providers (AWS, GCP, Azure, Hetzner, ...) are additional folders here later, each next to a matching adapter under `backend/adapters/` when the provider needs one. The contract and the backend core do not change for that.

## scaleway/

Layout:

| Path | Creates |
|---|---|
| `modules/functions/` | functions namespace and the `api` function: public, HTTP redirected to HTTPS, 60 s timeout, memory and min/max scale as variables, plain and secret environment variables. Zips `backend/` at every plan and deploys it when the hash changes |
| `modules/database/` | Serverless SQL database, the backend's IAM application, its API key (the database credential) and the policy that grants it read/write on the databases of the project |
| `modules/secrets/` | Secret Manager entries `database-url`, `admin-token` and `fcm-service-account` under `/ssh-sentinel/<env>/`, and the policy letting the backend read secret data. Values supplied at apply time, never stored in the repo |
| `environments/dev/`, `environments/prod/` | root modules, one state each in an Object Storage bucket (S3-compatible backend), one Scaleway project each, own sizing. Also the migration step |

Scaleway builds Go functions from source: the zip is the whole backend Go module (`go.mod` at its root), the handler is `adapters/scaleway/api/Handle`, and the platform compiles it on deploy. There is no binary to build by hand. One step before `terraform apply`: `go mod vendor` in `backend\`. The dashboard (`web/`, a sibling module the backend embeds) is outside the zip otherwise; vendored, it travels in `vendor/`, which the zip keeps and git ignores. The Go runtime (`function_runtime`, default `go124`) must be at least the `go` version named in `backend/go.mod`; `scw function runtime list` shows what is available.

### Created by hand, created by Terraform

By hand, once, in the Scaleway console and in the Firebase console. Nothing else is created by hand.

| # | What | Where | Used as |
|---|---|---|---|
| 1 | One project per environment, `ssh-sentinel-dev` and `ssh-sentinel-prod`. IAM policies are scoped per project, so this keeps the dev credentials away from prod | Scaleway console > Organization > Projects | `project_id` in `terraform.tfvars` |
| 2 | An IAM application for Terraform (or your own user) with one policy: `FunctionsFullAccess`, `ServerlessSQLDatabaseFullAccess`, `SecretManagerFullAccess` and `ObjectStorageFullAccess` on the two projects, plus `IAMManager` on the organization so it can create the backend's application, policies and API key | Scaleway console > IAM > Applications, then Policies | |
| 3 | An API key for that application. Its preferred project must be the project that holds the state bucket: Object Storage picks the project from the key, not from the bucket name | Scaleway console > IAM > API keys | `SCW_ACCESS_KEY` and `SCW_SECRET_KEY`; the same two values as `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` for the state backend |
| 4 | One private Object Storage bucket for the Terraform state, region `fr-par`, versioning on. Shared by dev and prod, one state key each. The state contains the secrets below, hence private | Scaleway console > Object Storage > Create bucket | `bucket` in `backend.hcl` |
| 5 | Firebase project: copy the Project ID | Firebase console > Project settings > General | `fcm_project_id` in `terraform.tfvars` |
| 6 | Firebase service account key: Generate new private key, save the JSON file outside the repository | Firebase console > Project settings > Service accounts | `TF_VAR_fcm_service_account_json` |
| 7 | The admin token, generated on your machine (command below). Keep it: the phones need it | | `TF_VAR_admin_token` |

Terraform creates, per environment:

| Resource | Name | Module |
|---|---|---|
| Functions namespace | `ssh-sentinel-<env>` | functions |
| Function `api`, public, zip built from `backend/`, environment and secret environment variables | `ssh-sentinel-<env>/api` | functions |
| Serverless SQL database | `ssh-sentinel-<env>-db` | database |
| IAM application, its API key, policy `ServerlessSQLDatabaseReadWrite` on the project | `ssh-sentinel-<env>-backend` | database |
| Secrets `database-url`, `admin-token`, `fcm-service-account`, one version each | `/ssh-sentinel/<env>/` | secrets |
| IAM policy `SecretManagerSecretAccess` on the project, for the backend application | `ssh-sentinel-<env>-secrets-access` | secrets |
| Migration step: `go run ./cmd/server migrate` on your machine against the new database, re-run when a migration file changes | | environment |

Not created by anything here: server tokens (enrollment script, `docs/architecture.md` section 6) and phones (`POST /devices` from the app).

### Prerequisites

Terraform 1.6 or newer, Go 1.22 or newer for the migration step (`run_migrations = false` skips it), and the credentials as environment variables, never in files (PowerShell):

```powershell
$env:SCW_ACCESS_KEY = "..."                       # API key from step 3
$env:SCW_SECRET_KEY = "..."
$env:SCW_DEFAULT_ORGANIZATION_ID = "..."          # IAM applications and policies are organization-level
$env:AWS_ACCESS_KEY_ID = $env:SCW_ACCESS_KEY      # the S3 state backend reads these two
$env:AWS_SECRET_ACCESS_KEY = $env:SCW_SECRET_KEY
```

Project id and region are in `terraform.tfvars`, not in the environment.

The two secrets are passed as `TF_VAR_` variables, typed at the prompt, or put in a gitignored `secrets.auto.tfvars`. `*.tfvars` files are gitignored; only `*.tfvars.example` is committed.

```powershell
$b = [byte[]]::new(32); [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b)
$env:TF_VAR_admin_token = [Convert]::ToBase64String($b).TrimEnd('=').Replace('+', '-').Replace('/', '_')
$env:TF_VAR_admin_token                            # copy it somewhere safe, the phones need it
$env:TF_VAR_fcm_service_account_json = Get-Content -Raw C:\secrets\firebase-adminsdk.json
```

### First deployment

```powershell
cd infra\scaleway\environments\dev
copy backend.hcl.example backend.hcl              # bucket name, step 4
copy terraform.tfvars.example terraform.tfvars    # project id, Firebase project id, sizing
terraform init -backend-config=backend.hcl
terraform plan -out tfplan
terraform apply tfplan
terraform output api_url
```

What apply does, in order: the database and the backend's IAM identity; the three secrets; `go run ./cmd/server migrate` from your machine against the new database; the zip of `backend/`, its upload and the Scaleway build, which takes a few minutes for Go and which apply waits for. `api_url` is the base URL for agents and phones. Then enroll the servers with the enrollment script from `backend/`, pointed at `terraform output -raw database_connection_string`.

Same steps for prod from `environments\prod`, only from `main`, after dev works end to end.

### What the function receives

| Variable | Kind | Value |
|---|---|---|
| `DATABASE_URL` | secret | `postgres://<application id>:<api key secret>@<host>:5432/<db>?sslmode=require` |
| `ADMIN_TOKEN` | secret | `TF_VAR_admin_token` |
| `SCW_SECRET_KEY` | secret | secret key of the backend's API key |
| `SCW_ACCESS_KEY`, `SCW_DEFAULT_PROJECT_ID`, `SCW_DEFAULT_REGION` | plain | the rest of the backend's Scaleway identity, under the names the Scaleway SDK and CLI read by default |
| `FCM_PROJECT_ID` | plain | `fcm_project_id` |
| `FCM_SERVICE_ACCOUNT_SECRET_ID` | plain | uuid of the `fcm-service-account` secret |
| `VERDICT_WAIT_SECONDS`, `AUTOBLOCK_THRESHOLD`, `AUTOBLOCK_WINDOW_SECONDS`, `AUTOBLOCK_DURATION_SECONDS`, `GEO_LOOKUP_URL` | plain | `terraform.tfvars` |

`FCM_SERVICE_ACCOUNT_JSON` is not set on Scaleway. The Terraform provider caps a function environment value at 1000 characters (the platform allows 64 KiB since June 2026, the provider has not followed yet) and a Firebase key is about 2.3 KB. The value sits in Secret Manager and the Scaleway adapter reads it once at start, with the identity above:

```
GET https://api.scaleway.com/secret-manager/v1beta1/regions/{SCW_DEFAULT_REGION}/secrets/{FCM_SERVICE_ACCOUNT_SECRET_ID}/versions/latest/access
X-Auth-Token: {SCW_SECRET_KEY}
```

The answer is JSON with a `data` field holding the key file in base64. Decode it and hand it to the core as `FCM_SERVICE_ACCOUNT_JSON`. Nothing else changes in the backend: `DATABASE_URL` and `ADMIN_TOKEN` arrive as ordinary variables. This is the one Scaleway-specific step of the adapter besides forwarding `Handle`. To be confirmed with the backend owner; the day the provider lifts its cap, `FCM_SERVICE_ACCOUNT_JSON` can be added to `secret_environment_variables` in `environments/*/main.tf` and the lookup dropped.

### Day to day

| Change | What plan shows | How |
|---|---|---|
| Backend code | `zip_hash` update on the function | `terraform apply` |
| New migration file | `terraform_data.migrate` replaced, then the function updated | `terraform apply`; the migration runs before the deploy |
| Admin token rotation | new `admin-token` version, function update | new `TF_VAR_admin_token`, apply, update the phones |
| Firebase key rotation | new `fcm-service-account` version | new `TF_VAR_fcm_service_account_json`, apply |
| Database credential rotation | API key replaced, `DATABASE_URL` updated everywhere | `terraform apply -replace=module.database.scaleway_iam_api_key.backend` |
| Sizing | function or database updated in place | edit `terraform.tfvars`, apply |
| Remove everything | | `terraform destroy`; the project and the state bucket stay |

The state holds every secret above. It lives in the private bucket only; never copy it elsewhere.

### Test

Without credentials:

```powershell
cd infra\scaleway
terraform fmt -check -recursive
cd environments\dev;  terraform init -backend=false; terraform validate
cd ..\prod;           terraform init -backend=false; terraform validate
```

`terraform init -backend=false` downloads the providers pinned in `.terraform.lock.hcl` (committed on purpose, with hashes for Windows, Linux and macOS) and leaves the state alone. Then `terraform plan` against dev, which must show only the changes you expect. Apply to prod only from `main`, after dev works end to end.

## docker-compose/

Files:

| File | Purpose |
|---|---|
| `docker-compose.yml` | services `backend` (image built from `backend/Dockerfile` with the repository root as context, since the binary embeds the dashboard from `web/`; or `BACKEND_IMAGE` from a registry), `db` (PostgreSQL 16, named volume `pgdata`, healthcheck), `caddy` (profile `tls`, automatic certificates, ports 80 and 443) |
| `.env.example` | every variable the stack reads, with comments; copy to `.env`, which is gitignored |
| `Caddyfile` | reverse proxy to `backend:8080` for `DOMAIN`, used only when the `tls` profile is on |

Run:

```powershell
cd infra\docker-compose
copy .env.example .env         # POSTGRES_PASSWORD, ADMIN_TOKEN, FCM_PROJECT_ID, FCM_SERVICE_ACCOUNT_JSON, DOMAIN
docker compose up -d --build
docker compose logs -f backend
```

The backend applies the migrations on start, then serves on port 8080 inside the compose network, published on `127.0.0.1:8080` only. Agents and phones require HTTPS. Either enable Caddy, with `DOMAIN` pointing at the machine and ports 80 and 443 open:

```powershell
docker compose --profile tls up -d
```

or keep the `tls` profile off and put Traefik, nginx or the cloud load balancer you already have in front of `127.0.0.1:8080`. With `DOMAIN=localhost` Caddy serves a self-signed certificate, enough for a local test with `curl -k`.

The Firebase key goes in `.env` on one line between single quotes; the comment above `FCM_SERVICE_ACCOUNT_JSON` in `.env.example` has the one-liner that produces it from the downloaded file. Single quotes matter: they keep the `\n` sequences inside `private_key` as two characters, which the JSON needs.

Any PostgreSQL 14+ works instead of the bundled `db` service: set `DATABASE_URL` in `.env`, then remove the `db` service and the `depends_on` block from `docker-compose.yml`. With TLS between the backend and a managed database, keep `sslmode=require` in the URL.

Operations:

```powershell
docker compose exec db pg_dump -U sentinel sentinel > backup.sql        # backup
git pull; docker compose build --pull; docker compose up -d              # upgrade, image built here
docker compose pull backend; docker compose up -d                        # upgrade, BACKEND_IMAGE from a registry
docker compose down                                                      # stop, keeps the volumes
```

Test: `docker compose config` validates the file and the `.env` (it stops with a message naming the first required variable that is empty), `curl http://localhost:8080/healthz` checks the stack, then the smoke test from `backend/README.md`.
