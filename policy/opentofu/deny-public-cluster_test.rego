# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

package keese.opentofu.security_test

import data.keese.opentofu.security
import future.keywords.if

# ---------------------------------------------------------------------------
# deny-public-cluster — EKS
# ---------------------------------------------------------------------------

test_eks_public_no_cidrs_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "aws_eks_cluster.main",
			"type": "aws_eks_cluster",
			"change": {"after": {"vpc_config": [{
				"endpoint_public_access": true,
				"public_access_cidrs": [],
			}]}},
		}]
	}
}

test_eks_public_open_cidr_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "aws_eks_cluster.main",
			"type": "aws_eks_cluster",
			"change": {"after": {"vpc_config": [{
				"endpoint_public_access": true,
				"public_access_cidrs": ["0.0.0.0/0"],
			}]}},
		}]
	}
}

test_eks_public_null_cidrs_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "aws_eks_cluster.main",
			"type": "aws_eks_cluster",
			"change": {"after": {"vpc_config": [{
				"endpoint_public_access": true,
				"public_access_cidrs": null,
			}]}},
		}]
	}
}

test_eks_public_restricted_cidrs_allowed if {
	count(security.deny) == 0 with input as {
		"resource_changes": [{
			"address": "aws_eks_cluster.main",
			"type": "aws_eks_cluster",
			"change": {"after": {
				"vpc_config": [{"endpoint_public_access": true, "public_access_cidrs": ["203.0.113.0/24"]}],
				"encryption_config": [{"resources": ["secrets"], "provider": [{"key_arn": "arn:aws:kms:us-east-1:123456789012:key/mrk-abc"}]}],
			}},
		}]
	}
}

test_eks_private_only_allowed if {
	count(security.deny) == 0 with input as {
		"resource_changes": [{
			"address": "aws_eks_cluster.main",
			"type": "aws_eks_cluster",
			"change": {"after": {
				"vpc_config": [{"endpoint_public_access": false, "public_access_cidrs": []}],
				"encryption_config": [{"resources": ["secrets"], "provider": [{"key_arn": "arn:aws:kms:us-east-1:123456789012:key/mrk-abc"}]}],
			}},
		}]
	}
}

# ---------------------------------------------------------------------------
# deny-public-cluster — GKE
# ---------------------------------------------------------------------------

test_gke_public_master_no_authorized_networks_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "google_container_cluster.main",
			"type": "google_container_cluster",
			"change": {"after": {
				"private_cluster_config": [{"enable_private_endpoint": false}],
				"master_authorized_networks_config": null,
			}},
		}]
	}
}

test_gke_private_endpoint_allowed if {
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
