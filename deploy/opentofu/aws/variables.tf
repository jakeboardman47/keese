# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

variable "cluster_name" {
  description = "Name of the EKS cluster."
  type        = string
}

variable "region" {
  description = "AWS region to deploy into (e.g. us-east-1)."
  type        = string
}

variable "kubernetes_version" {
  description = "EKS Kubernetes version (e.g. 1.30)."
  type        = string
  default     = "1.30"
}

variable "vpc_cidr" {
  description = "IPv4 CIDR block for the new VPC."
  type        = string
  default     = "10.0.0.0/16"
}

variable "availability_zones" {
  description = "List of AZs to span (minimum 2 required)."
  type        = list(string)
}

variable "node_instance_type" {
  description = "EC2 instance type for the default managed node group."
  type        = string
  default     = "m6i.xlarge"
}

variable "node_min_size" {
  description = "Minimum number of worker nodes per AZ."
  type        = number
  default     = 1
}

variable "node_max_size" {
  description = "Maximum number of worker nodes per AZ."
  type        = number
  default     = 5
}

variable "node_desired_size" {
  description = "Desired number of worker nodes per AZ at steady state."
  type        = number
  default     = 2
}

variable "eks_public_access_cidrs" {
  description = "CIDRs allowed to reach the EKS public API endpoint. Empty disables public access entirely."
  type        = list(string)
  default     = []
}

variable "secrets_encryption_key_arn" {
  description = "KMS key ARN used for etcd secrets encryption. Must be set; no default (deny-no-encryption Conftest rule)."
  type        = string
}

variable "state_bucket" {
  description = "S3 bucket name for OpenTofu state (placeholder — set via backend config, not this var)."
  type        = string
  default     = "PLACEHOLDER_STATE_BUCKET"
}

variable "lock_table" {
  description = "DynamoDB table name for state locking (placeholder — set via backend config, not this var)."
  type        = string
  default     = "PLACEHOLDER_LOCK_TABLE"
}

variable "tags" {
  description = "Tags applied to every resource."
  type        = map(string)
  default     = {}
}
