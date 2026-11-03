variable "name" {
  description = "Pair name, e.g. gw-nyc-1. Members become gw-nyc-1a and gw-nyc-1b."
  type        = string
  validation {
    condition     = can(regex("^gw-[a-z]{3}-[0-9]+$", var.name))
    error_message = "Pair name must look like gw-nyc-1."
  }
}

variable "cell" {
  type = string
}

variable "region" {
  type = string
}

variable "vpc_id" {
  type = string
}

variable "size" {
  description = "Droplet size slug for both members. Pilot: s-2vcpu-2gb."
  type        = string
  default     = "s-2vcpu-2gb"
}

variable "image" {
  type    = string
  default = "ubuntu-24-04-x64"
}

variable "ssh_key_ids" {
  type = list(string)
}

variable "domain" {
  type = string
}
