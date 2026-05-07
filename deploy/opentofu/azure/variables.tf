# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

variable "cluster_name" {
  description = "Name of the AKS cluster and enclosing resource group."
  type        = string
}

variable "location" {
  description = "Azure region (e.g. eastus2)."
  type        = string
}

variable "kubernetes_version" {
  description = "AKS Kubernetes version (e.g. 1.30.2)."
  type        = string
  default     = "1.30.2"
}

variable "node_vm_size" {
  description = "VM size for the system node pool."
  type        = string
  default     = "Standard_D4s_v5"
}

variable "node_min_count" {
  description = "Minimum nodes in the system node pool (autoscaler lower bound)."
  type        = number
  default     = 2
}

variable "node_max_count" {
  description = "Maximum nodes in the system node pool."
  type        = number
  default     = 10
}

variable "availability_zones" {
  description = "Availability zones to spread nodes across (minimum 2)."
  type        = list(string)
  default     = ["1", "2"]
}

variable "vnet_cidr" {
  description = "CIDR for the AKS virtual network."
  type        = string
  default     = "10.0.0.0/16"
}

variable "subnet_cidr" {
  description = "Subnet CIDR for AKS nodes."
  type        = string
  default     = "10.0.0.0/20"
}

variable "disk_encryption_set_id" {
  description = "Resource ID of an Azure Disk Encryption Set (CMK) for etcd + node disk encryption. Must be set."
  type        = string
}

variable "key_vault_id" {
  description = "Resource ID of the Azure Key Vault used for keese secrets (BackendSecurityPolicy)."
  type        = string
}

variable "state_storage_account" {
  description = "Azure Storage Account name for OpenTofu state (placeholder — configure via backend config)."
  type        = string
  default     = "PLACEHOLDER_STORAGE_ACCOUNT"
}

variable "state_container" {
  description = "Storage container for OpenTofu state (placeholder — configure via backend config)."
  type        = string
  default     = "PLACEHOLDER_CONTAINER"
}

variable "tags" {
  description = "Tags applied to every resource."
  type        = map(string)
  default     = {}
}
