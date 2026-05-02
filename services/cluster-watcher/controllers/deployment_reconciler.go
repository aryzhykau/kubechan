// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
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

// DeploymentReconciler watches Deployments (and triggering ReplicaSets) and runs
// deployment-scoped detectors.
//
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=replicasets,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubechan.io,resources=problemcases,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=kubechan.io,resources=problemcases/status,verbs=get;update;patch
type DeploymentReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Detectors        []detector.Detector
	Debouncer        *debounce.Debouncer
	ControlNamespace string
}

// SetupWithManager registers DeploymentReconciler with the controller-runtime Manager.
func (r *DeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// Map a ReplicaSet change to its owner Deployment.
	rsToDeployment := handler.EnqueueRequestsFromMapFunc(func(_ context.Context, obj client.Object) []reconcile.Request {
		for _, ref := range obj.GetOwnerReferences() {
			if ref.Kind == "Deployment" {
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Namespace: obj.GetNamespace(),
						Name:      ref.Name,
					},
				}}
			}
		}
		return nil
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&appsv1.Deployment{}).
		Watches(&appsv1.ReplicaSet{}, rsToDeployment).
		Complete(r)
}

func (r *DeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	deploy := &appsv1.Deployment{}
	if err := r.Get(ctx, req.NamespacedName, deploy); err != nil {
		if errors.IsNotFound(err) {
			for _, d := range r.Detectors {
				r.Debouncer.Cancel(debounceKey(req.Namespace, "Deployment", req.Name, d.Name()))
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("getting Deployment %s: %w", req.NamespacedName, err)
	}

	ref := v1alpha1.ResourceRef{
		Namespace: deploy.Namespace,
		Kind:      "Deployment",
		Name:      deploy.Name,
	}

	for _, d := range r.Detectors {
		symptoms, err := d.Evaluate(ctx, deploy, r.Client)
		if err != nil {
			logger.Error(err, "detector evaluation failed", "detector", d.Name())
			continue
		}

		key := debounceKey(deploy.Namespace, "Deployment", deploy.Name, d.Name())

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
				logger.Error(err, "failed to create/update ProblemCase", "deployment", req.NamespacedName, "detector", capturedDetector)
			}
		})
	}

	return ctrl.Result{}, nil
}
