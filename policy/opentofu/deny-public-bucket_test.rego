# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

package keese.opentofu.security_test

import data.keese.opentofu.security
import future.keywords.if

# ---------------------------------------------------------------------------
# deny-public-bucket — S3
# ---------------------------------------------------------------------------

test_s3_public_read_acl_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "aws_s3_bucket_acl.state",
			"type": "aws_s3_bucket_acl",
			"change": {"after": {"acl": "public-read"}},
		}]
	}
}

test_s3_public_read_write_acl_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "aws_s3_bucket_acl.state",
			"type": "aws_s3_bucket_acl",
			"change": {"after": {"acl": "public-read-write"}},
		}]
	}
}

test_s3_private_acl_allowed if {
	count(security.deny) == 0 with input as {
		"resource_changes": [{
			"address": "aws_s3_bucket_acl.state",
			"type": "aws_s3_bucket_acl",
			"change": {"after": {"acl": "private"}},
		}]
	}
}

test_s3_public_access_block_incomplete_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "aws_s3_bucket_public_access_block.state",
			"type": "aws_s3_bucket_public_access_block",
			"change": {"after": {
				"block_public_acls": true,
				"block_public_policy": false,
				"ignore_public_acls": true,
				"restrict_public_buckets": true,
			}},
		}]
	}
}

test_s3_public_access_block_full_allowed if {
	count(security.deny) == 0 with input as {
		"resource_changes": [{
			"address": "aws_s3_bucket_public_access_block.state",
			"type": "aws_s3_bucket_public_access_block",
			"change": {"after": {
				"block_public_acls": true,
				"block_public_policy": true,
				"ignore_public_acls": true,
				"restrict_public_buckets": true,
			}},
		}]
	}
}

# ---------------------------------------------------------------------------
# deny-public-bucket — GCS
# ---------------------------------------------------------------------------

test_gcs_allusers_iam_member_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "google_storage_bucket_iam_member.state",
			"type": "google_storage_bucket_iam_member",
			"change": {"after": {"member": "allUsers"}},
		}]
	}
}

test_gcs_allauthenticated_iam_binding_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "google_storage_bucket_iam_binding.state",
			"type": "google_storage_bucket_iam_binding",
			"change": {"after": {"members": ["allAuthenticatedUsers"]}},
		}]
	}
}

test_gcs_specific_member_allowed if {
	count(security.deny) == 0 with input as {
		"resource_changes": [{
			"address": "google_storage_bucket_iam_member.state",
			"type": "google_storage_bucket_iam_member",
			"change": {"after": {"member": "serviceAccount:terraform@project.iam.gserviceaccount.com"}},
		}]
	}
}

# ---------------------------------------------------------------------------
# deny-public-bucket — Azure
# ---------------------------------------------------------------------------

test_azure_storage_public_blob_denied if {
	count(security.deny) > 0 with input as {
		"resource_changes": [{
			"address": "azurerm_storage_account.state",
			"type": "azurerm_storage_account",
			"change": {"after": {"allow_blob_public_access": true}},
		}]
	}
}

test_azure_storage_no_public_blob_allowed if {
	count(security.deny) == 0 with input as {
		"resource_changes": [{
			"address": "azurerm_storage_account.state",
			"type": "azurerm_storage_account",
			"change": {"after": {"allow_blob_public_access": false}},
		}]
	}
}
