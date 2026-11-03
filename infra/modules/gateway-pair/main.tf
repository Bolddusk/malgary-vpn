# Gateway pair: primary + standby sharing one reserved IP (constitution Principles III and IV).
# keepalived moves the reserved IP at runtime, so OpenTofu only sets the initial assignment.
terraform {
  required_providers {
    digitalocean = { source = "digitalocean/digitalocean" }
  }
}

locals {
  members = {
    a = "${var.name}a"
    b = "${var.name}b"
  }
}

resource "digitalocean_droplet" "member" {
  for_each   = local.members
  name       = "${each.value}.${var.cell}"
  region     = var.region
  size       = var.size
  image      = var.image
  vpc_uuid   = var.vpc_id
  monitoring = true
  ssh_keys   = var.ssh_key_ids
  tags       = ["gateway", "pair_${replace(var.name, "-", "_")}", "cell_${var.cell}"]

  lifecycle {
    ignore_changes = [image]
  }
}

resource "digitalocean_reserved_ip" "egress" {
  region = var.region
}

resource "digitalocean_reserved_ip_assignment" "egress" {
  ip_address = digitalocean_reserved_ip.egress.ip_address
  droplet_id = digitalocean_droplet.member["a"].id

  lifecycle {
    # keepalived reassigns the IP on failover; don't fight it.
    ignore_changes = [droplet_id]
  }
}

resource "digitalocean_record" "member" {
  for_each = local.members
  domain   = var.domain
  type     = "A"
  name     = "${each.value}.${var.cell}"
  value    = digitalocean_droplet.member[each.key].ipv4_address_private
  ttl      = 300
}
