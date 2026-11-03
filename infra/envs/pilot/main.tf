# Cell c01 composition. To grow, edit terraform.tfvars only (constitution Principle IX).
provider "digitalocean" {
  # Token from DIGITALOCEAN_TOKEN; Spaces keys from SPACES_ACCESS_KEY_ID / SPACES_SECRET_ACCESS_KEY.
}

# The delegated zone itself. Its NS records must be added at the malgary.com DNS host once.
resource "digitalocean_domain" "zone" {
  name = var.domain
}

module "cell" {
  source             = "../../modules/cell"
  code               = var.cell
  region             = var.region
  ip_range           = var.vpc_ip_range
  domain             = digitalocean_domain.zone.name
  control_ingress_ip = module.control[0].public_ipv4
  backup_region      = var.backup_region
}

module "control" {
  source       = "../../modules/control-host"
  count        = var.control_host_count
  cell         = var.cell
  index        = count.index + 1
  region       = var.region
  vpc_id       = module.cell.vpc_id
  size         = var.control_host_size
  db_volume_gb = var.db_volume_gb
  ssh_key_ids  = var.ssh_key_ids
  domain       = digitalocean_domain.zone.name
}

module "gateway_pair" {
  source      = "../../modules/gateway-pair"
  for_each    = var.gateway_pairs
  name        = each.key
  cell        = var.cell
  region      = each.value.region
  vpc_id      = module.cell.vpc_id
  size        = each.value.size
  ssh_key_ids = var.ssh_key_ids
  domain      = digitalocean_domain.zone.name
}

module "relay" {
  source      = "../../modules/relay"
  enabled     = var.relay_enabled
  cell        = var.cell
  region      = var.region
  vpc_id      = module.cell.vpc_id
  ssh_key_ids = var.ssh_key_ids
  domain      = digitalocean_domain.zone.name
}
