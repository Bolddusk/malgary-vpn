variable "enabled" {
  type    = bool
  default = false
}

variable "instances" {
  description = "At least 2 once enabled (constitution Principle IV)."
  type        = number
  default     = 2
  validation {
    condition     = var.instances >= 2
    error_message = "Relays run as at least two instances."
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
  type    = string
  default = "s-2vcpu-4gb"
}

variable "ssh_key_ids" {
  type = list(string)
}

variable "domain" {
  type = string
}
