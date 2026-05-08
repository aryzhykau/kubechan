// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package detector_test

import (
	"testing"

	"github.com/org/kubechan/services/cluster-watcher/detector"
)

func TestDetectorNames(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		d    detector.Detector
	}{
		{"CrashLoopBackOff", &detector.CrashLoopBackOffDetector{}},
		{"DeploymentUnavailable", &detector.DeploymentUnavailableDetector{}},
		{"ImagePullBackOff", &detector.ImagePullBackOffDetector{}},
		{"PendingTooLong", &detector.PendingTooLongDetector{}},
		{"ServiceNoEndpoints", &detector.ServiceNoEndpointsDetector{}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.d.Name(); got != tc.name {
				t.Errorf("Name() = %q, want %q", got, tc.name)
			}
		})
	}
}
