// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package detector

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ImagePullBackOffDetector fires when any container cannot pull its image.
type ImagePullBackOffDetector struct{}

func (d *ImagePullBackOffDetector) Name() string { return "ImagePullBackOff" }

func (d *ImagePullBackOffDetector) Evaluate(_ context.Context, obj client.Object, _ client.Reader) ([]Symptom, error) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return nil, nil
	}

	var symptoms []Symptom
	check := func(cs corev1.ContainerStatus) {
		if cs.State.Waiting == nil {
			return
		}
		switch cs.State.Waiting.Reason {
		case "ImagePullBackOff", "ErrImagePull":
			symptoms = append(symptoms, Symptom{
				Message:  fmt.Sprintf("container %q cannot pull image %q: %s", cs.Name, cs.Image, cs.State.Waiting.Reason),
				Severity: "high",
			})
		}
	}
	for _, cs := range pod.Status.ContainerStatuses {
		check(cs)
	}
	for _, cs := range pod.Status.InitContainerStatuses {
		check(cs)
	}
	return symptoms, nil
}
