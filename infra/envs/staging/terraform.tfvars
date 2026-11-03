# Staging cell c90: same shape as the pilot, smallest sizes. Destroy when idle.
cell               = "c90" # staging cell code, so names never collide with production
region             = "nyc1"
vpc_ip_range       = "10.20.0.0/16"
domain             = "vpn-staging.malgary.com" # delegated from malgary.com (research R14)
backup_region      = "ams3"
ssh_key_ids        = []
control_host_count = 1
control_host_size  = "s-4vcpu-8gb"
db_volume_gb       = 20
relay_enabled      = false

gateway_pairs = {
  "gw-nyc-1" = { region = "nyc1", size = "s-1vcpu-2gb" }
}
