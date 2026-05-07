# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

package keese.opentofu.security_test

import data.keese.opentofu.security
import future.keywords.if

# ---------------------------------------------------------------------------
# deny-default-iam — EKS node group
# ---------------------------------------------------------------------------

test_node_group_default_role_pattern_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "aws_eks_node_group.main",
			"type": "aws_eks_node_group",
			"change": {"after": {"node_role_arn": "arn:aws:iam::123456789012:role/AmazonEKSNodeRole"}},
		}]
	}
}

test_node_group_eks_default_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "aws_eks_node_group.main",
			"type": "aws_eks_node_group",
			"change": {"after": {"node_role_arn": "arn:aws:iam::123456789012:role/eks-default-node"}},
		}]
	}
}

test_node_group_no_role_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "aws_eks_node_group.main",
			"type": "aws_eks_node_group",
			"change": {"after": {}},
		}]
	}
}

test_node_group_explicit_role_allowed if {
	count(security.deny) == 0 with input as {
		"resource_changes": [{
			"address": "aws_eks_node_group.main",
			"type": "aws_eks_node_group",
			"change": {"after": {"node_role_arn": "arn:aws:iam::123456789012:role/keese-prod-eks-node"}},
		}]
	}
}
