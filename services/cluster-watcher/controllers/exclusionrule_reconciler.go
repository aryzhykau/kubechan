// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package controllers

import (
	"context"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
)

// ExclusionRuleReconciler watches KubechanExclusionRule objects. Whenever a rule is
// created or updated (and enabled), it scans all open Incidents and auto-resolves any
// whose root resource and ProblemCase detectors are matched by the rule.
//
// +kubebuilder:rbac:groups=kubechan.io,resources=kubechanexclusionrules,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubechan.io,resources=kubechanexclusionrules/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubechan.io,resources=incidents,verbs=get;list;watch
// +kubebuilder:rbac:groups=kubechan.io,resources=incidents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubechan.io,resources=problemcases,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=kubechan.io,resources=problemcases/status,verbs=get;update;patch
type ExclusionRuleReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	ControlNamespace string
}

// SetupWithManager registers ExclusionRuleReconciler with the controller-runtime Manager.
func (r *ExclusionRuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.KubechanExclusionRule{}).
		Named("exclusion-rule").
		Complete(r)
}

func (r *ExclusionRuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	rule := &v1alpha1.KubechanExclusionRule{}
	if err := r.Get(ctx, req.NamespacedName, rule); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !rule.Spec.Enabled {
		return ctrl.Result{}, nil
	}

	// If the rule has a time window, check whether we are inside it right now.
	// If not, requeue for when the next window opens.
	if rule.Spec.TimeWindow != nil {
		active, requeueIn := isWindowActive(rule.Spec.TimeWindow)
		if !active {
			if requeueIn > 0 {
				return ctrl.Result{RequeueAfter: requeueIn}, nil
			}
			return ctrl.Result{}, nil
		}
	}

	// List all open incidents in the control namespace.
	incidentList := &v1alpha1.IncidentList{}
	if err := r.List(ctx, incidentList, client.InNamespace(r.ControlNamespace)); err != nil {
		return ctrl.Result{}, err
	}

	resolved := 0
	for i := range incidentList.Items {
		inc := &incidentList.Items[i]
		if inc.Status.State == v1alpha1.IncidentStateResolved {
			continue
		}
		// Skip manual incidents — only auto-suppress auto-detected ones.
		if inc.Spec.Source == "manual" {
			continue
		}
		if r.incidentMatchesRule(inc, rule) {
			if err := r.resolveIncident(ctx, inc); err != nil {
				logger.Error(err, "resolving incident", "incident", inc.Name)
				continue
			}
			logger.Info("auto-resolved incident via exclusion rule",
				"incident", inc.Name, "rule", rule.Name)
			resolved++
		}
	}

	if resolved > 0 {
		// Update the rule's suppressed count.
		patch := client.MergeFrom(rule.DeepCopy())
		rule.Status.SuppressedCount += resolved
		now := metav1.Now()
		rule.Status.LastMatchedAt = &now
		if err := r.Status().Patch(ctx, rule, patch); err != nil {
			logger.Error(err, "updating ExclusionRule status")
		}
	}

	return ctrl.Result{}, nil
}

// incidentMatchesRule returns true if the incident's root resource is covered by the rule.
func (r *ExclusionRuleReconciler) incidentMatchesRule(
	inc *v1alpha1.Incident,
	rule *v1alpha1.KubechanExclusionRule,
) bool {
	spec := &rule.Spec
	root := inc.Spec.RootResource

	// Check namespace filter on the rule itself.
	if spec.Namespace != "" && spec.Namespace != root.Namespace {
		return false
	}

	// Must match at least one TargetResource or a Selector.
	if len(spec.TargetResources) > 0 {
		if !resourceRefMatchesAny(root, spec.TargetResources) {
			return false
		}
	} else if spec.Selector == nil {
		// No targeting criteria — rule is intentionally broad; allow it.
		// (An enabled rule with no targets/selector matches everything in its namespace.)
		if spec.Namespace == "" {
			return false // too broad with no namespace either — skip for safety
		}
	}

	return true
}

// resourceRefMatchesAny returns true if ref matches any entry in targets by kind + name + optional namespace.
func resourceRefMatchesAny(ref v1alpha1.ResourceRef, targets []v1alpha1.ResourceRef) bool {
	for _, t := range targets {
		if !kindMatches(ref.Kind, t.Kind) {
			continue
		}
		if t.Name != "" && t.Name != ref.Name {
			continue
		}
		if t.Namespace != "" && t.Namespace != ref.Namespace {
			continue
		}
		return true
	}
	return false
}

// kindMatches does a case-insensitive kind comparison.
func kindMatches(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// resolveIncident resolves all open ProblemCases of the incident, then resolves the incident.
func (r *ExclusionRuleReconciler) resolveIncident(ctx context.Context, inc *v1alpha1.Incident) error {
	now := metav1.Now()

	for _, pcName := range inc.Spec.ProblemCases {
		pc := &v1alpha1.ProblemCase{}
		if err := r.Get(ctx, client.ObjectKey{Namespace: inc.Namespace, Name: pcName}, pc); err != nil {
			continue // already gone — fine
		}
		if pc.Status.State == v1alpha1.ProblemCaseStateResolved {
			continue
		}
		patch := client.MergeFrom(pc.DeepCopy())
		pc.Status.State = v1alpha1.ProblemCaseStateResolved
		pc.Status.ResolvedAt = &now
		if err := r.Status().Patch(ctx, pc, patch); err != nil {
			return err
		}
	}

	patch := client.MergeFrom(inc.DeepCopy())
	inc.Status.State = v1alpha1.IncidentStateResolved
	inc.Status.ResolvedAt = &now
	inc.Status.ActiveProblemCases = 0
	return r.Status().Patch(ctx, inc, patch)
}

// isWindowActive returns whether the current moment falls inside one of the rule's time
// window periods. It also returns a suggested requeue duration (time until the next
// window opens) when inactive.
func isWindowActive(tw *v1alpha1.ExclusionTimeWindow) (bool, time.Duration) {
	loc, err := time.LoadLocation(tw.Timezone)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	weekday := now.Weekday().String()[:3] // "Mon", "Tue", …

	for _, p := range tw.Periods {
		for _, d := range p.Days {
			if d != weekday {
				continue
			}
			start := parseDayTime(now, p.Start, loc)
			end := parseDayTime(now, p.End, loc)
			// Handle midnight-spanning windows.
			if end.Before(start) {
				end = end.Add(24 * time.Hour)
			}
			if !now.Before(start) && now.Before(end) {
				return true, 0
			}
		}
	}
	return false, 5 * time.Minute // check again in 5 minutes
}

// parseDayTime parses "HH:MM" and returns a time.Time on the same calendar day as ref.
func parseDayTime(ref time.Time, hhmm string, loc *time.Location) time.Time {
	var h, m int
	_, _ = parseHHMM(hhmm, &h, &m)
	return time.Date(ref.Year(), ref.Month(), ref.Day(), h, m, 0, 0, loc)
}

func parseHHMM(s string, h, m *int) (int, int) {
	if len(s) == 5 && s[2] == ':' {
		*h = int(s[0]-'0')*10 + int(s[1]-'0')
		*m = int(s[3]-'0')*10 + int(s[4]-'0')
	}
	return *h, *m
}
