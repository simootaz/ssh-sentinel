# Secret Manager entries for the backend. Terraform writes the values it is
# given at apply time; nothing is stored in the repository.
#
# DATABASE_URL and ADMIN_TOKEN are short: the environment root hands them to
# the function as secret environment variables directly. The Firebase service
# account JSON is about 2.3 KB and the Scaleway provider caps a function
# environment value at 1000 characters, so the function receives the id of the
# fcm-service-account secret instead and reads its value through the Secret
# Manager API when it starts. The reader policy below grants exactly that
# (SecretManagerSecretAccess: read version data, nothing else).

locals {
  secrets = {
    "database-url"        = "PostgreSQL connection string used by the backend (DATABASE_URL)"
    "admin-token"         = "Bearer token accepted from the phones (ADMIN_TOKEN)"
    "fcm-service-account" = "Firebase service account key, JSON, used to send pushes (FCM_SERVICE_ACCOUNT_JSON)"
  }

  # Kept apart from local.secrets: for_each must not iterate over sensitive values.
  values = {
    "database-url"        = var.database_url
    "admin-token"         = var.admin_token
    "fcm-service-account" = var.fcm_service_account_json
  }
}

resource "scaleway_secret" "this" {
  for_each = local.secrets

  name        = each.key
  path        = var.path
  description = each.value
  project_id  = var.project_id
  tags        = var.tags
}

resource "scaleway_secret_version" "this" {
  for_each = local.secrets

  secret_id   = scaleway_secret.this[each.key].id
  data        = local.values[each.key]
  description = "managed by terraform"
}

resource "scaleway_iam_policy" "reader" {
  count = var.create_reader_policy ? 1 : 0

  name           = "${var.name_prefix}-secrets-access"
  description    = "Read secret version data in the project"
  application_id = var.reader_application_id

  rule {
    project_ids          = [var.project_id]
    permission_set_names = ["SecretManagerSecretAccess"]
  }
}
