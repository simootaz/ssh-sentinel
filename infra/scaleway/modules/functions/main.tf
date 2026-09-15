# One Scaleway Serverless Function serving every route of the backend contract.
#
# Scaleway builds Go functions from source: the zip holds the whole backend Go
# module (go.mod at its root) and the platform compiles it on deploy. The
# handler is the exported Handle function of backend/adapters/scaleway/api,
# named "adapters/scaleway/api/Handle" (directory inside the zip, a slash, the
# function name).
#
# The zip is rebuilt from var.source_dir at every plan. Its sha256 is the
# zip_hash, so a backend code change shows up as an update of the function and
# is deployed by terraform apply. Nothing is built by hand.

locals {
  zip_path = coalesce(var.zip_output_path, "${path.root}/build/${var.name}.zip")
}

data "archive_file" "source" {
  type             = "zip"
  source_dir       = var.source_dir
  output_path      = local.zip_path
  output_file_mode = "0644"
  excludes         = var.source_excludes
}

resource "scaleway_function_namespace" "this" {
  name        = var.namespace_name
  description = var.namespace_description
  project_id  = var.project_id
  tags        = var.tags
}

resource "scaleway_function" "this" {
  namespace_id = scaleway_function_namespace.this.id
  name         = var.name
  description  = var.description
  tags         = var.tags

  runtime = var.runtime
  handler = var.handler

  # Agents and phones call the function directly. Authentication is the bearer
  # tokens checked by the backend, not a platform token, so the function is
  # public. Plain HTTP is redirected to HTTPS: the contract is HTTPS only.
  privacy     = "public"
  http_option = "redirected"

  # 60 s covers the backend's 25 s verdict wait with margin (docs/architecture.md, 3.2).
  timeout      = var.timeout
  memory_limit = var.memory_limit
  min_scale    = var.min_scale
  max_scale    = var.max_scale

  zip_file = data.archive_file.source.output_path
  zip_hash = data.archive_file.source.output_sha256
  deploy   = true

  environment_variables        = var.environment_variables
  secret_environment_variables = var.secret_environment_variables
}
