# Remote state in DigitalOcean Spaces (S3-compatible). Credentials come from
# AWS_ACCESS_KEY_ID / AWS_SECRET_ACCESS_KEY set to the Spaces key, never from this file.
terraform {
  backend "s3" {
    bucket                      = "malgary-tofu-state"
    key                         = "staging/terraform.tfstate"
    region                      = "us-east-1" # ignored by Spaces, required by the backend
    endpoints                   = { s3 = "https://ams3.digitaloceanspaces.com" }
    skip_credentials_validation = true
    skip_requesting_account_id  = true
    skip_metadata_api_check     = true
    skip_region_validation      = true
    skip_s3_checksum            = true
    use_path_style              = false
    use_lockfile                = true
  }
}
