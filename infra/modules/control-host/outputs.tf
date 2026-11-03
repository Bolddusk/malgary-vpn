output "id" {
  value = digitalocean_droplet.this.id
}

output "public_ipv4" {
  value = digitalocean_droplet.this.ipv4_address
}

output "private_ipv4" {
  value = digitalocean_droplet.this.ipv4_address_private
}

output "fqdn" {
  value = "${digitalocean_record.host.name}.${var.domain}"
}
