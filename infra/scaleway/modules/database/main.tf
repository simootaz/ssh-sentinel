# Serverless SQL database plus the identity the backend uses to reach it.
#
# Serverless SQL authenticates with IAM: the username is an IAM application id,
# the password is the secret key of one of its API keys. The same key is also
# the backend's Scaleway credential for Secret Manager (see the secrets module),
# so the backend has exactly one identity to revoke or rotate.

resource "scaleway_sdb_sql_database" "this" {
  name       = var.name
  project_id = var.project_id
  min_cpu    = var.min_cpu
  max_cpu    = var.max_cpu
}

resource "scaleway_iam_application" "backend" {
  name        = var.application_name
  description = "ssh-sentinel backend: database credential and Secret Manager access"
}

resource "scaleway_iam_policy" "database" {
  name           = "${var.application_name}-database"
  description    = "Read and write on the Serverless SQL databases of the project"
  application_id = scaleway_iam_application.backend.id

  rule {
    project_ids          = [var.project_id]
    permission_set_names = ["ServerlessSQLDatabaseReadWrite"]
  }
}

resource "scaleway_iam_api_key" "backend" {
  application_id = scaleway_iam_application.backend.id
  description    = "${var.application_name}: database password and API credential"
}

locals {
  # Scaleway exports postgres://host:5432/dbname?sslmode=require. The credential goes in front.
  connection_string = format(
    "postgres://%s:%s@%s",
    scaleway_iam_application.backend.id,
    scaleway_iam_api_key.backend.secret_key,
    trimprefix(scaleway_sdb_sql_database.this.endpoint, "postgres://"),
  )
}
