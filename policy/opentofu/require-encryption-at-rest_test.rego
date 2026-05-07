# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

package keese.opentofu.security_test

import data.keese.opentofu.security
import future.keywords.if

# ---------------------------------------------------------------------------
# require-encryption-at-rest — EKS
# ---------------------------------------------------------------------------

test_eks_no_encryption_config_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "aws_eks_cluster.main",
			"type": "aws_eks_cluster",
			"change": {"after": {
				"vpc_config": [{"endpoint_public_access": false, "public_access_cidrs": []}],
				"encryption_config": [],
			}},
		}]
	}
}

test_eks_encryption_missing_secrets_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "aws_eks_cluster.main",
			"type": "aws_eks_cluster",
			"change": {"after": {
				"vpc_config": [{"endpoint_public_access": false, "public_access_cidrs": []}],
				"encryption_config": [{
					"resources": ["configmaps"],
					"provider": [{"key_arn": "arn:aws:kms:us-east-1:123456789012:key/mrk-abc"}],
				}],
			}},
		}]
	}
}

test_eks_secrets_encryption_allowed if {
	count(security.deny) == 0 with input as {
		"resource_changes": [{
			"address": "aws_eks_cluster.main",
			"type": "aws_eks_cluster",
			"change": {"after": {
				"vpc_config": [{"endpoint_public_access": false, "public_access_cidrs": []}],
				"encryption_config": [{
					"resources": ["secrets"],
					"provider": [{"key_arn": "arn:aws:kms:us-east-1:123456789012:key/mrk-abc"}],
				}],
			}},
		}]
	}
}

# ---------------------------------------------------------------------------
# require-encryption-at-rest — GKE
# ---------------------------------------------------------------------------

test_gke_no_db_encryption_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "google_container_cluster.main",
			"type": "google_container_cluster",
			"change": {"after": {
				"private_cluster_config": [{"enable_private_endpoint": true}],
				"database_encryption": [{"state": "DECRYPTED", "key_name": ""}],
			}},
		}]
	}
}

test_gke_encrypted_allowed if {
	count(security.deny) == 0 with input as {
		"resource_changes": [{
			"address": "google_container_cluster.main",
			"type": "google_container_cluster",
			"change": {"after": {
				"private_cluster_config": [{"enable_private_endpoint": true}],
				"database_encryption": [{"state": "ENCRYPTED", "key_name": "projects/p/locations/l/keyRings/r/cryptoKeys/k"}],
			}},
		}]
	}
}

# ---------------------------------------------------------------------------
# require-encryption-at-rest — AKS
# ---------------------------------------------------------------------------

test_aks_no_disk_encryption_set_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "azurerm_kubernetes_cluster.main",
			"type": "azurerm_kubernetes_cluster",
			"change": {"after": {"disk_encryption_set_id": null}},
		}]
	}
}

test_aks_empty_disk_encryption_set_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "azurerm_kubernetes_cluster.main",
			"type": "azurerm_kubernetes_cluster",
			"change": {"after": {"disk_encryption_set_id": ""}},
		}]
	}
}

test_aks_disk_encryption_set_allowed if {
	count(security.deny) == 0 with input as {
		"resource_changes": [{
			"address": "azurerm_kubernetes_cluster.main",
			"type": "azurerm_kubernetes_cluster",
			"change": {"after": {"disk_encryption_set_id": "/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Compute/diskEncryptionSets/des"}},
		}]
	}
}
