output "namespace_id" {
  value = scaleway_function_namespace.this.id
}

output "function_id" {
  value = scaleway_function.this.id
}

output "domain_name" {
  description = "Native domain of the function. TLS is terminated by Scaleway."
  value       = scaleway_function.this.domain_name
}

output "url" {
  description = "Base URL for agents and phones. The contract routes are appended to it."
  value       = "https://${scaleway_function.this.domain_name}"
}

output "zip_sha256" {
  value = data.archive_file.source.output_sha256
}

output "zip_size_bytes" {
  value = data.archive_file.source.output_size
}
