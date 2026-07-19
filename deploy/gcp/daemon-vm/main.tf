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
  zone    = var.zone
}

# Persistent cache disk
resource "google_compute_disk" "cache_disk" {
  name = "kiwidaemon-cache-${var.org_id}"
  type = "pd-standard"
  zone = var.zone
  size = var.cache_disk_size_gb
  labels = {
    org_id = var.org_id
  }
}

# The Daemon VM
resource "google_compute_instance" "daemon_vm" {
  name         = "kiwidaemon-${var.org_id}"
  machine_type = var.machine_type
  zone         = var.zone

  # Nested virtualization is required for Firecracker
  advanced_machine_features {
    enable_nested_virtualization = true
  }

  boot_disk {
    initialize_params {
      image = "ubuntu-os-cloud/ubuntu-2204-lts"
      size  = 20
    }
  }

  shielded_instance_config {
    enable_secure_boot          = true
    enable_vtpm                 = true
    enable_integrity_monitoring = true
  }

  service_account {
    # No email specified means it uses the default compute service account.
    # We provide an empty scopes list to disable the default token scope.
    scopes = []
  }

  attached_disk {
    source      = google_compute_disk.cache_disk.id
    device_name = "cache-disk"
  }

  network_interface {
    network    = var.network_name
    subnetwork = var.subnet_name
    # No external IP (hostile-by-default environment)
  }

  metadata = {
    "kiwi-org-id"            = var.org_id
    "kiwi-join-token"        = var.join_token
    "kiwi-api-url"           = var.api_url
    "kiwi-image"             = var.daemon_image
    "block-project-ssh-keys" = "true"
    "enable-oslogin"         = "false"
    "serial-port-enable"     = "false"
  }

  # Startup script to mount the disk, pull the image, and run the daemon
  metadata_startup_script = <<-EOF
    #!/bin/bash
    set -euo pipefail

    # 1. Format and mount the cache disk
    MNT_DIR="/mnt/kiwi-cache"
    DEVICE="/dev/disk/by-id/google-cache-disk"

    if ! blkid $DEVICE; then
      mkfs.ext4 -m 0 -E lazy_itable_init=0,lazy_journal_init=0,discard $DEVICE
    fi
    mkdir -p $MNT_DIR
    mount -o discard,defaults $DEVICE $MNT_DIR
    echo "$DEVICE $MNT_DIR ext4 discard,defaults 0 2" >> /etc/fstab

    # 2. Read metadata
    ORG_ID=$(curl -s "http://metadata.google.internal/computeMetadata/v1/instance/attributes/kiwi-org-id" -H "Metadata-Flavor: Google")
    JOIN_TOKEN=$(curl -s "http://metadata.google.internal/computeMetadata/v1/instance/attributes/kiwi-join-token" -H "Metadata-Flavor: Google")
    API_URL=$(curl -s "http://metadata.google.internal/computeMetadata/v1/instance/attributes/kiwi-api-url" -H "Metadata-Flavor: Google")
    IMAGE=$(curl -s "http://metadata.google.internal/computeMetadata/v1/instance/attributes/kiwi-image" -H "Metadata-Flavor: Google")

    # 3. Install Docker (minimal)
    apt-get update
    apt-get install -y docker.io

    # 4. Run the daemon
    docker run -d \
      --name kiwidaemon \
      --restart always \
      -v $MNT_DIR:/var/lib/kiwi/cache \
      -e KIWI_ORG_ID="$ORG_ID" \
      -e KIWI_JOIN_TOKEN="$JOIN_TOKEN" \
      -e KIWI_API_URL="$API_URL" \
      -e KIWI_CACHE_DIR="/var/lib/kiwi/cache" \
      "$IMAGE" -role daemon
  EOF

  tags = ["kiwi-daemon"]

  labels = {
    org_id = var.org_id
  }
}
