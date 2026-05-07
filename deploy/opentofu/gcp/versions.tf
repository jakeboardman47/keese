# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

terraform {
  required_version = ">= 1.7.0"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "= 5.35.0"
    }
    google-beta = {
      source  = "hashicorp/google-beta"
      version = "= 5.35.0"
    }
  }
}
