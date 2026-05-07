# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

# package keese.opentofu.security
#
# deny-public-cluster.rego — deny any cluster resource that exposes a public
# API endpoint without a locked-down CIDR allowlist.
#
# Covered resource types:
#   - aws_eks_cluster  (EKS)
#   - google_container_cluster (GKE)
#   - azurerm_kubernetes_cluster (AKS)
#
# A cluster is denied when public access is enabled AND the CIDR allowlist is
# absent, empty, or contains 0.0.0.0/0.

package keese.opentofu.security

import future.keywords.contains
import future.keywords.if
import future.keywords.in

# ---------------------------------------------------------------------------
# EKS
# ---------------------------------------------------------------------------

deny contains msg if {
	r := input.resource_changes[_]
	r.type == "aws_eks_cluster"
	cfg := r.change.after.vpc_config[_]
	cfg.endpoint_public_access == true
	_public_cidrs_unrestricted(cfg.public_access_cidrs)
	msg := sprintf(
		"[deny-public-cluster] EKS cluster %q has endpoint_public_access=true with unrestricted CIDRs (%v). Set endpoint_public_access=false or restrict public_access_cidrs.",
		[r.address, cfg.public_access_cidrs],
	)
}

# ---------------------------------------------------------------------------
# GKE
# ---------------------------------------------------------------------------

deny contains msg if {
	r := input.resource_changes[_]
	r.type == "google_container_cluster"
	cfg := r.change.after.private_cluster_config[_]
	cfg.enable_private_endpoint == false
	not r.change.after.master_authorized_networks_config
	msg := sprintf(
		"[deny-public-cluster] GKE cluster %q has a public master endpoint with no master_authorized_networks_config. Set enable_private_endpoint=true or restrict master authorized networks.",
		[r.address],
	)
}

# ---------------------------------------------------------------------------
# AKS
# ---------------------------------------------------------------------------

deny contains msg if {
	r := input.resource_changes[_]
	r.type == "azurerm_kubernetes_cluster"
	r.change.after.api_server_access_profile != null
	profile := r.change.after.api_server_access_profile[_]
	_public_cidrs_unrestricted(profile.authorized_ip_ranges)
	msg := sprintf(
		"[deny-public-cluster] AKS cluster %q has an API server access profile with unrestricted IPs (%v). Restrict authorized_ip_ranges.",
		[r.address, profile.authorized_ip_ranges],
	)
}

# ---------------------------------------------------------------------------
# Helper
# ---------------------------------------------------------------------------

_public_cidrs_unrestricted(cidrs) if {
	cidrs == null
}

_public_cidrs_unrestricted(cidrs) if {
	is_array(cidrs)
	count(cidrs) == 0
}

_public_cidrs_unrestricted(cidrs) if {
	is_array(cidrs)
	"0.0.0.0/0" in cidrs
}
