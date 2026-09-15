variable "project_id" {
  description = "Scaleway project that holds the database. The IAM policy is scoped to it."
  type        = string
}

variable "name" {
  description = "Database name, shown in the console and part of the endpoint."
  type        = string
}

variable "application_name" {
  description = "Name of the IAM application that owns the database credential."
  type        = string
}

variable "min_cpu" {
  description = "vCPU kept allocated. 0 lets the database sleep when idle: the first query after a pause takes a few seconds, taken from the 30 s login budget."
  type        = number
  default     = 0
}

variable "max_cpu" {
  description = "vCPU ceiling for autoscaling."
  type        = number
  default     = 4
}
