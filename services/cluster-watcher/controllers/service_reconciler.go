// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	"github.com/org/kubechan/services/cluster-watcher/debounce"
	"github.com/org/kubechan/services/cluster-watcher/detector"
	"github.com/org/kubechan/services/cluster-watcher/problemcase"
)

// ServiceReconciler watches Services and EndpointSlices and runs service-scoped detectors.
//
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups=discovery.k8s.io,resources=endpointslices,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubechan.io,resources=problemcases,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=kubechan.io,resources=problemcases/status,verbs=get;update;patch
type ServiceReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Detectors        []detector.Detector
	Debouncer        *debounce.Debouncer
	ControlNamespace string
}

// SetupWithManager registers ServiceReconciler with the controller-runtime Manager.
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Map an EndpointSlice change to its owning Service.
	esToService := handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
		svcName := obj.GetLabels()["kubernetes.io/service-name"]
		if svcName == "" {
			return nil
		}
		return []reconcile.Request{{
			NamespacedName: types.NamespacedName{
				Namespace: obj.GetNamespace(),
				Name:      svcName,
			},
		}}
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Service{}).
		Watches(&discoveryv1.EndpointSlice{}, esToService).
		Complete(r)
}

func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// Skip resources in the control namespace — KubeChan doesn't monitor herself.
	if req.Namespace == r.ControlNamespace {
		return ctrl.Result{}, nil
	}

	svc := &corev1.Service{}
	if err := r.Get(ctx, req.NamespacedName, svc); err != nil {
		if errors.IsNotFound(err) {
			for _, d := range r.Detectors {
				r.Debouncer.Cancel(debounceKey(req.Namespace, "Service", req.Name, d.Name()))
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("getting Service %s: %w", req.NamespacedName, err)
	}

	ref := v1alpha1.ResourceRef{
		Namespace: svc.Namespace,
		Kind:      "Service",
		Name:      svc.Name,
	}

	for _, d := range r.Detectors {
		symptoms, err := d.Evaluate(ctx, svc, r.Client)
		if err != nil {
			logger.Error(err, "detector evaluation failed", "detector", d.Name())
			continue
		}

		key := debounceKey(svc.Namespace, "Service", svc.Name, d.Name())

		if len(symptoms) == 0 {
			r.Debouncer.Cancel(key)
			continue
		}

		capturedDetector := d.Name()
		capturedSeverity := highestSeverity(symptoms)
		capturedSymptoms := symptoms

		r.Debouncer.Debounce(key, func() {
			dctx := context.Background()
			if err := problemcase.CreateOrUpdate(dctx, r.Client, r.ControlNamespace, ref, capturedSeverity, capturedDetector, capturedSymptoms); err != nil {
				logger.Error(err, "failed to create/update ProblemCase", "service", req.NamespacedName, "detector", capturedDetector)
			}
		})
	}

	return ctrl.Result{}, nil
}
