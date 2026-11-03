variable "code" {
  description = "Cell code, e.g. c01."
  type        = string
  validation {
    condition     = can(regex("^c[0-9]{2}$", var.code))
    error_message = "Cell code must look like c01."
  }
}

variable "region" {
  description = "DigitalOcean region for the cell, e.g. nyc1."
  type        = string
}

variable "ip_range" {
  description = "VPC CIDR. Also the only source allowed to reach SSH."
  type        = string
  default     = "10.10.0.0/16"
}

variable "wg_port_range" {
  description = "UDP port range for per-tenant routing peers on gateways."
  type        = string
  default     = "51820-52820"
}

variable "domain" {
  description = "DNS zone in DigitalOcean, a delegated subdomain such as vpn.malgary.com (research R14)."
  type        = string
}

variable "control_ingress_ip" {
  description = "Public IPv4 that serves *.vpn, id and api (the control host in the pilot)."
  type        = string
}

variable "backup_region" {
  description = "Spaces region for backups, outside the cell region (FR-025)."
  type        = string
  default     = "ams3"
}

variable "backup_retention_days" {
  type    = number
  default = 30
}
