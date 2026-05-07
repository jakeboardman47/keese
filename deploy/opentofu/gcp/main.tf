# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

# ---------------------------------------------------------------------------
# Backend hint — configure in a separate backend.tf or via CLI flags.
#
# Example backend.tf:
#   terraform {
#     backend "gcs" {
#       bucket = "<your-state-bucket>"
#       prefix = "keese/<env>"
#     }
#   }
#
# The GCS bucket should have:
#   - Object versioning enabled (built-in locking mechanism for GCS backend)
#   - CMEK encryption via var.database_encryption_key's key ring
# ---------------------------------------------------------------------------

provider "google" {
  project = var.project_id
  region  = var.region
}

provider "google-beta" {
  project = var.project_id
  region  = var.region
}

# ---------------------------------------------------------------------------
# VPC
# ---------------------------------------------------------------------------

resource "google_compute_network" "main" {
  name                    = "${var.cluster_name}-vpc"
  auto_create_subnetworks = false
  project                 = var.project_id
}

resource "google_compute_subnetwork" "main" {
  name          = "${var.cluster_name}-subnet"
  ip_cidr_range = var.network_cidr
  region        = var.region
  network       = google_compute_network.main.id
  project       = var.project_id

  secondary_ip_range {
    range_name    = "pods"
    ip_cidr_range = var.pods_cidr
  }

  secondary_ip_range {
    range_name    = "services"
    ip_cidr_range = var.services_cidr
  }

  private_ip_google_access = true
}

# ---------------------------------------------------------------------------
# GKE Autopilot cluster
# ---------------------------------------------------------------------------

resource "google_container_cluster" "main" {
  provider = google-beta
  name     = var.cluster_name
  location = var.region
  project  = var.project_id

  # Autopilot manages node lifecycle; no node_pool blocks needed.
  enable_autopilot = true

  min_master_version = var.kubernetes_version

  network    = google_compute_network.main.id
  subnetwork = google_compute_subnetwork.main.id

  ip_allocation_policy {
    cluster_secondary_range_name  = "pods"
    services_secondary_range_name = "services"
  }

  # Private cluster — no public endpoint by default.
  private_cluster_config {
    enable_private_nodes    = true
    enable_private_endpoint = false
    master_ipv4_cidr_block  = "172.16.0.0/28"
  }

  # Etcd database encryption (Conftest require-encryption-at-rest.rego enforces this).
  database_encryption {
    state    = "ENCRYPTED"
    key_name = var.database_encryption_key
  }

  # Workload Identity enables per-SA GCP IAM bindings (design 04b-ii).
  workload_identity_config {
    workload_pool = "${var.project_id}.svc.id.goog"
  }

  release_channel {
    channel = "REGULAR"
  }

  resource_labels = var.labels
}

# ---------------------------------------------------------------------------
# GCP Service Account — keese controller
# ---------------------------------------------------------------------------

resource "google_service_account" "keese_controller" {
  account_id   = "${var.cluster_name}-keese-ctrl"
  display_name = "keese controller manager (${var.cluster_name})"
  project      = var.project_id
}

# Allow the Kubernetes ServiceAccount to impersonate the GCP SA.
resource "google_service_account_iam_member" "keese_wi_binding" {
  service_account_id = google_service_account.keese_controller.name
  role               = "roles/iam.workloadIdentityUser"
  member             = "serviceAccount:${var.project_id}.svc.id.goog[keese-system/keese-controller-manager]"
}

# Grant access to Secret Manager for BackendSecurityPolicy credential resolution.
resource "google_project_iam_member" "keese_secrets" {
  project = var.project_id
  role    = "roles/secretmanager.secretAccessor"
  member  = "serviceAccount:${google_service_account.keese_controller.email}"

  condition {
    title       = "keese-secrets-only"
    description = "Scoped to secrets prefixed keese/"
    expression  = "resource.name.startsWith(\"projects/${var.project_id}/secrets/keese/\")"
  }
}

# ---------------------------------------------------------------------------
# Workload Identity Pool + Provider (per design 04b-ii)
# Referenced externally; the cluster implicitly creates the pool
# "${project_id}.svc.id.goog". Additional external pools (for OIDC federation
# with GitHub Actions CI) are out of scope for this module.
# ---------------------------------------------------------------------------
