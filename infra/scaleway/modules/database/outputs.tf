output "database_id" {
  value = scaleway_sdb_sql_database.this.id
}

output "database_name" {
  value = scaleway_sdb_sql_database.this.name
}

output "endpoint" {
  description = "postgres://host:port/dbname?sslmode=require, without credentials."
  value       = scaleway_sdb_sql_database.this.endpoint
}

output "connection_string" {
  description = "DATABASE_URL for the backend: endpoint plus the IAM credential."
  value       = local.connection_string
  sensitive   = true
}

output "application_id" {
  description = "IAM application of the backend. Also the database username."
  value       = scaleway_iam_application.backend.id
}

output "api_key_access_key" {
  value = scaleway_iam_api_key.backend.access_key
}

output "api_key_secret_key" {
  description = "Secret key of the backend's API key. Also the database password."
  value       = scaleway_iam_api_key.backend.secret_key
  sensitive   = true
}
