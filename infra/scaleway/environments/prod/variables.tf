# Non-secret values go in terraform.tfvars (copy terraform.tfvars.example).
# Secrets are passed as TF_VAR_admin_token and TF_VAR_fcm_service_account_json,
# or typed at the prompt. Never in a committed file.

variable "project_id" {
  description = "Scaleway project for this environment, created by hand in the console."
  type        = string
}

variable "organization_id" {
  description = "Scaleway organization. Null reads SCW_DEFAULT_ORGANIZATION_ID."
  type        = string
  default     = null
}

variable "region" {
  description = "Region for the function, the database and the secrets."
  type        = string
  default     = "fr-par"
}

variable "name_prefix" {
  description = "Prefix of every resource name."
  type        = string
  default     = "ssh-sentinel-prod"
}

variable "tags" {
  type    = list(string)
  default = ["ssh-sentinel", "prod"]
}

# Function

variable "function_runtime" {
  description = "Scaleway Go runtime, at least the Go version in backend/go.mod. List them with: scw function runtime list"
  type        = string
  default     = "go124"
}

variable "function_timeout" {
  description = "Seconds a request may take. Above the 25 s verdict wait with margin."
  type        = number
  default     = 60
}

variable "function_memory_limit" {
  description = "Memory in MB, one of the Scaleway tiers."
  type        = number
  default     = 256
}

variable "function_min_scale" {
  description = "Instances kept warm. 0 for dev; 1 in prod to keep cold starts out of the login path."
  type        = number
  default     = 1
}

variable "function_max_scale" {
  description = "Instance ceiling. Every pending access request holds one instance for up to 25 s."
  type        = number
  default     = 10
}

# Database

variable "db_min_cpu" {
  description = "vCPU kept allocated. 0 lets the database sleep when idle, at the price of a few seconds on the first query."
  type        = number
  default     = 0
}

variable "db_max_cpu" {
  description = "vCPU ceiling for autoscaling."
  type        = number
  default     = 4
}

# Backend configuration, names and defaults from backend/README.md

variable "admin_token" {
  description = "Bearer token shared by the phones. 32 random bytes, base64url. Pass it as TF_VAR_admin_token."
  type        = string
  sensitive   = true

  validation {
    condition     = length(var.admin_token) >= 32
    error_message = "admin_token must be at least 32 characters. Generate it as described in infra/README.md."
  }
}

variable "fcm_project_id" {
  description = "Firebase project id (Firebase console, Project settings, General)."
  type        = string
}

variable "fcm_service_account_json" {
  description = "Content of the Firebase service account key file. Pass it as TF_VAR_fcm_service_account_json."
  type        = string
  sensitive   = true

  validation {
    condition     = try(jsondecode(var.fcm_service_account_json).type, "") == "service_account"
    error_message = "fcm_service_account_json must be the JSON key file of a Firebase service account (type = service_account)."
  }
}

variable "verdict_wait_seconds" {
  description = "How long access-request waits for a verdict. Keep below the agent's 30 s budget."
  type        = number
  default     = 25
}

variable "autoblock_threshold" {
  description = "Admin denials from one IP that trigger a block."
  type        = number
  default     = 3
}

variable "autoblock_window_seconds" {
  description = "Window in which those denials are counted."
  type        = number
  default     = 3600
}

variable "autoblock_duration_seconds" {
  description = "How long a block lasts. 0 means until unblocked from the app."
  type        = number
  default     = 0
}

variable "geo_lookup_url" {
  description = "Geolocation lookup endpoint. Empty disables geo, and geo rules then never match."
  type        = string
  default     = ""
}

variable "run_migrations" {
  description = "Apply backend/migrations from this machine during apply (needs Go). Set to false when the backend applies them itself."
  type        = bool
  default     = true
}
