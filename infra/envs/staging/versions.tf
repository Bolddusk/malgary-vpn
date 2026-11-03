# Shared version pins. Each env symlinks or copies this file (see envs/*/versions.tf).
terraform {
  required_version = ">= 1.10.0"
  required_providers {
    digitalocean = {
      source  = "digitalocean/digitalocean"
      version = "~> 2.106"
    }
  }
}
