variable "project_id" {
  description = "Scaleway project that holds the namespace and the function."
  type        = string
}

variable "namespace_name" {
  description = "Name of the functions namespace, for example ssh-sentinel-dev."
  type        = string
}

variable "namespace_description" {
  type    = string
  default = "ssh-sentinel backend"
}

variable "name" {
  description = "Function name. It is part of the function URL."
  type        = string
  default     = "api"
}

variable "description" {
  type    = string
  default = "ssh-sentinel API, every route of contract v1"
}

variable "tags" {
  type    = list(string)
  default = []
}

variable "runtime" {
  description = "Scaleway Go runtime. Must be at least the Go version named in backend/go.mod. List the runtimes with: scw function runtime list"
  type        = string
  default     = "go124"
}

variable "handler" {
  description = "Directory of the adapter inside the zip, a slash, the exported function name (Scaleway Go format <folder>/<function>)."
  type        = string
  default     = "adapters/scaleway/api/Handle"
}

variable "source_dir" {
  description = "Path of the backend Go module, the folder that holds go.mod. Zipped as is."
  type        = string
}

variable "source_excludes" {
  description = "Glob patterns, relative to source_dir, left out of the zip. Build outputs and git metadata; vendor/ stays in when present."
  type        = list(string)
  default = [
    "bin",
    "bin/**",
    "dist",
    "dist/**",
    "*.zip",
    "**/*.zip",
    "*.exe",
    "**/*.exe",
    "*.test",
    "**/*.test",
    "*.out",
    "**/*.out",
    "coverage.*",
    ".git",
    ".git/**",
  ]
}

variable "zip_output_path" {
  description = "Where the zip is written. Null means build/<name>.zip next to the root module (gitignored)."
  type        = string
  default     = null
}

variable "timeout" {
  description = "Seconds a request may take. Keep it above the backend's verdict wait (25 s) plus margin."
  type        = number
  default     = 60
}

variable "memory_limit" {
  description = "Memory in MB, one of the Scaleway tiers (128, 256, 512, 1024, ...). CPU follows memory."
  type        = number
  default     = 256
}

variable "min_scale" {
  description = "Instances kept warm. 0 means a cold start on the login path after idle time; 1 avoids it and is billed continuously."
  type        = number
  default     = 0
}

variable "max_scale" {
  description = "Upper bound of instances. Every pending access request holds one instance for up to 25 s."
  type        = number
  default     = 5
}

variable "environment_variables" {
  description = "Plain environment variables handed to the backend."
  type        = map(string)
  default     = {}
}

variable "secret_environment_variables" {
  description = "Secret environment variables handed to the backend. The provider caps each value at 1000 characters."
  type        = map(string)
  default     = {}
  sensitive   = true
}
