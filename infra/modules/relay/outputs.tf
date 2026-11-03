output "relay_fqdns" {
  value = [for r in digitalocean_record.relay : "${r.name}.${var.domain}"]
}
