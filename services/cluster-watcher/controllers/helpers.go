// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	"github.com/org/kubechan/services/cluster-watcher/detector"
)

// severityOrder maps severity names to a numeric weight (higher = more severe).
var severityOrder = map[string]int{
	"critical": 4,
	"high":     3,
	"medium":   2,
	"low":      1,
}

// highestSeverity returns the most severe ProblemCaseSeverity from a slice of symptoms.
// Falls back to "low" if the slice is empty or contains unknown values.
func highestSeverity(symptoms []detector.Symptom) v1alpha1.ProblemCaseSeverity {
	best := 0
	result := v1alpha1.ProblemCaseSeverity("low")
	for _, s := range symptoms {
		if w := severityOrder[s.Severity]; w > best {
			best = w
			result = v1alpha1.ProblemCaseSeverity(s.Severity)
		}
	}
	return result
}

// debounceKey builds a stable debounce key for a specific resource + detector pair.
func debounceKey(namespace, kind, name, detectorName string) string {
	return fmt.Sprintf("%s/%s/%s/%s", namespace, kind, name, detectorName)
}

// ruleNamespace returns the namespace to re-scan when an exclusion rule changes.
// An empty Namespace on the rule means it covers all namespaces — we return ""
// so callers list across all namespaces.
func ruleNamespace(obj client.Object) string {
	rule, ok := obj.(*v1alpha1.KubechanExclusionRule)
	if !ok {
		return obj.GetNamespace()
	}
	return rule.Spec.Namespace
}

// ruleToPodsMapper returns a handler map function that re-queues all Pods
// in the namespace targeted by a KubechanExclusionRule.
func ruleToPodsMapper(c client.Client) func(context.Context, client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		ns := ruleNamespace(obj)
		var list corev1.PodList
		if err := c.List(ctx, &list, client.InNamespace(ns)); err != nil {
			return nil
		}
		reqs := make([]reconcile.Request, 0, len(list.Items))
		for _, p := range list.Items {
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&p)})
		}
		return reqs
	}
}

// ruleToServicesMapper returns a handler map function that re-queues all Services
// in the namespace targeted by a KubechanExclusionRule.
func ruleToServicesMapper(c client.Client) func(context.Context, client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		ns := ruleNamespace(obj)
		var list corev1.ServiceList
		if err := c.List(ctx, &list, client.InNamespace(ns)); err != nil {
			return nil
		}
		reqs := make([]reconcile.Request, 0, len(list.Items))
		for _, s := range list.Items {
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&s)})
		}
		return reqs
	}
}

// ruleToDeploymentsMapper returns a handler map function that re-queues all Deployments
// in the namespace targeted by a KubechanExclusionRule.
func ruleToDeploymentsMapper(c client.Client) func(context.Context, client.Object) []reconcile.Request {
	return func(ctx context.Context, obj client.Object) []reconcile.Request {
		ns := ruleNamespace(obj)
		var list appsv1.DeploymentList
		if err := c.List(ctx, &list, client.InNamespace(ns)); err != nil {
			return nil
		}
		reqs := make([]reconcile.Request, 0, len(list.Items))
		for _, d := range list.Items {
			reqs = append(reqs, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&d)})
		}
		return reqs
	}
}
