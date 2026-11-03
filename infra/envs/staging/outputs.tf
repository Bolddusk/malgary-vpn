output "egress_ips" {
  description = "Reserved IPs per gateway pair. Register each with the PTA before use (FR-011)."
  value       = { for k, m in module.gateway_pair : k => m.egress_ip }
}

output "control_hosts" {
  value = [for c in module.control : { fqdn = c.fqdn, public = c.public_ipv4, private = c.private_ipv4 }]
}

output "backup_bucket" {
  value = module.cell.backup_bucket
}
