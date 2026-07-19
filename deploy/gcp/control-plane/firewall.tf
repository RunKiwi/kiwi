# Deny all egress from daemon VMs by default
resource "google_compute_firewall" "daemon_egress_deny_all" {
  name      = "kiwi-daemon-egress-deny-all"
  network   = google_compute_network.vpc.id
  direction = "EGRESS"
  priority  = 65535 # Lowest priority

  deny {
    protocol = "all"
  }

  destination_ranges = ["0.0.0.0/0"]
  target_tags        = ["kiwi-daemon"]
}

# Allow DNS (UDP 53)
resource "google_compute_firewall" "daemon_egress_allow_dns" {
  name      = "kiwi-daemon-egress-allow-dns"
  network   = google_compute_network.vpc.id
  direction = "EGRESS"
  priority  = 1000

  allow {
    protocol = "udp"
    ports    = ["53"]
  }
  allow {
    protocol = "tcp"
    ports    = ["53"]
  }

  destination_ranges = ["0.0.0.0/0"]
  target_tags        = ["kiwi-daemon"]
}

# Allow HTTPS to known CIDRs (VCS, Model APIs, and public API LB)
resource "google_compute_firewall" "daemon_egress_allow_https" {
  name      = "kiwi-daemon-egress-allow-https"
  network   = google_compute_network.vpc.id
  direction = "EGRESS"
  priority  = 1000

  allow {
    protocol = "tcp"
    ports    = ["443"]
  }

  destination_ranges = var.allowed_egress_cidrs
  target_tags        = ["kiwi-daemon"]
}

# Cloud Router for NAT
resource "google_compute_router" "router" {
  name    = "kiwi-router"
  region  = var.region
  network = google_compute_network.vpc.id
}

# Cloud NAT for Daemon Subnet
resource "google_compute_router_nat" "nat" {
  name                               = "kiwi-nat"
  router                             = google_compute_router.router.name
  region                             = var.region
  nat_ip_allocate_option             = "AUTO_ONLY"
  source_subnetwork_ip_ranges_to_nat = "LIST_OF_SUBNETWORKS"

  subnetwork {
    name                    = google_compute_subnetwork.daemon_subnet.id
    source_ip_ranges_to_nat = ["ALL_IP_RANGES"]
  }
}
