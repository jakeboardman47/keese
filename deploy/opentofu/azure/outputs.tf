# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

output "kubeconfig_command" {
  description = "az CLI command that writes a kubeconfig entry for this cluster."
  value       = "az aks get-credentials --resource-group ${azurerm_resource_group.main.name} --name ${azurerm_kubernetes_cluster.main.name}"
}

output "oidc_issuer_url" {
  description = "OIDC issuer URL for the AKS cluster (used by Workspace controller for federated credentials)."
  value       = azurerm_kubernetes_cluster.main.oidc_issuer_url
}

output "controller_sa_client_id" {
  description = "Client ID of the user-assigned managed identity to annotate the keese controller ServiceAccount."
  value       = azurerm_user_assigned_identity.keese_controller.client_id
}

output "controller_sa_annotation" {
  description = "Full annotation value for the keese-controller-manager Kubernetes ServiceAccount."
  value       = "azure.workload.identity/client-id: ${azurerm_user_assigned_identity.keese_controller.client_id}"
}

output "cluster_endpoint" {
  description = "AKS API server endpoint."
  value       = azurerm_kubernetes_cluster.main.kube_config[0].host
  sensitive   = true
}

output "cluster_ca_data" {
  description = "Base64-encoded cluster CA certificate."
  value       = azurerm_kubernetes_cluster.main.kube_config[0].cluster_ca_certificate
  sensitive   = true
}
