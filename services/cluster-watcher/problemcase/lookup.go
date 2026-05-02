// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package problemcase

import (
	"context"
	"fmt"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
)

const (
	LabelAffectedResource = "kubechan.io/affected-resource"
	LabelDetector         = "kubechan.io/detector"
)

// AffectedResourceLabelValue returns a Kubernetes-safe label value identifying the resource.
// Format: "<kind>.<namespace>.<name>" (lowercased, truncated to 63 chars).
func AffectedResourceLabelValue(ref v1alpha1.ResourceRef) string {
	ns := ref.Namespace
	if ns == "" {
		ns = "cluster"
	}
	v := strings.ToLower(fmt.Sprintf("%s.%s.%s", ref.Kind, ns, ref.Name))
	if len(v) > 63 {
		return v[:63]
	}
	return v
}

// FindOpen returns the first open (non-resolved) ProblemCase for the given resource and detector,
// or nil if none exists. It searches in controlNamespace (the KubeChan control plane namespace).
func FindOpen(ctx context.Context, c client.Client, controlNamespace string, ref v1alpha1.ResourceRef, detectorName string) (*v1alpha1.ProblemCase, error) {
	list := &v1alpha1.ProblemCaseList{}

	labelValue := AffectedResourceLabelValue(ref)
	if err := c.List(ctx, list,
		client.InNamespace(controlNamespace),
		client.MatchingLabels{
			LabelAffectedResource: labelValue,
			LabelDetector:         detectorName,
		},
	); err != nil {
		return nil, fmt.Errorf("listing ProblemCases: %w", err)
	}

	for i := range list.Items {
		if list.Items[i].Status.State != v1alpha1.ProblemCaseStateResolved {
			return &list.Items[i], nil
		}
	}
	return nil, nil
}
