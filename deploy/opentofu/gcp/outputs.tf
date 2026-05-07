# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

output "kubeconfig_command" {
  description = "gcloud CLI command that writes a kubeconfig entry for this cluster."
  value       = "gcloud container clusters get-credentials ${google_container_cluster.main.name} --region ${var.region} --project ${var.project_id}"
}

output "oidc_issuer_url" {
  description = "OIDC issuer URL for the GKE cluster (used by Workload Identity Federation)."
  value       = "https://container.googleapis.com/v1/projects/${var.project_id}/locations/${var.region}/clusters/${google_container_cluster.main.name}"
}

output "workload_identity_pool" {
  description = "Workload Identity Pool ID (project-number.svc.id.goog)."
  value       = "${var.project_id}.svc.id.goog"
}

output "controller_sa_email" {
  description = "GCP Service Account email to bind to the keese controller KSA (Workload Identity annotation value)."
  value       = google_service_account.keese_controller.email
}

output "cluster_endpoint" {
  description = "GKE master endpoint."
  value       = google_container_cluster.main.endpoint
  sensitive   = true
}

output "cluster_ca_data" {
  description = "Base64-encoded cluster CA certificate."
  value       = google_container_cluster.main.master_auth[0].cluster_ca_certificate
  sensitive   = true
}
