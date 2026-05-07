# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

# ---------------------------------------------------------------------------
# Backend hint — configure in a separate backend.tf or via CLI flags.
#
# Example backend.tf:
#   terraform {
#     backend "azurerm" {
#       resource_group_name  = "<rg-for-state>"
#       storage_account_name = "<your-storage-account>"
#       container_name       = "<your-container>"
#       key                  = "keese/<env>/terraform.tfstate"
#     }
#   }
#
# Azure Blob storage backend uses built-in lease-based locking.
# ---------------------------------------------------------------------------

provider "azurerm" {
  features {
    resource_group {
      prevent_deletion_if_contains_resources = true
    }
    key_vault {
      purge_soft_delete_on_destroy = false
    }
  }
}

provider "azuread" {}

data "azurerm_client_config" "current" {}

# ---------------------------------------------------------------------------
# Resource group
# ---------------------------------------------------------------------------

resource "azurerm_resource_group" "main" {
  name     = "${var.cluster_name}-rg"
  location = var.location
  tags     = var.tags
}

# ---------------------------------------------------------------------------
# Virtual network + subnet
# ---------------------------------------------------------------------------

resource "azurerm_virtual_network" "main" {
  name                = "${var.cluster_name}-vnet"
  address_space       = [var.vnet_cidr]
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  tags                = var.tags
}

resource "azurerm_subnet" "aks" {
  name                 = "${var.cluster_name}-aks-subnet"
  resource_group_name  = azurerm_resource_group.main.name
  virtual_network_name = azurerm_virtual_network.main.name
  address_prefixes     = [var.subnet_cidr]
}

# ---------------------------------------------------------------------------
# User-assigned managed identity — keese controller
# ---------------------------------------------------------------------------

resource "azurerm_user_assigned_identity" "keese_controller" {
  name                = "${var.cluster_name}-keese-ctrl"
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  tags                = var.tags
}

# Grant the managed identity access to Key Vault secrets.
resource "azurerm_key_vault_access_policy" "keese_controller" {
  key_vault_id = var.key_vault_id
  tenant_id    = data.azurerm_client_config.current.tenant_id
  object_id    = azurerm_user_assigned_identity.keese_controller.principal_id

  secret_permissions = ["Get", "List"]
}

# ---------------------------------------------------------------------------
# AKS cluster
# ---------------------------------------------------------------------------

resource "azurerm_kubernetes_cluster" "main" {
  name                = var.cluster_name
  location            = azurerm_resource_group.main.location
  resource_group_name = azurerm_resource_group.main.name
  dns_prefix          = var.cluster_name
  kubernetes_version  = var.kubernetes_version

  # Entra Workload Identity (replaces AAD Pod Identity).
  workload_identity_enabled = true
  oidc_issuer_enabled       = true

  # CMK disk encryption for node OS disks + etcd (Conftest require-encryption-at-rest.rego).
  disk_encryption_set_id = var.disk_encryption_set_id

  default_node_pool {
    name                         = "system"
    vm_size                      = var.node_vm_size
    zones                        = var.availability_zones
    min_count                    = var.node_min_count
    max_count                    = var.node_max_count
    enable_auto_scaling          = true
    vnet_subnet_id               = azurerm_subnet.aks.id
    os_disk_size_gb              = 128
    type                         = "VirtualMachineScaleSets"
    only_critical_addons_enabled = true

    upgrade_settings {
      max_surge = "33%"
    }
  }

  identity {
    type = "SystemAssigned"
  }

  network_profile {
    network_plugin    = "azure"
    network_policy    = "calico"
    load_balancer_sku = "standard"
    outbound_type     = "loadBalancer"
  }

  # Disable local accounts; use Entra ID only.
  local_account_disabled = true

  azure_active_directory_role_based_access_control {
    managed            = true
    azure_rbac_enabled = true
  }

  tags = var.tags
}

# ---------------------------------------------------------------------------
# Federated credential — bind keese controller KSA to the managed identity
# (Entra Workload Identity, design 04b-ii)
# ---------------------------------------------------------------------------

resource "azurerm_federated_identity_credential" "keese_controller" {
  name                = "keese-controller-manager"
  resource_group_name = azurerm_resource_group.main.name
  parent_id           = azurerm_user_assigned_identity.keese_controller.id
  audience            = ["api://AzureADTokenExchange"]
  issuer              = azurerm_kubernetes_cluster.main.oidc_issuer_url
  subject             = "system:serviceaccount:keese-system:keese-controller-manager"
}
