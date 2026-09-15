# State in a Scaleway Object Storage bucket, through the S3-compatible backend.
#
# The bucket name, region and endpoint live in backend.hcl (copy
# backend.hcl.example, gitignored), so this file is the same for everyone:
#
#   terraform init -backend-config=backend.hcl
#
# The S3 backend reads AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY: set them to
# the Scaleway access key and secret key before init.

terraform {
  backend "s3" {
    key = "ssh-sentinel/prod/terraform.tfstate"

    # Object Storage is not AWS: no STS, no account id, no region catalogue and
    # no checksum trailer on uploads.
    skip_credentials_validation = true
    skip_region_validation      = true
    skip_requesting_account_id  = true
    skip_s3_checksum            = true
  }
}
