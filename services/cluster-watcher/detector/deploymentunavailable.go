// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package detector

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultUnavailableThreshold = 5 * time.Minute

// DeploymentUnavailableDetector fires when a Deployment has had unavailable replicas
// for longer than Threshold, avoiding false positives during normal rollouts and
// initial provisioning.
type DeploymentUnavailableDetector struct {
	// Threshold is how long unavailability must persist before a symptom is raised.
	// Defaults to 5 minutes if zero.
	Threshold time.Duration
}

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

	// Fast-path: everything is fine.
	if deploy.Status.AvailableReplicas >= desired && deploy.Status.UnavailableReplicas == 0 {
		return nil, nil
	}
	if deploy.Status.AvailableReplicas == 0 && deploy.Status.UnavailableReplicas == 0 {
		// Deployment just created, no status yet.
		return nil, nil
	}

	threshold := d.Threshold
	if threshold == 0 {
		threshold = defaultUnavailableThreshold
	}

	// Use the Available condition's LastTransitionTime as the start of unavailability.
	// Fall back to CreationTimestamp for brand-new deployments that have no conditions yet.
	unavailableSince := deploy.CreationTimestamp.Time
	for _, cond := range deploy.Status.Conditions {
		if cond.Type == appsv1.DeploymentAvailable && cond.Status == corev1.ConditionFalse {
			unavailableSince = cond.LastTransitionTime.Time
			break
		}
	}

	if time.Since(unavailableSince) < threshold {
		return nil, nil
	}

	duration := time.Since(unavailableSince).Round(time.Second)
	var symptoms []Symptom

	if deploy.Status.AvailableReplicas == 0 {
		symptoms = append(symptoms, Symptom{
			Message:  fmt.Sprintf("deployment %q has 0/%d available replicas for %s", deploy.Name, desired, duration),
			Severity: "high",
		})
	} else if deploy.Status.UnavailableReplicas > 0 {
		symptoms = append(symptoms, Symptom{
			Message: fmt.Sprintf(
				"deployment %q has %d unavailable replica(s) (%d/%d ready) for %s",
				deploy.Name,
				deploy.Status.UnavailableReplicas,
				deploy.Status.ReadyReplicas,
				desired,
				duration,
			),
			Severity: "high",
		})
	}

	return symptoms, nil
}
