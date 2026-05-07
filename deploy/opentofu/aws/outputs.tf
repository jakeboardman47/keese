# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

output "kubeconfig_command" {
  description = "aws CLI command that writes a kubeconfig entry for this cluster."
  value       = "aws eks update-kubeconfig --name ${aws_eks_cluster.main.name} --region ${var.region}"
}

output "oidc_issuer_url" {
  description = "OIDC issuer URL of the EKS cluster (used by Workspace controller for IRSA trust)."
  value       = aws_eks_cluster.main.identity[0].oidc[0].issuer
}

output "oidc_provider_arn" {
  description = "ARN of the IAM OIDC provider (referenced by per-tenant IRSA role trust policies)."
  value       = aws_iam_openid_connect_provider.eks.arn
}

output "controller_sa_role_arn" {
  description = "IAM role ARN to annotate the keese controller ServiceAccount with (IRSA)."
  value       = aws_iam_role.keese_controller.arn
}

output "node_role_arn" {
  description = "IAM role ARN used by the managed node group."
  value       = aws_iam_role.eks_node.arn
}

output "cluster_endpoint" {
  description = "EKS API server endpoint."
  value       = aws_eks_cluster.main.endpoint
}

output "cluster_ca_data" {
  description = "Base64-encoded cluster CA certificate (for kubeconfig construction)."
  value       = aws_eks_cluster.main.certificate_authority[0].data
  sensitive   = true
}
