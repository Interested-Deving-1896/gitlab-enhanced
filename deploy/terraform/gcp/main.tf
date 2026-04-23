# gitlab-enhanced — GCP deployment
#
# Provisions a GitLab Omnibus instance on a Compute Engine VM with:
#   - VPC network and firewall rules
#   - Compute Engine instance (Ubuntu 24.04) with GitLab via startup script
#   - GCS bucket for object storage (LFS, artefacts, registry)
#   - Static external IP
#   - Cloud DNS A record (optional)
#
# Usage:
#   cd deploy/terraform/gcp
#   terraform init
#   terraform apply \
#     -var="project=my-gcp-project" \
#     -var="gitlab_domain=gitlab.example.com"

terraform {
  required_version = ">= 1.6"
  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }
}

provider "google" {
  project = var.project
  region  = var.region
  zone    = var.zone
}

# ── Variables ─────────────────────────────────────────────────────────────────

variable "project" {
  description = "GCP project ID"
  type        = string
}

variable "region" {
  description = "GCP region"
  type        = string
  default     = "us-central1"
}

variable "zone" {
  description = "GCP zone"
  type        = string
  default     = "us-central1-a"
}

variable "gitlab_domain" {
  description = "External hostname for GitLab"
  type        = string
}

variable "gitlab_edition" {
  description = "GitLab edition: ce or ee"
  type        = string
  default     = "ce"
}

variable "machine_type" {
  description = "Compute Engine machine type"
  type        = string
  default     = "n2-standard-4"
}

variable "disk_size_gb" {
  description = "Boot disk size in GB"
  type        = number
  default     = 100
}

variable "dns_zone_name" {
  description = "Cloud DNS managed zone name (leave empty to skip DNS)"
  type        = string
  default     = ""
}

variable "allowed_source_ranges" {
  description = "Source IP ranges allowed to reach GitLab"
  type        = list(string)
  default     = ["0.0.0.0/0"]
}

variable "labels" {
  description = "Labels applied to all resources"
  type        = map(string)
  default     = {project = "gitlab-enhanced"}
}

# ── Networking ────────────────────────────────────────────────────────────────

resource "google_compute_network" "main" {
  name                    = "gitlab-enhanced"
  auto_create_subnetworks = false
}

resource "google_compute_subnetwork" "main" {
  name          = "gitlab-enhanced"
  ip_cidr_range = "10.0.1.0/24"
  region        = var.region
  network       = google_compute_network.main.id
}

resource "google_compute_firewall" "gitlab_https" {
  name    = "gitlab-enhanced-https"
  network = google_compute_network.main.name

  allow {
    protocol = "tcp"
    ports    = ["80", "443", "22"]
  }

  source_ranges = var.allowed_source_ranges
  target_tags   = ["gitlab-enhanced"]
}

resource "google_compute_address" "gitlab" {
  name   = "gitlab-enhanced"
  region = var.region
}

# ── Service account ───────────────────────────────────────────────────────────

resource "google_service_account" "gitlab" {
  account_id   = "gitlab-enhanced"
  display_name = "GitLab Enhanced"
}

resource "google_project_iam_member" "gitlab_gcs" {
  project = var.project
  role    = "roles/storage.objectAdmin"
  member  = "serviceAccount:${google_service_account.gitlab.email}"
}

# ── GCS bucket ────────────────────────────────────────────────────────────────

resource "google_storage_bucket" "gitlab" {
  name          = "gitlab-enhanced-${var.project}"
  location      = var.region
  force_destroy = false
  labels        = var.labels

  versioning {
    enabled = true
  }

  uniform_bucket_level_access = true
}

# ── Compute instance ──────────────────────────────────────────────────────────

resource "google_compute_instance" "gitlab" {
  name         = "gitlab-enhanced"
  machine_type = var.machine_type
  zone         = var.zone
  tags         = ["gitlab-enhanced"]
  labels       = var.labels

  boot_disk {
    initialize_params {
      image = "ubuntu-os-cloud/ubuntu-2404-lts-amd64"
      size  = var.disk_size_gb
      type  = "pd-ssd"
    }
  }

  network_interface {
    subnetwork = google_compute_subnetwork.main.id
    access_config {
      nat_ip = google_compute_address.gitlab.address
    }
  }

  service_account {
    email  = google_service_account.gitlab.email
    scopes = ["cloud-platform"]
  }

  metadata_startup_script = templatefile("${path.module}/startup.sh.tpl", {
    gitlab_domain  = var.gitlab_domain
    gitlab_edition = var.gitlab_edition
    gcs_bucket     = google_storage_bucket.gitlab.name
    gcp_project    = var.project
  })

  lifecycle {
    ignore_changes = [metadata_startup_script]
  }
}

# ── DNS (optional) ────────────────────────────────────────────────────────────

resource "google_dns_record_set" "gitlab" {
  count        = var.dns_zone_name != "" ? 1 : 0
  name         = "${var.gitlab_domain}."
  type         = "A"
  ttl          = 300
  managed_zone = var.dns_zone_name
  rrdatas      = [google_compute_address.gitlab.address]
}

# ── Outputs ───────────────────────────────────────────────────────────────────

output "gitlab_ip" {
  description = "External IP of the GitLab instance"
  value       = google_compute_address.gitlab.address
}

output "gitlab_url" {
  description = "GitLab URL"
  value       = "https://${var.gitlab_domain}"
}

output "gcs_bucket" {
  description = "GCS bucket name for GitLab object storage"
  value       = google_storage_bucket.gitlab.name
}

output "ssh_command" {
  description = "SSH command (via gcloud)"
  value       = "gcloud compute ssh gitlab-enhanced --zone=${var.zone} --project=${var.project}"
}
