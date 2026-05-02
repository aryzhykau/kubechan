// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	"github.com/org/kubechan/services/cluster-watcher/detector"
	"github.com/org/kubechan/services/cluster-watcher/problemcase"
)

// ProblemCaseReconciler watches ProblemCases and auto-resolves them when symptoms clear.
//
// +kubebuilder:rbac:groups=kubechan.io,resources=problemcases,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=kubechan.io,resources=problemcases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
type ProblemCaseReconciler struct {
	client.Client
	Scheme    *runtime.Scheme
	Detectors []detector.Detector
}

// SetupWithManager registers ProblemCaseReconciler with the controller-runtime Manager.
func (r *ProblemCaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ProblemCase{}).
		Complete(r)
}

func (r *ProblemCaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	pc := &v1alpha1.ProblemCase{}
	if err := r.Get(ctx, req.NamespacedName, pc); err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("getting ProblemCase %s: %w", req.NamespacedName, err)
	}

	// Only act on open ProblemCases.
	if pc.Status.State != v1alpha1.ProblemCaseStateOpen {
		return ctrl.Result{}, nil
	}

	// Fetch the affected resource from cache.
	obj, err := r.fetchAffectedResource(ctx, pc.Spec.AffectedResource)
	if err != nil {
		if errors.IsNotFound(err) {
			// Resource deleted — resolve the ProblemCase.
			logger.Info("affected resource not found, resolving ProblemCase", "problemcase", req.NamespacedName)
			return ctrl.Result{}, problemcase.Resolve(ctx, r.Client, pc)
		}
		return ctrl.Result{}, fmt.Errorf("fetching affected resource: %w", err)
	}

	// Re-run the specific detector for this ProblemCase.
	d := r.findDetector(pc.Spec.Detector)
	if d == nil {
		logger.Info("detector not found for ProblemCase, skipping", "detector", pc.Spec.Detector)
		return ctrl.Result{RequeueAfter: requeueAfter}, nil
	}

	symptoms, err := d.Evaluate(ctx, obj, r.Client)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("evaluating detector %q: %w", d.Name(), err)
	}

	if len(symptoms) == 0 {
		logger.Info("no symptoms, resolving ProblemCase", "problemcase", req.NamespacedName)
		return ctrl.Result{}, problemcase.Resolve(ctx, r.Client, pc)
	}

	// Symptoms still present — requeue to check again later.
	return ctrl.Result{RequeueAfter: requeueAfter}, nil
}

const requeueAfter = 60 * time.Second

func (r *ProblemCaseReconciler) findDetector(name string) detector.Detector {
	for _, d := range r.Detectors {
		if d.Name() == name {
			return d
		}
	}
	return nil
}

// fetchAffectedResource retrieves the affected resource as a typed client.Object.
func (r *ProblemCaseReconciler) fetchAffectedResource(ctx context.Context, ref v1alpha1.ResourceRef) (client.Object, error) {
	key := client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}

	switch ref.Kind {
	case "Pod":
		obj := &corev1.Pod{}
		return obj, r.Get(ctx, key, obj)
	case "Deployment":
		obj := &appsv1.Deployment{}
		return obj, r.Get(ctx, key, obj)
	case "Service":
		obj := &corev1.Service{}
		return obj, r.Get(ctx, key, obj)
	case "Node":
		obj := &corev1.Node{}
		return obj, r.Get(ctx, key, obj)
	default:
		return nil, fmt.Errorf("unsupported kind %q in ProblemCase", ref.Kind)
	}
}
