// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
)

const (
	// LabelIncident is the label on a ProblemCase pointing to its parent Incident name.
	LabelIncident = "kubechan.io/incident"
	// LabelRootResourceKind / LabelRootResourceName are placed on Incidents for indexed lookup.
	LabelRootResourceKind = "kubechan.io/root-resource-kind"
	LabelRootResourceName = "kubechan.io/root-resource-name"
)

// +kubebuilder:rbac:groups=kubechan.io,resources=incidents,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=kubechan.io,resources=incidents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=replicasets;statefulsets;daemonsets,verbs=get
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get

// CorrelationReconciler groups related ProblemCases into Incidents by walking
// the Kubernetes owner-reference chain to a common workload root.
type CorrelationReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	ControlNamespace string
}

// SetupWithManager registers CorrelationReconciler with the controller-runtime Manager.
func (r *CorrelationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.ProblemCase{}).
		Named("correlation").
		Complete(r)
}

func (r *CorrelationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	pc := &v1alpha1.ProblemCase{}
	if err := r.Get(ctx, req.NamespacedName, pc); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	incidentName := pc.Labels[LabelIncident]

	// A resolved ProblemCase may trigger Incident resolution.
	if pc.Status.State == v1alpha1.ProblemCaseStateResolved {
		if incidentName == "" {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, r.maybeResolveIncident(ctx, r.ControlNamespace, incidentName)
	}

	// Already correlated — nothing left to do.
	if incidentName != "" {
		return ctrl.Result{}, nil
	}

	// Determine the workload root for this ProblemCase.
	root, err := r.findRootResource(ctx, pc.Spec.AffectedResource)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("finding root resource: %w", err)
	}

	// Find an existing open Incident for the same root.
	incident, err := r.findOpenIncident(ctx, r.ControlNamespace, root)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("finding open incident: %w", err)
	}

	if incident == nil {
		incident, err = r.createIncident(ctx, r.ControlNamespace, root)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("creating incident: %w", err)
		}
		logger.Info("created Incident", "incident", incident.Name, "root", fmt.Sprintf("%s/%s", root.Kind, root.Name))
	}

	// Add this ProblemCase to the Incident's spec.problemCases list.
	if err := r.addProblemCaseToIncident(ctx, incident, pc.Name); err != nil {
		return ctrl.Result{}, fmt.Errorf("adding ProblemCase to Incident: %w", err)
	}

	// Label the ProblemCase so future reconciles are a no-op.
	patch := client.MergeFrom(pc.DeepCopy())
	if pc.Labels == nil {
		pc.Labels = make(map[string]string)
	}
	pc.Labels[LabelIncident] = incident.Name
	if err := r.Patch(ctx, pc, patch); err != nil {
		return ctrl.Result{}, fmt.Errorf("labelling ProblemCase: %w", err)
	}

	logger.Info("correlated ProblemCase to Incident", "problemcase", pc.Name, "incident", incident.Name)
	return ctrl.Result{}, nil
}

// findRootResource walks ownerReferences upward to the workload root.
// Pods climb through ReplicaSet → Deployment (or StatefulSet / DaemonSet / Job directly).
// Workload-level kinds are returned as-is.
func (r *CorrelationReconciler) findRootResource(ctx context.Context, ref v1alpha1.ResourceRef) (v1alpha1.ResourceRef, error) {
	gvk, ok := kindToGVK(ref.Kind)
	if !ok {
		// Unknown kind — treat it as its own root.
		return ref, nil
	}

	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	if err := r.Get(ctx, client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return ref, nil // resource gone; use as-is
		}
		return ref, err
	}

	for _, owner := range obj.GetOwnerReferences() {
		if owner.Controller != nil && *owner.Controller {
			parent := v1alpha1.ResourceRef{
				Namespace: ref.Namespace,
				Kind:      owner.Kind,
				Name:      owner.Name,
			}
			return r.findRootResource(ctx, parent) // recurse until no owner
		}
	}
	return ref, nil
}

// findOpenIncident returns the first non-resolved Incident whose rootResource matches.
func (r *CorrelationReconciler) findOpenIncident(ctx context.Context, namespace string, root v1alpha1.ResourceRef) (*v1alpha1.Incident, error) {
	list := &v1alpha1.IncidentList{}
	if err := r.List(ctx, list,
		client.InNamespace(namespace),
		client.MatchingLabels{
			LabelRootResourceKind: strings.ToLower(root.Kind),
			LabelRootResourceName: labelSafe(root.Name),
		},
	); err != nil {
		return nil, err
	}
	for i := range list.Items {
		inc := &list.Items[i]
		if inc.Status.State != v1alpha1.IncidentStateResolved {
			return inc, nil
		}
	}
	return nil, nil
}

// createIncident creates and initialises a new Incident for the given workload root.
func (r *CorrelationReconciler) createIncident(ctx context.Context, namespace string, root v1alpha1.ResourceRef) (*v1alpha1.Incident, error) {
	now := metav1.Now()
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("incident-%s-%s-", strings.ToLower(root.Kind), root.Name),
			Namespace:    namespace,
			Labels: map[string]string{
				LabelRootResourceKind: strings.ToLower(root.Kind),
				LabelRootResourceName: labelSafe(root.Name),
			},
		},
		Spec: v1alpha1.IncidentSpec{
			RootResource: root,
		},
	}
	if err := r.Create(ctx, inc); err != nil {
		return nil, err
	}
	// Initialise status via the status subresource.
	statusPatch := client.MergeFrom(inc.DeepCopy())
	inc.Status.State = v1alpha1.IncidentStateOpen
	inc.Status.OpenedAt = &now
	if err := r.Status().Patch(ctx, inc, statusPatch); err != nil {
		return nil, err
	}
	return inc, nil
}

// addProblemCaseToIncident appends pcName to Incident.spec.problemCases if not already present.
func (r *CorrelationReconciler) addProblemCaseToIncident(ctx context.Context, inc *v1alpha1.Incident, pcName string) error {
	for _, existing := range inc.Spec.ProblemCases {
		if existing == pcName {
			return nil // already listed
		}
	}
	specPatch := client.MergeFrom(inc.DeepCopy())
	inc.Spec.ProblemCases = append(inc.Spec.ProblemCases, pcName)
	if err := r.Patch(ctx, inc, specPatch); err != nil {
		return err
	}
	// Reflect updated count in status.
	statusPatch := client.MergeFrom(inc.DeepCopy())
	inc.Status.ActiveProblemCases = len(inc.Spec.ProblemCases)
	return r.Status().Patch(ctx, inc, statusPatch)
}

// maybeResolveIncident resolves the Incident if all member ProblemCases are resolved.
func (r *CorrelationReconciler) maybeResolveIncident(ctx context.Context, namespace, incidentName string) error {
	inc := &v1alpha1.Incident{}
	if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: incidentName}, inc); err != nil {
		return client.IgnoreNotFound(err)
	}
	if inc.Status.State == v1alpha1.IncidentStateResolved {
		return nil
	}
	if len(inc.Spec.ProblemCases) == 0 {
		return nil // nothing to evaluate yet
	}
	active := 0
	for _, pcName := range inc.Spec.ProblemCases {
		pc := &v1alpha1.ProblemCase{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: namespace, Name: pcName}, pc); err != nil {
			if apierrors.IsNotFound(err) {
				continue // deleted counts as resolved
			}
			return err
		}
		if pc.Status.State != v1alpha1.ProblemCaseStateResolved {
			active++
		}
	}
	patch := client.MergeFrom(inc.DeepCopy())
	inc.Status.ActiveProblemCases = active
	if active == 0 {
		now := metav1.Now()
		inc.Status.State = v1alpha1.IncidentStateResolved
		inc.Status.ResolvedAt = &now
	}
	return r.Status().Patch(ctx, inc, patch)
}

// kindToGVK maps known Kubernetes kinds to their GroupVersionKind for unstructured fetch.
func kindToGVK(kind string) (schema.GroupVersionKind, bool) {
	switch kind {
	case "Pod", "Service":
		return schema.GroupVersionKind{Group: "", Version: "v1", Kind: kind}, true
	case "ReplicaSet", "Deployment", "StatefulSet", "DaemonSet":
		return schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: kind}, true
	case "Job":
		return schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: kind}, true
	default:
		return schema.GroupVersionKind{}, false
	}
}

// labelSafe truncates s to 63 characters so it fits in a Kubernetes label value.
func labelSafe(s string) string {
	if len(s) > 63 {
		return s[:63]
	}
	return s
}
