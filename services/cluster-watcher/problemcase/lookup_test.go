// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package problemcase

import (
	"testing"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
)

// ── AffectedResourceLabelValue ────────────────────────────────────────────────

func TestAffectedResourceLabelValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		ref  v1alpha1.ResourceRef
		want string
	}{
		{
			name: "namespaced resource",
			ref:  v1alpha1.ResourceRef{Kind: "Deployment", Namespace: "default", Name: "my-app"},
			want: "deployment.default.my-app",
		},
		{
			name: "cluster-scoped resource uses 'cluster'",
			ref:  v1alpha1.ResourceRef{Kind: "Node", Namespace: "", Name: "node-1"},
			want: "node.cluster.node-1",
		},
		{
			name: "uppercased kind is lowercased",
			ref:  v1alpha1.ResourceRef{Kind: "StatefulSet", Namespace: "prod", Name: "DB"},
			want: "statefulset.prod.db",
		},
		{
			name: "value truncated at 63 chars",
			ref: v1alpha1.ResourceRef{
				Kind:      "Deployment",
				Namespace: "very-long-namespace-name-that-makes-this-exceed-the-limit",
				Name:      "also-long-name",
			},
			// just verify length constraint
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := AffectedResourceLabelValue(tc.ref)
			if len(got) > 63 {
				t.Errorf("label value too long: %d chars: %q", len(got), got)
			}
			if tc.want != "" && got != tc.want {
				t.Errorf("AffectedResourceLabelValue() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAffectedResourceLabelValue_TruncatedTo63(t *testing.T) {
	t.Parallel()
	ref := v1alpha1.ResourceRef{
		Kind:      "Deployment",
		Namespace: "this-is-a-very-long-namespace-name-exceeding-all-limits",
		Name:      "this-is-also-a-very-long-application-name-exceeding-limits",
	}
	got := AffectedResourceLabelValue(ref)
	if len(got) > 63 {
		t.Errorf("expected label value ≤63 chars, got %d: %q", len(got), got)
	}
}

// ── Label constants ───────────────────────────────────────────────────────────

func TestLabelConstants(t *testing.T) {
	t.Parallel()
	if LabelAffectedResource != "kubechan.io/affected-resource" {
		t.Errorf("LabelAffectedResource = %q", LabelAffectedResource)
	}
	if LabelDetector != "kubechan.io/detector" {
		t.Errorf("LabelDetector = %q", LabelDetector)
	}
}
