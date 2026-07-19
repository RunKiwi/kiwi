output "instance_name" {
  value       = google_compute_instance.daemon_vm.name
  description = "The name of the daemon VM instance"
}

output "instance_zone" {
  value       = google_compute_instance.daemon_vm.zone
  description = "The zone of the daemon VM instance"
}
