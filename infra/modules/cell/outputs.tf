output "vpc_id" {
  value = digitalocean_vpc.this.id
}

output "vpc_ip_range" {
  value = digitalocean_vpc.this.ip_range
}

output "firewall_ids" {
  value = {
    control = digitalocean_firewall.control.id
    gateway = digitalocean_firewall.gateway.id
  }
}

output "backup_bucket" {
  value = digitalocean_spaces_bucket.backups.name
}

output "artifacts_bucket" {
  value = digitalocean_spaces_bucket.artifacts.name
}
