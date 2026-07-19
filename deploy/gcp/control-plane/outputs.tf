output "vpc_id" {
  value       = google_compute_network.vpc.id
  description = "The ID of the VPC"
}

output "daemon_subnet_id" {
  value       = google_compute_subnetwork.daemon_subnet.id
  description = "The ID of the daemon subnet"
}

output "load_balancer_ip" {
  value       = google_compute_global_address.default.address
  description = "The IP address of the Global Load Balancer"
}

output "api_service_url" {
  value       = google_cloud_run_v2_service.api.uri
  description = "The direct URL of the API Cloud Run service"
}

output "frontend_service_url" {
  value       = google_cloud_run_v2_service.frontend.uri
  description = "The direct URL of the Frontend Cloud Run service"
}

output "kms_key_id" {
  value       = google_kms_crypto_key.key.id
  description = "The ID of the KMS crypto key"
}
