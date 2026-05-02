// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package detector

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Symptom describes a single detected problem symptom.
type Symptom struct {
	// Message is a human-readable description of the symptom.
	Message string
	// Severity is the assessed severity: critical|high|medium|low.
	Severity string
}

// Detector evaluates a Kubernetes object for known problem patterns.
// Implementations must be safe for concurrent use.
type Detector interface {
	// Name returns the stable, unique name of this detector (used as label value).
	Name() string
	// Evaluate inspects obj and returns any observed symptoms.
	// reader is backed by the controller-runtime informer cache — no extra API-server calls.
	Evaluate(ctx context.Context, obj client.Object, reader client.Reader) ([]Symptom, error)
}
