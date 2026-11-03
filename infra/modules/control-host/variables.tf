variable "cell" {
  type = string
}

variable "index" {
  description = "Host number within the cell (1, 2, ...)."
  type        = number
  default     = 1
}

variable "region" {
  type = string
}

variable "vpc_id" {
  type = string
}

variable "size" {
  description = "Droplet size slug. Pilot: s-4vcpu-8gb (research R11)."
  type        = string
  default     = "s-4vcpu-8gb"
}

variable "image" {
  type    = string
  default = "ubuntu-24-04-x64"
}

variable "db_volume_gb" {
  type    = number
  default = 50
}

variable "ssh_key_ids" {
  description = "DigitalOcean SSH key IDs, one per operator."
  type        = list(string)
}

variable "domain" {
  type = string
}
