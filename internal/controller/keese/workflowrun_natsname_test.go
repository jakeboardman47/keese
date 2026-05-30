// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

package keese

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	keesev1alpha1 "github.com/keese-ai/keese/api/keese/v1alpha1"
)

// TestNATSStreamName regression-guards the leaked-stream bug: the delete path
// used to hardcode the tenant segment to the WorkflowRun UID, so the delete-time
// stream name never matched the provision-time name. Both paths now call
// natsStreamName, which resolves the tenant UID (Tenant CR → Workspace → run UID
// fallback). This asserts the resolution, and therefore that provision and
// delete compute an identical name.
func TestNATSStreamName(t *testing.T) {
	const (
		tenantUID = types.UID("tenant-uid-1111")
		wsUID     = types.UID("workspace-uid-2222")
		wfrUID    = types.UID("wfr-uid-3333")
	)

	tenant := &keesev1alpha1.Tenant{ObjectMeta: metav1.ObjectMeta{Name: "acme", UID: tenantUID}}
	wsWithTenant := &keesev1alpha1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-acme", Name: "ws1", UID: wsUID},
		Spec:       keesev1alpha1.WorkspaceSpec{TenantRef: corev1.ObjectReference{Name: "acme"}},
	}
	wsNoTenant := &keesev1alpha1.Workspace{
		ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-acme", Name: "ws1", UID: wsUID},
	}

	tests := []struct {
		name       string
		objs       []client.Object
		wantStream string
		wantSubj   string
	}{
		{
			name:       "resolves Tenant CR UID",
			objs:       []client.Object{tenant, wsWithTenant},
			wantStream: "keese-tenant-tenant-uid-1111-wf-wfr-uid-3333",
			wantSubj:   "keese.tenant.tenant-uid-1111.wf.wfr-uid-3333.>",
		},
		{
			name:       "falls back to Workspace UID when no TenantRef",
			objs:       []client.Object{wsNoTenant},
			wantStream: "keese-tenant-workspace-uid-2222-wf-wfr-uid-3333",
			wantSubj:   "keese.tenant.workspace-uid-2222.wf.wfr-uid-3333.>",
		},
		{
			name:       "falls back to run UID when Workspace missing",
			objs:       nil,
			wantStream: "keese-tenant-wfr-uid-3333-wf-wfr-uid-3333",
			wantSubj:   "keese.tenant.wfr-uid-3333.wf.wfr-uid-3333.>",
		},
	}

	s := runtime.NewScheme()
	if err := keesev1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("add scheme: %v", err)
	}

	wfr := &keesev1alpha1.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{Namespace: "tenant-acme", Name: "run1", UID: wfrUID},
		Spec:       keesev1alpha1.WorkflowRunSpec{WorkspaceRef: keesev1alpha1.LocalObjectReference{Name: "ws1"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := fake.NewClientBuilder().WithScheme(s).WithObjects(tc.objs...).Build()
			r := &WorkflowRunReconciler{Client: c}

			stream, subj := r.natsStreamName(context.Background(), wfr)
			if stream != tc.wantStream {
				t.Errorf("stream = %q, want %q", stream, tc.wantStream)
			}
			if subj != tc.wantSubj {
				t.Errorf("subject = %q, want %q", subj, tc.wantSubj)
			}
			// Provision and delete both call natsStreamName — verify identical output.
			stream2, _ := r.natsStreamName(context.Background(), wfr)
			if stream2 != stream {
				t.Errorf("non-deterministic stream name: %q != %q", stream2, stream)
			}
			// Regression: the tenant segment must not collapse to the run UID
			// (the old delete-path bug) when a tenant/workspace is resolvable.
			if len(tc.objs) > 0 && stream == "keese-tenant-wfr-uid-3333-wf-wfr-uid-3333" {
				t.Errorf("tenant segment collapsed to run UID (the leaked-stream bug): %q", stream)
			}
		})
	}
}
