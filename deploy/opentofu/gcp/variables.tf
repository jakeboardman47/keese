# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

variable "cluster_name" {
  description = "Name of the GKE Autopilot cluster."
  type        = string
}

variable "project_id" {
  description = "GCP project ID."
  type        = string
}

variable "region" {
  description = "GCP region (e.g. us-central1). Autopilot clusters are regional."
  type        = string
}

variable "kubernetes_version" {
  description = "Minimum master version for GKE (e.g. 1.30). Use 'latest' for the latest stable."
  type        = string
  default     = "1.30"
}

variable "network_cidr" {
  description = "VPC subnet CIDR for cluster nodes."
  type        = string
  default     = "10.0.0.0/20"
}

variable "pods_cidr" {
  description = "Secondary CIDR range for pods."
  type        = string
  default     = "10.1.0.0/16"
}

variable "services_cidr" {
  description = "Secondary CIDR range for services."
  type        = string
  default     = "10.2.0.0/20"
}

variable "database_encryption_key" {
  description = "Cloud KMS key name for etcd database encryption (format: projects/P/locations/L/keyRings/R/cryptoKeys/K). Must be set."
  type        = string
}

variable "state_bucket" {
  description = "GCS bucket for OpenTofu state (placeholder — configure via backend config, not this var)."
  type        = string
  default     = "PLACEHOLDER_STATE_BUCKET"
}

variable "labels" {
  description = "Labels applied to all resources."
  type        = map(string)
  default     = {}
}
