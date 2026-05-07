// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 keese-ai

// Package podexec wraps client-go remotecommand for executing
// commands inside Kubernetes pods. SPI providers receive this
// implementation so they can shell into agent containers for Drain,
// Resume, and Bootstrap.
package podexec

import (
	"bytes"
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
)

// Executor wraps a kubernetes.Interface + REST config so it can
// stream commands into pod containers.
type Executor struct {
	clientset *kubernetes.Clientset
	cfg       *rest.Config
}

// New constructs an Executor bound to the given REST config.
func New(cfg *rest.Config) (*Executor, error) {
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("podexec: build clientset: %w", err)
	}
	return &Executor{clientset: cs, cfg: cfg}, nil
}

// Exec satisfies the SPI's PodExecutor interface (see
// internal/runtime/providers/goose for the shape). It streams stdout
// and stderr into separate buffers and returns an error when the
// remote command exits non-zero or the SPDY connection fails.
func (e *Executor) Exec(ctx context.Context, namespace, podName, container string, argv []string) (stdout, stderr []byte, err error) {
	req := e.clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   argv,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(e.cfg, "POST", req.URL())
	if err != nil {
		return nil, nil, fmt.Errorf("podexec: spdy: %w", err)
	}
	var so, se bytes.Buffer
	if err := exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &so,
		Stderr: &se,
	}); err != nil {
		return so.Bytes(), se.Bytes(), fmt.Errorf("podexec: stream: %w", err)
	}
	return so.Bytes(), se.Bytes(), nil
}
