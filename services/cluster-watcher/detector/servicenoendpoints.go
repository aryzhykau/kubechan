// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package detector

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ServiceNoEndpointsDetector fires when a ClusterIP Service has no ready endpoints.
type ServiceNoEndpointsDetector struct{}

func (d *ServiceNoEndpointsDetector) Name() string { return "ServiceNoEndpoints" }

func (d *ServiceNoEndpointsDetector) Evaluate(ctx context.Context, obj client.Object, reader client.Reader) ([]Symptom, error) {
	svc, ok := obj.(*corev1.Service)
	if !ok {
		return nil, nil
	}

	// Only check non-headless ClusterIP services.
	if svc.Spec.Type != corev1.ServiceTypeClusterIP {
		return nil, nil
	}
	if svc.Spec.ClusterIP == "None" || svc.Spec.ClusterIP == "" {
		return nil, nil
	}

	esList := &discoveryv1.EndpointSliceList{}
	if err := reader.List(ctx, esList,
		client.InNamespace(svc.Namespace),
		client.MatchingLabels{"kubernetes.io/service-name": svc.Name},
	); err != nil {
		return nil, fmt.Errorf("listing EndpointSlices for service %s/%s: %w", svc.Namespace, svc.Name, err)
	}

	for _, es := range esList.Items {
		for _, ep := range es.Endpoints {
			if ep.Conditions.Ready != nil && *ep.Conditions.Ready {
				return nil, nil
			}
		}
	}

	return []Symptom{{
		Message:  fmt.Sprintf("service %q has no ready endpoints", svc.Name),
		Severity: "high",
	}}, nil
}
