# dev environment: one function, one database, three secrets.
# prod is the same wiring with its own state, project and sizing.

locals {
  # environments/dev -> environments -> scaleway -> infra -> repository root
  backend_dir = abspath("${path.root}/../../../../backend")

  # Migrations are re-applied when a migration file changes or the database is recreated.
  migrations_hash = sha256(join("\n", [
    for f in sort(fileset("${local.backend_dir}/migrations", "*.sql")) :
    filesha256("${local.backend_dir}/migrations/${f}")
  ]))
}

module "database" {
  source = "../../modules/database"

  project_id       = var.project_id
  name             = "${var.name_prefix}-db"
  application_name = "${var.name_prefix}-backend"
  min_cpu          = var.db_min_cpu
  max_cpu          = var.db_max_cpu
}

module "secrets" {
  source = "../../modules/secrets"

  project_id  = var.project_id
  name_prefix = var.name_prefix
  path        = "/ssh-sentinel/dev/"
  tags        = var.tags

  database_url             = module.database.connection_string
  admin_token              = var.admin_token
  fcm_service_account_json = var.fcm_service_account_json

  # The backend reads the fcm-service-account secret at start.
  create_reader_policy  = true
  reader_application_id = module.database.application_id
}

# Applies backend/migrations to the database from this machine with the
# backend's own binary, before the function is deployed. Needs Go on this
# machine; the database is reachable from the internet with the IAM credential.
resource "terraform_data" "migrate" {
  count = var.run_migrations ? 1 : 0

  triggers_replace = [
    module.database.database_id,
    local.migrations_hash,
  ]

  provisioner "local-exec" {
    working_dir = local.backend_dir
    command     = "go run ./cmd/server migrate"
    environment = {
      DATABASE_URL = module.database.connection_string
    }
  }
}

module "functions" {
  source = "../../modules/functions"

  project_id     = var.project_id
  namespace_name = var.name_prefix
  name           = "api"
  tags           = var.tags

  source_dir   = local.backend_dir
  runtime      = var.function_runtime
  timeout      = var.function_timeout
  memory_limit = var.function_memory_limit
  min_scale    = var.function_min_scale
  max_scale    = var.function_max_scale

  # What the backend reads, names from backend/README.md. The SCW_* variables
  # are the standard names of the Scaleway SDK and CLI: the adapter uses them
  # to fetch FCM_SERVICE_ACCOUNT_JSON from Secret Manager (see infra/README.md).
  environment_variables = {
    FCM_PROJECT_ID                = var.fcm_project_id
    FCM_SERVICE_ACCOUNT_SECRET_ID = module.secrets.secret_uuids["fcm-service-account"]
    SCW_ACCESS_KEY                = module.database.api_key_access_key
    SCW_DEFAULT_PROJECT_ID        = var.project_id
    SCW_DEFAULT_REGION            = var.region
    VERDICT_WAIT_SECONDS          = tostring(var.verdict_wait_seconds)
    AUTOBLOCK_THRESHOLD           = tostring(var.autoblock_threshold)
    AUTOBLOCK_WINDOW_SECONDS      = tostring(var.autoblock_window_seconds)
    AUTOBLOCK_DURATION_SECONDS    = tostring(var.autoblock_duration_seconds)
    GEO_LOOKUP_URL                = var.geo_lookup_url
  }

  secret_environment_variables = {
    DATABASE_URL   = module.database.connection_string
    ADMIN_TOKEN    = var.admin_token
    SCW_SECRET_KEY = module.database.api_key_secret_key
  }

  # Deploy after the schema is in place. Data sources inside the module are
  # only deferred to apply time when the migration step has pending changes,
  # so a plain code change still shows a fully known plan.
  depends_on = [terraform_data.migrate]
}
