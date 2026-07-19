variable "project_id" {
  type        = string
  description = "The GCP project ID"
}

variable "region" {
  type        = string
  description = "The GCP region to deploy to"
  default     = "us-central1"
}

variable "zone" {
  type        = string
  description = "The GCP zone to deploy the VM to"
  default     = "us-central1-a"
}

variable "org_id" {
  type        = string
  description = "The Kiwi organization ID this daemon belongs to"
}

variable "join_token" {
  type        = string
  description = "The join token for the daemon to authenticate with the API"
  sensitive   = true
}

variable "api_url" {
  type        = string
  description = "The URL of the Kiwi Control Plane API"
}

variable "network_name" {
  type        = string
  description = "The VPC network name"
  default     = "kiwi-vpc"
}

variable "subnet_name" {
  type        = string
  description = "The daemon subnet name"
  default     = "kiwi-daemon-subnet"
}

variable "daemon_image" {
  type        = string
  description = "The Docker image for the kiwidaemon"
}

variable "machine_type" {
  type        = string
  description = "The machine type for the daemon VM"
  default     = "e2-small"
}

variable "cache_disk_size_gb" {
  type        = number
  description = "Size of the persistent cache disk in GB"
  default     = 50
}
