variable "project_id" {
  type        = string
  description = "The GCP project ID"
}

variable "region" {
  type        = string
  description = "The GCP region to deploy to"
  default     = "us-central1"
}

variable "vpc_name" {
  type        = string
  description = "Name for the VPC network"
  default     = "kiwi-vpc"
}

variable "subnet_daemon_cidr" {
  type        = string
  description = "CIDR block for the daemon subnet"
  default     = "10.0.1.0/24"
}

variable "subnet_connector_cidr" {
  type        = string
  description = "CIDR block for the Serverless VPC Access connector"
  default     = "10.0.2.0/28"
}

variable "db_instance_name" {
  type        = string
  description = "Name for the Cloud SQL instance"
  default     = "kiwi-db-instance"
}

variable "db_name" {
  type        = string
  description = "Name of the Postgres database"
  default     = "kiwi"
}

variable "db_user" {
  type        = string
  description = "Database username"
  default     = "postgres"
}

variable "db_password" {
  type        = string
  description = "Database password"
  sensitive   = true
}

variable "db_tier" {
  type        = string
  description = "Cloud SQL tier"
  default     = "db-custom-2-8192"
}

variable "kms_keyring_name" {
  type        = string
  description = "Name for the KMS keyring"
  default     = "kiwi-keyring"
}

variable "kms_key_name" {
  type        = string
  description = "Name for the KMS crypto key"
  default     = "kiwi-encryption-key"
}

variable "artifact_registry_repo" {
  type        = string
  description = "Name for the Artifact Registry repository"
  default     = "kiwi-repo"
}

variable "kiwi_server_token" {
  type        = string
  description = "Admin bootstrap token"
  sensitive   = true
}

variable "allowed_cors_origins" {
  type        = string
  description = "Allowed CORS origins for the API"
  default     = "*"
}

variable "api_domain" {
  type        = string
  description = "Domain name for the API (e.g. api.runkiwi.com)"
}

variable "frontend_domain" {
  type        = string
  description = "Domain name for the Frontend (e.g. app.runkiwi.com)"
}

variable "kiwid_image" {
  type        = string
  description = "Container image for kiwid"
}

variable "frontend_image" {
  type        = string
  description = "Container image for frontend"
}

variable "allowed_egress_cidrs" {
  type        = list(string)
  description = "List of CIDRs allowed for egress from daemon VMs (e.g. GitHub, Anthropic, Kiwi API)"
  default     = ["140.82.112.0/20", "192.30.252.0/22"] # Example GitHub CIDRs
}

variable "github_oauth_client_id" {
  type        = string
  description = "GitHub OAuth Client ID"
  default     = ""
}

variable "github_oauth_client_secret" {
  type        = string
  description = "GitHub OAuth Client Secret"
  sensitive   = true
  default     = ""
}
