provider "scaleway" {
  # Access key and secret key come from SCW_ACCESS_KEY and SCW_SECRET_KEY.
  organization_id = var.organization_id
  project_id      = var.project_id
  region          = var.region
  zone            = "${var.region}-1"
}
