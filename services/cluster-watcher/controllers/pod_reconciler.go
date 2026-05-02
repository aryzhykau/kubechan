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

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	"github.com/org/kubechan/services/cluster-watcher/debounce"
	"github.com/org/kubechan/services/cluster-watcher/detector"
	"github.com/org/kubechan/services/cluster-watcher/problemcase"
)

// PodReconciler watches Pods and runs pod-scoped detectors.
//
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubechan.io,resources=problemcases,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=kubechan.io,resources=problemcases/status,verbs=get;update;patch
type PodReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Detectors        []detector.Detector
	Debouncer        *debounce.Debouncer
	ControlNamespace string
}

// SetupWithManager registers PodReconciler with the controller-runtime Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	pod := &corev1.Pod{}
	if err := r.Get(ctx, req.NamespacedName, pod); err != nil {
		if errors.IsNotFound(err) {
			for _, d := range r.Detectors {
				r.Debouncer.Cancel(debounceKey(req.Namespace, "Pod", req.Name, d.Name()))
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("getting Pod %s: %w", req.NamespacedName, err)
	}

	ref := v1alpha1.ResourceRef{
		Namespace: pod.Namespace,
		Kind:      "Pod",
		Name:      pod.Name,
	}

	for _, d := range r.Detectors {
		symptoms, err := d.Evaluate(ctx, pod, r.Client)
		if err != nil {
			logger.Error(err, "detector evaluation failed", "detector", d.Name())
			continue
		}

		key := debounceKey(pod.Namespace, "Pod", pod.Name, d.Name())

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
				logger.Error(err, "failed to create/update ProblemCase", "pod", req.NamespacedName, "detector", capturedDetector)
			}
		})
	}

	return ctrl.Result{}, nil
}
