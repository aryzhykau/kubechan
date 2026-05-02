// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// EventReconciler watches Kubernetes Events, filtered to relevant involvedObject kinds.
// No detectors are wired in Phase 1 — stub for future use.
//
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch
type EventReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// relevantEventKinds is the set of involvedObject kinds we care about.
var relevantEventKinds = map[string]bool{
	"Pod": true, "Deployment": true, "Service": true, "Node": true,
}

// SetupWithManager registers EventReconciler with the controller-runtime Manager.
func (r *EventReconciler) SetupWithManager(mgr ctrl.Manager) error {
	filter := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		event, ok := obj.(*corev1.Event)
		if !ok {
			return false
		}
		return relevantEventKinds[event.InvolvedObject.Kind]
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Event{}).
		WithEventFilter(filter).
		Complete(r)
}

func (r *EventReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	event := &corev1.Event{}
	if err := r.Get(ctx, req.NamespacedName, event); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("getting Event %s: %w", req.NamespacedName, err)
	}

	logger.V(1).Info("event reconciled — no detectors wired",
		"involvedObject", event.InvolvedObject.Name,
		"reason", event.Reason,
	)
	return ctrl.Result{}, nil
}
