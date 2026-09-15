variable "project_id" {
  description = "Scaleway project that holds the secrets."
  type        = string
}

variable "path" {
  description = "Folder of the secrets in Secret Manager, for example /ssh-sentinel/dev/."
  type        = string
  default     = "/ssh-sentinel/"
}

variable "name_prefix" {
  description = "Prefix for the IAM policy name."
  type        = string
}

variable "tags" {
  type    = list(string)
  default = []
}

variable "database_url" {
  description = "PostgreSQL connection string, stored as the database-url secret."
  type        = string
  sensitive   = true
}

variable "admin_token" {
  description = "Bearer token shared by the phones, stored as the admin-token secret."
  type        = string
  sensitive   = true
}

variable "fcm_service_account_json" {
  description = "Firebase service account key file content, stored as the fcm-service-account secret."
  type        = string
  sensitive   = true
}

variable "create_reader_policy" {
  description = "Create an IAM policy letting reader_application_id read secret version data in the project. Must be known at plan time, hence a separate flag."
  type        = bool
  default     = false
}

variable "reader_application_id" {
  description = "IAM application that receives the reader policy, the backend's identity."
  type        = string
  default     = null
}
