terraform {
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

provider "google" {
  project = var.project_id
  region  = var.region
}

# VPC and Subnets
resource "google_compute_network" "vpc" {
  name                    = var.vpc_name
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "daemon_subnet" {
  name          = "kiwi-daemon-subnet"
  ip_cidr_range = var.subnet_daemon_cidr
  region        = var.region
  network       = google_compute_network.vpc.id
}

# Serverless VPC Access Connector for Cloud Run to reach Cloud SQL
resource "google_vpc_access_connector" "connector" {
  name          = "kiwi-vpc-conn"
  region        = var.region
  network       = google_compute_network.vpc.name
  ip_cidr_range = var.subnet_connector_cidr
}

# Private Service Access for Cloud SQL
resource "google_compute_global_address" "private_ip_address" {
  name          = "private-ip-address"
  purpose       = "VPC_PEERING"
  address_type  = "INTERNAL"
  prefix_length = 16
  network       = google_compute_network.vpc.id
}

resource "google_service_networking_connection" "private_vpc_connection" {
  network                 = google_compute_network.vpc.id
  service                 = "servicenetworking.googleapis.com"
  reserved_peering_ranges = [google_compute_global_address.private_ip_address.name]
}

# Cloud SQL (Postgres)
resource "google_sql_database_instance" "instance" {
  name             = var.db_instance_name
  region           = var.region
  database_version = "POSTGRES_15"

  depends_on = [google_service_networking_connection.private_vpc_connection]

  settings {
    tier = var.db_tier

    ip_configuration {
      ipv4_enabled    = false
      private_network = google_compute_network.vpc.id
    }

    backup_configuration {
      enabled                        = true
      point_in_time_recovery_enabled = true
    }

    availability_type = "REGIONAL"
  }
  deletion_protection = false
}

resource "google_sql_database" "database" {
  name     = var.db_name
  instance = google_sql_database_instance.instance.name
}

resource "google_sql_user" "users" {
  name     = var.db_user
  instance = google_sql_database_instance.instance.name
  password = var.db_password
}

# Artifact Registry
resource "google_artifact_registry_repository" "repo" {
  location      = var.region
  repository_id = var.artifact_registry_repo
  description   = "Kiwi Docker repository"
  format        = "DOCKER"
}

# Secret Manager
resource "google_secret_manager_secret" "kiwi_server_token" {
  secret_id = "kiwi-server-token"
  replication {
    auto {}
  }
}

resource "google_secret_manager_secret_version" "kiwi_server_token_version" {
  secret      = google_secret_manager_secret.kiwi_server_token.id
  secret_data = var.kiwi_server_token
}

# Cloud KMS
resource "google_kms_key_ring" "keyring" {
  name     = var.kms_keyring_name
  location = var.region
}

resource "google_kms_crypto_key" "key" {
  name            = var.kms_key_name
  key_ring        = google_kms_key_ring.keyring.id
  rotation_period = "7776000s" # 90 days
}

# Cloud Run Service Account
resource "google_service_account" "cloudrun_sa" {
  account_id   = "kiwi-cloudrun-sa"
  display_name = "Kiwi Cloud Run Service Account"
}

# Grant Cloud SQL client
resource "google_project_iam_member" "cloudsql_client" {
  project = var.project_id
  role    = "roles/cloudsql.client"
  member  = "serviceAccount:${google_service_account.cloudrun_sa.email}"
}

# Cloud Run Services
resource "google_cloud_run_v2_service" "api" {
  name     = "kiwi-api"
  location = var.region

  template {
    service_account = google_service_account.cloudrun_sa.email
    scaling {
      min_instance_count = 1
    }
    containers {
      image = var.kiwid_image
      args  = ["-role", "api"]

      env {
        name  = "KIWI_DSN"
        value = "host=${google_sql_database_instance.instance.private_ip_address} user=${var.db_user} password=${var.db_password} dbname=${var.db_name} sslmode=disable"
      }
      env {
        name = "KIWI_SERVER_TOKEN"
        value_source {
          secret_key_ref {
            secret  = google_secret_manager_secret.kiwi_server_token.secret_id
            version = "latest"
          }
        }
      }
      env {
        name  = "KIWI_CORS_ALLOWED_ORIGINS"
        value = var.allowed_cors_origins
      }
      env {
        name  = "KIWI_SKIP_BOOT_MIGRATE"
        value = "true"
      }
    }

    vpc_access {
      connector = google_vpc_access_connector.connector.id
      egress    = "ALL_TRAFFIC"
    }
  }
}

resource "google_cloud_run_v2_service" "orchestrator" {
  name     = "kiwi-orchestrator"
  location = var.region

  template {
    service_account = google_service_account.cloudrun_sa.email
    scaling {
      min_instance_count = 1
      max_instance_count = 1
    }
    containers {
      image = var.kiwid_image
      args  = ["-role", "orchestrator"]

      resources {
        cpu_idle = false
      }

      env {
        name  = "KIWI_DSN"
        value = "host=${google_sql_database_instance.instance.private_ip_address} user=${var.db_user} password=${var.db_password} dbname=${var.db_name} sslmode=disable"
      }
      env {
        name  = "KIWI_SKIP_BOOT_MIGRATE"
        value = "true"
      }
    }
    vpc_access {
      connector = google_vpc_access_connector.connector.id
      egress    = "ALL_TRAFFIC"
    }
  }
}

resource "google_cloud_run_v2_service" "frontend" {
  name     = "kiwi-frontend"
  location = var.region

  template {
    containers {
      image = var.frontend_image

      env {
        name  = "NEXT_PUBLIC_KIWI_API_URL"
        value = "https://${var.api_domain}"
      }
    }
  }
}

# Cloud Run Job for Migrations
resource "google_cloud_run_v2_job" "migrate" {
  name     = "kiwi-migrate"
  location = var.region

  template {
    template {
      service_account = google_service_account.cloudrun_sa.email
      containers {
        image = var.kiwid_image
        args  = ["-role", "migrate"]

        env {
          name  = "KIWI_DSN"
          value = "host=${google_sql_database_instance.instance.private_ip_address} user=${var.db_user} password=${var.db_password} dbname=${var.db_name} sslmode=disable"
        }
      }
      vpc_access {
        connector = google_vpc_access_connector.connector.id
        egress    = "ALL_TRAFFIC"
      }
    }
  }
}

# Network Endpoint Groups for Cloud Run
resource "google_compute_region_network_endpoint_group" "api_neg" {
  name                  = "api-neg"
  network_endpoint_type = "SERVERLESS"
  region                = var.region
  cloud_run {
    service = google_cloud_run_v2_service.api.name
  }
}

resource "google_compute_region_network_endpoint_group" "frontend_neg" {
  name                  = "frontend-neg"
  network_endpoint_type = "SERVERLESS"
  region                = var.region
  cloud_run {
    service = google_cloud_run_v2_service.frontend.name
  }
}

# Load Balancer Backend Services
resource "google_compute_backend_service" "api_backend" {
  name      = "api-backend"
  protocol  = "HTTPS"
  port_name = "http"

  backend {
    group = google_compute_region_network_endpoint_group.api_neg.id
  }
}

resource "google_compute_backend_service" "frontend_backend" {
  name      = "frontend-backend"
  protocol  = "HTTPS"
  port_name = "http"

  backend {
    group = google_compute_region_network_endpoint_group.frontend_neg.id
  }
}

# URL Map for Load Balancing
resource "google_compute_url_map" "default" {
  name            = "kiwi-lb"
  default_service = google_compute_backend_service.frontend_backend.id

  host_rule {
    hosts        = [var.api_domain]
    path_matcher = "api-paths"
  }

  host_rule {
    hosts        = [var.frontend_domain]
    path_matcher = "frontend-paths"
  }

  path_matcher {
    name            = "api-paths"
    default_service = google_compute_backend_service.api_backend.id
  }

  path_matcher {
    name            = "frontend-paths"
    default_service = google_compute_backend_service.frontend_backend.id
  }
}

# Managed SSL Certificate
resource "google_compute_managed_ssl_certificate" "default" {
  name = "kiwi-cert"

  managed {
    domains = [var.api_domain, var.frontend_domain]
  }
}

# HTTPS Target Proxy
resource "google_compute_target_https_proxy" "default" {
  name             = "kiwi-https-proxy"
  url_map          = google_compute_url_map.default.id
  ssl_certificates = [google_compute_managed_ssl_certificate.default.id]
}

# Global Forwarding Rule
resource "google_compute_global_address" "default" {
  name = "kiwi-lb-ip"
}

resource "google_compute_global_forwarding_rule" "default" {
  name       = "kiwi-forwarding-rule"
  target     = google_compute_target_https_proxy.default.id
  port_range = "443"
  ip_address = google_compute_global_address.default.id
}
