# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

# package keese.opentofu.security
#
# deny-default-iam.rego — deny EKS node groups that use a default or shared
# IAM node role instead of a per-cluster explicit role.
#
# Heuristic: node role ARNs that contain "eks-default", "default-node",
# "AmazonEKSNodeRole", or are absent are rejected. Production modules must
# provision an explicit role per cluster (see deploy/opentofu/aws/main.tf).

package keese.opentofu.security

import future.keywords.contains
import future.keywords.if

_default_role_patterns := [
	"eks-default",
	"default-node",
	"AmazonEKSNodeRole",
]

deny contains msg if {
	r := input.resource_changes[_]
	r.type == "aws_eks_node_group"
	arn := r.change.after.node_role_arn
	_matches_default_pattern(arn)
	msg := sprintf(
		"[deny-default-iam] EKS node group %q uses a default/shared IAM role (%q). Provision an explicit per-cluster node role.",
		[r.address, arn],
	)
}

deny contains msg if {
	r := input.resource_changes[_]
	r.type == "aws_eks_node_group"
	not r.change.after.node_role_arn
	msg := sprintf(
		"[deny-default-iam] EKS node group %q has no node_role_arn set. An explicit per-cluster node IAM role is required.",
		[r.address],
	)
}

_matches_default_pattern(arn) if {
	pattern := _default_role_patterns[_]
	contains(arn, pattern)
}
