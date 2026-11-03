variable "cell" {
  type    = string
  default = "c01"
}

variable "region" {
  type    = string
  default = "nyc1"
}

variable "vpc_ip_range" {
  type = string
}

variable "domain" {
  description = "DigitalOcean DNS zone: a subdomain delegated from malgary.com (research R14)."
  type        = string
}

variable "backup_region" {
  type    = string
  default = "ams3"
}

variable "ssh_key_ids" {
  type = list(string)
}

variable "control_host_count" {
  type    = number
  default = 1
}

variable "control_host_size" {
  type = string
}

variable "db_volume_gb" {
  type    = number
  default = 50
}

variable "gateway_pairs" {
  description = "Map of pair name to region and size. Add a pair to scale out or add a region."
  type = map(object({
    region = string
    size   = string
  }))
}

variable "relay_enabled" {
  type    = bool
  default = false
}
