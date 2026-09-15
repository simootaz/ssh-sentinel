output "api_url" {
  description = "Base URL for the agents (backend URL in the agent config) and the phones (settings screen)."
  value       = module.functions.url
}

output "function_id" {
  value = module.functions.function_id
}

output "namespace_id" {
  value = module.functions.namespace_id
}

output "zip_sha256" {
  description = "Hash of the deployed backend zip. Changes when the backend code changes."
  value       = module.functions.zip_sha256
}

output "database_endpoint" {
  description = "Database endpoint without credentials."
  value       = module.database.endpoint
}

output "database_connection_string" {
  description = "DATABASE_URL, for the enrollment script and manual migrations: terraform output -raw database_connection_string"
  value       = module.database.connection_string
  sensitive   = true
}

output "backend_application_id" {
  description = "IAM application of the backend: database username and Secret Manager reader."
  value       = module.database.application_id
}

output "secret_ids" {
  description = "Secret Manager ids by name. fcm-service-account is what the function reads at start."
  value       = module.secrets.secret_uuids
}

output "secret_paths" {
  value = module.secrets.secret_paths
}
