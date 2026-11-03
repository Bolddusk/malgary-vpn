# Pilot sizing for cell c01 (research R11, about $108/month). This is the only file to edit to grow.
cell               = "c01"
region             = "nyc1"
vpc_ip_range       = "10.10.0.0/16"
domain             = "vpn.malgary.com" # delegated from malgary.com (research R14)
backup_region      = "ams3"
ssh_key_ids        = [] # DigitalOcean SSH key IDs, one per operator
control_host_count = 1
control_host_size  = "s-4vcpu-8gb"
db_volume_gb       = 50
relay_enabled      = false

gateway_pairs = {
  "gw-nyc-1" = { region = "nyc1", size = "s-2vcpu-2gb" }
}
