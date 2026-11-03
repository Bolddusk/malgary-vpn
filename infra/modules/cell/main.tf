# Cell: shared network, firewalls, DNS and storage for one region (constitution Principle V).
terraform {
  required_providers {
    digitalocean = { source = "digitalocean/digitalocean" }
  }
}

resource "digitalocean_vpc" "this" {
  name     = "malgary-${var.code}"
  region   = var.region
  ip_range = var.ip_range
}

# Control hosts: HTTPS for management URLs, setup pages and sign-in. No SSH from the internet:
# operators reach SSH over the VPC through the malgary-ops tenant (research R13, FR-029).
resource "digitalocean_firewall" "control" {
  name = "malgary-${var.code}-control"
  tags = ["control", "cell_${var.code}"]

  inbound_rule {
    protocol         = "tcp"
    port_range       = "80"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }
  inbound_rule {
    protocol         = "tcp"
    port_range       = "443"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }
  # STUN for tenant stacks: one port per stack, 3478 + (httpPort - 20000).
  inbound_rule {
    protocol         = "udp"
    port_range       = "3478-4477"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }
  inbound_rule {
    protocol         = "tcp"
    port_range       = "1-65535"
    source_addresses = [var.ip_range]
  }
  inbound_rule {
    protocol         = "udp"
    port_range       = "1-65535"
    source_addresses = [var.ip_range]
  }

  outbound_rule {
    protocol              = "tcp"
    port_range            = "1-65535"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }
  outbound_rule {
    protocol              = "udp"
    port_range            = "1-65535"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }
  outbound_rule {
    protocol              = "icmp"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }
}

# Gateways: WireGuard from anywhere; everything else only from the VPC.
# VRRP between the pair runs over the VPC (unicast), allowed by host nftables.
resource "digitalocean_firewall" "gateway" {
  name = "malgary-${var.code}-gateway"
  tags = ["gateway", "cell_${var.code}"]

  inbound_rule {
    protocol         = "udp"
    port_range       = var.wg_port_range
    source_addresses = ["0.0.0.0/0", "::/0"]
  }
  inbound_rule {
    protocol         = "tcp"
    port_range       = "1-65535"
    source_addresses = [var.ip_range]
  }
  inbound_rule {
    protocol         = "udp"
    port_range       = "1-65535"
    source_addresses = [var.ip_range]
  }

  outbound_rule {
    protocol              = "tcp"
    port_range            = "1-65535"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }
  outbound_rule {
    protocol              = "udp"
    port_range            = "1-65535"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }
  outbound_rule {
    protocol              = "icmp"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }
}

resource "digitalocean_spaces_bucket" "backups" {
  name   = "malgary-${var.code}-backups"
  region = var.backup_region
  acl    = "private"

  versioning {
    enabled = true
  }

  lifecycle_rule {
    id      = "expire-30d"
    enabled = true
    expiration {
      days = var.backup_retention_days
    }
    noncurrent_version_expiration {
      days = 7
    }
  }
}

resource "digitalocean_spaces_bucket" "artifacts" {
  name   = "malgary-${var.code}-artifacts"
  region = var.backup_region
  acl    = "private"

  versioning {
    enabled = true
  }
}

# Wildcard for tenant management URLs and setup pages: <tenant>.<zone>, e.g. onedge.vpn.malgary.com.
# The zone (var.domain) is a subdomain delegated to DigitalOcean (research R14).
resource "digitalocean_record" "tenants_wildcard" {
  domain = var.domain
  type   = "A"
  name   = "*"
  value  = var.control_ingress_ip
  ttl    = 300
}

resource "digitalocean_record" "identity" {
  domain = var.domain
  type   = "A"
  name   = "id"
  value  = var.control_ingress_ip
  ttl    = 300
}

resource "digitalocean_record" "api" {
  domain = var.domain
  type   = "A"
  name   = "api"
  value  = var.control_ingress_ip
  ttl    = 300
}
