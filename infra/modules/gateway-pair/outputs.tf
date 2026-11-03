output "egress_ip" {
  description = "Reserved IP that every tenant on this pair leaves from. Register with the PTA (FR-011)."
  value       = digitalocean_reserved_ip.egress.ip_address
}

output "member_ids" {
  value = { for k, d in digitalocean_droplet.member : k => d.id }
}

output "member_private_ips" {
  value = { for k, d in digitalocean_droplet.member : k => d.ipv4_address_private }
}
