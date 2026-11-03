# Control host: runs tenant NetBird stacks, PostgreSQL and (in the pilot) the platform services.
terraform {
  required_providers {
    digitalocean = { source = "digitalocean/digitalocean" }
  }
}

resource "digitalocean_droplet" "this" {
  name       = "ctl-${var.index}.${var.cell}"
  region     = var.region
  size       = var.size
  image      = var.image
  vpc_uuid   = var.vpc_id
  backups    = true
  monitoring = true
  ssh_keys   = var.ssh_key_ids
  tags       = ["control", "cell_${var.cell}"]

  lifecycle {
    # Resizing is done in place by changing var.size; never replace the droplet for image drift.
    ignore_changes = [image]
  }
}

resource "digitalocean_volume" "pgdata" {
  name                    = "pgdata-ctl-${var.index}-${var.cell}"
  region                  = var.region
  size                    = var.db_volume_gb
  initial_filesystem_type = "ext4"
  description             = "PostgreSQL data for ctl-${var.index}.${var.cell}"
}

resource "digitalocean_volume_attachment" "pgdata" {
  droplet_id = digitalocean_droplet.this.id
  volume_id  = digitalocean_volume.pgdata.id
}

resource "digitalocean_record" "host" {
  domain = var.domain
  type   = "A"
  name   = "ctl-${var.index}.${var.cell}"
  value  = digitalocean_droplet.this.ipv4_address_private
  ttl    = 300
}
