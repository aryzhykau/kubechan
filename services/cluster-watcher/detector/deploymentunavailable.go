// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package detector

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeploymentUnavailableDetector fires when a Deployment has unavailable replicas.
type DeploymentUnavailableDetector struct{}

func (d *DeploymentUnavailableDetector) Name() string { return "DeploymentUnavailable" }

func (d *DeploymentUnavailableDetector) Evaluate(_ context.Context, obj client.Object, _ client.Reader) ([]Symptom, error) {
	deploy, ok := obj.(*appsv1.Deployment)
	if !ok {
		return nil, nil
	}

	desired := int32(1)
	if deploy.Spec.Replicas != nil {
		desired = *deploy.Spec.Replicas
	}
	if desired == 0 {
		return nil, nil
	}

	var symptoms []Symptom

	if deploy.Status.AvailableReplicas == 0 {
		symptoms = append(symptoms, Symptom{
			Message:  fmt.Sprintf("deployment %q has 0/%d available replicas", deploy.Name, desired),
			Severity: "high",
		})
	} else if deploy.Status.UnavailableReplicas > 0 {
		symptoms = append(symptoms, Symptom{
			Message: fmt.Sprintf(
				"deployment %q has %d unavailable replica(s) (%d/%d ready)",
				deploy.Name,
				deploy.Status.UnavailableReplicas,
				deploy.Status.ReadyReplicas,
				desired,
			),
			Severity: "high",
		})
	}

	return symptoms, nil
}
