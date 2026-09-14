output "secret_ids" {
  description = "Regional ids (region/uuid) keyed by secret name, for Terraform references."
  value       = { for k, s in scaleway_secret.this : k => s.id }
}

output "secret_uuids" {
  description = "Bare uuids keyed by secret name, the form the Secret Manager API expects."
  value       = { for k, s in scaleway_secret.this : k => element(reverse(split("/", s.id)), 0) }
}

output "secret_paths" {
  description = "Full path of each secret, as shown in the console."
  value       = { for k, s in scaleway_secret.this : k => "${var.path}${k}" }
}
