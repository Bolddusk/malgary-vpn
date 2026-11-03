# Standalone NetBird relay. Unused in the pilot (tenant stacks use their built-in relay).
# From Tier 2, set enabled = true in the env tfvars: a variable change, not new code (Principle IX).
terraform {
  required_providers {
    digitalocean = { source = "digitalocean/digitalocean" }
  }
}

resource "digitalocean_droplet" "relay" {
  count      = var.enabled ? var.instances : 0
  name       = "relay-${count.index + 1}.${var.cell}"
  region     = var.region
  size       = var.size
  image      = "ubuntu-24-04-x64"
  vpc_uuid   = var.vpc_id
  monitoring = true
  ssh_keys   = var.ssh_key_ids
  tags       = ["relay", "cell_${var.cell}"]
}

resource "digitalocean_record" "relay" {
  count  = var.enabled ? var.instances : 0
  domain = var.domain
  type   = "A"
  name   = "relay-${count.index + 1}.${var.cell}"
  value  = digitalocean_droplet.relay[count.index].ipv4_address
  ttl    = 300
}
