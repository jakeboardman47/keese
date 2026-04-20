#!/usr/bin/env bats
# SPDX-License-Identifier: Apache-2.0
# Copyright (c) 2026 keese-ai

setup() {
  REPO_ROOT="$(git rev-parse --show-toplevel)"
  SCRIPT="${REPO_ROOT}/scripts/check-design-gate.sh"
  TMPDIR="$(mktemp -d)"
  # Create a minimal skeleton inside TMPDIR mirroring the real layout.
  mkdir -p "${TMPDIR}"/{api/workspace/v1alpha1,internal/controller/workspace,docs/{designs,specs,plans}}
  pushd "${TMPDIR}" >/dev/null
  git init -q
  git config user.email test@keese.ai
  git config user.name  test

  # Stub types + controller (has the sentinel + ≤20 LOC -> stub).
  cat > api/workspace/v1alpha1/workspace_types.go <<'EOF'
package v1alpha1
// TODO(design-gate): schema defined elsewhere
type WorkspaceSpec struct{}
type WorkspaceStatus struct{}
EOF

  cat > internal/controller/workspace/workspace_controller.go <<'EOF'
package workspace
// TODO(design-gate): see docs/designs
func Reconcile() error { return nil }
EOF

  cat > docs/plans/README.md <<'EOF'
---
gate_status: closed
---
# Plans
EOF
}

teardown() {
  popd >/dev/null
  rm -rf "${TMPDIR}"
}

@test "empty-stub scaffold passes gate check" {
  cp "${SCRIPT}" ./check-design-gate.sh
  run bash ./check-design-gate.sh
  [ "$status" -eq 0 ]
}

@test "non-stub controller without supporting design fails gate" {
  # Replace controller with a fat body lacking the sentinel.
  cat > internal/controller/workspace/workspace_controller.go <<'EOF'
package workspace

import (
	"context"
	ctrl "sigs.k8s.io/controller-runtime"
)

type WorkspaceReconciler struct{}

func (r *WorkspaceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// actual logic, no sentinel.
	_ = ctx
	_ = req
	// enough real lines to exceed the LOC_LIMIT (35).
	a := 1
	b := 2
	c := 3
	d := 4
	e := 5
	f := 6
	g := 7
	h := 8
	i := 9
	j := 10
	k := 11
	l := 12
	m := 13
	n := 14
	o := 15
	p := 16
	q := 17
	r2 := 18
	s := 19
	t := 20
	u := 21
	v := 22
	w := 23
	_ = a + b + c + d + e + f + g + h + i + j + k + l + m + n + o + p + q + r2 + s + t + u + v + w
	return ctrl.Result{}, nil
}

func (r *WorkspaceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return nil
}
EOF

  cp "${SCRIPT}" ./check-design-gate.sh
  run bash ./check-design-gate.sh
  [ "$status" -ne 0 ]
  [[ "$output" =~ "non-stub code but no docs/designs" ]]
}
