# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

terraform {
  required_version = ">= 1.7.0"

  required_providers {
    azurerm = {
      source  = "hashicorp/azurerm"
      version = "= 3.111.0"
    }
    azuread = {
      source  = "hashicorp/azuread"
      version = "= 2.53.1"
    }
  }
}
