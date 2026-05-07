# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

# package keese.opentofu.security
#
# require-encryption-at-rest.rego — deny cluster resources that do not
# configure etcd / disk encryption.
#
# EKS  — encryption_config must include "secrets" in resources.
# GKE  — database_encryption.state must be "ENCRYPTED".
# AKS  — disk_encryption_set_id must be set (non-empty).

package keese.opentofu.security

import future.keywords.contains
import future.keywords.if
import future.keywords.in

# ---------------------------------------------------------------------------
# EKS secrets encryption
# ---------------------------------------------------------------------------

deny contains msg if {
	r := input.resource_changes[_]
	r.type == "aws_eks_cluster"
	not _eks_has_secrets_encryption(r.change.after)
	msg := sprintf(
		"[require-encryption-at-rest] EKS cluster %q is missing encryption_config for 'secrets'. Set secrets_encryption_key_arn in the module.",
		[r.address],
	)
}

_eks_has_secrets_encryption(after) if {
	ec := after.encryption_config[_]
	"secrets" in ec.resources
	ec.provider[_].key_arn != ""
}

# ---------------------------------------------------------------------------
# GKE database encryption
# ---------------------------------------------------------------------------

deny contains msg if {
	r := input.resource_changes[_]
	r.type == "google_container_cluster"
	not _gke_encrypted(r.change.after)
	msg := sprintf(
		"[require-encryption-at-rest] GKE cluster %q does not have database_encryption.state=ENCRYPTED. Set database_encryption_key in the module.",
		[r.address],
	)
}

_gke_encrypted(after) if {
	de := after.database_encryption[_]
	de.state == "ENCRYPTED"
	de.key_name != ""
}

# ---------------------------------------------------------------------------
# AKS disk encryption
# ---------------------------------------------------------------------------

deny contains msg if {
	r := input.resource_changes[_]
	r.type == "azurerm_kubernetes_cluster"
	not _aks_encrypted(r.change.after)
	msg := sprintf(
		"[require-encryption-at-rest] AKS cluster %q has no disk_encryption_set_id. Set disk_encryption_set_id in the module.",
		[r.address],
	)
}

_aks_encrypted(after) if {
	after.disk_encryption_set_id != null
	after.disk_encryption_set_id != ""
}
