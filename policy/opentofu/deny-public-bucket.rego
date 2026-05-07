# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

# package keese.opentofu.security
#
# deny-public-bucket.rego — deny S3, GCS, and Azure Blob storage resources
# used as OpenTofu state backends if they allow public read.
#
# S3  — aws_s3_bucket_public_access_block must set all four block_* = true,
#        OR the bucket must have no public ACL grant.
# GCS — google_storage_bucket must not have allUsers / allAuthenticatedUsers
#        IAM members.
# Azure — azurerm_storage_account must have allow_blob_public_access = false.

package keese.opentofu.security

import future.keywords.contains
import future.keywords.if
import future.keywords.in

# ---------------------------------------------------------------------------
# S3 — public access block
# ---------------------------------------------------------------------------

deny contains msg if {
	r := input.resource_changes[_]
	r.type == "aws_s3_bucket_public_access_block"
	after := r.change.after
	not _s3_fully_blocked(after)
	msg := sprintf(
		"[deny-public-bucket] S3 public access block for %q does not block all public access. Set block_public_acls, block_public_policy, ignore_public_acls, and restrict_public_buckets to true.",
		[r.address],
	)
}

_s3_fully_blocked(after) if {
	after.block_public_acls == true
	after.block_public_policy == true
	after.ignore_public_acls == true
	after.restrict_public_buckets == true
}

deny contains msg if {
	r := input.resource_changes[_]
	r.type == "aws_s3_bucket_acl"
	r.change.after.acl == "public-read"
	msg := sprintf(
		"[deny-public-bucket] S3 bucket ACL for %q is set to public-read. State buckets must not be publicly readable.",
		[r.address],
	)
}

deny contains msg if {
	r := input.resource_changes[_]
	r.type == "aws_s3_bucket_acl"
	r.change.after.acl == "public-read-write"
	msg := sprintf(
		"[deny-public-bucket] S3 bucket ACL for %q is set to public-read-write. State buckets must not be publicly accessible.",
		[r.address],
	)
}

# ---------------------------------------------------------------------------
# GCS — IAM members
# ---------------------------------------------------------------------------

_public_gcs_members := {"allUsers", "allAuthenticatedUsers"}

deny contains msg if {
	r := input.resource_changes[_]
	r.type == "google_storage_bucket_iam_member"
	r.change.after.member in _public_gcs_members
	msg := sprintf(
		"[deny-public-bucket] GCS bucket IAM binding %q grants access to %q. State buckets must not be publicly readable.",
		[r.address, r.change.after.member],
	)
}

deny contains msg if {
	r := input.resource_changes[_]
	r.type == "google_storage_bucket_iam_binding"
	member := r.change.after.members[_]
	member in _public_gcs_members
	msg := sprintf(
		"[deny-public-bucket] GCS bucket IAM binding %q grants access to %q. State buckets must not be publicly readable.",
		[r.address, member],
	)
}

# ---------------------------------------------------------------------------
# Azure Blob
# ---------------------------------------------------------------------------

deny contains msg if {
	r := input.resource_changes[_]
	r.type == "azurerm_storage_account"
	r.change.after.allow_blob_public_access == true
	msg := sprintf(
		"[deny-public-bucket] Azure Storage Account %q has allow_blob_public_access=true. State storage accounts must not allow public blob access.",
		[r.address],
	)
}
