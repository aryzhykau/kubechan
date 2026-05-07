// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package exclusion

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
)

// IsExcluded checks whether any enabled KubechanExclusionRule in controlNS suppresses
// the given detector for obj. Returns (true, ruleName, nil) on the first matching rule,
// or (false, "", nil) when no rule matches.
func IsExcluded(
	ctx context.Context,
	reader client.Reader,
	controlNS string,
	obj client.Object,
	detectorName string,
) (bool, string, error) {
	var rules v1alpha1.KubechanExclusionRuleList
	if err := reader.List(ctx, &rules, client.InNamespace(controlNS)); err != nil {
		return false, "", fmt.Errorf("listing KubechanExclusionRules: %w", err)
	}

	now := time.Now()

	for _, rule := range rules.Items {
		if !rule.Spec.Enabled {
			continue
		}

		// Fast pre-filter: namespace scope set on the rule itself.
		if rule.Spec.Namespace != "" && rule.Spec.Namespace != obj.GetNamespace() {
			continue
		}

		// Resource match — OR between TargetResources and Selector.
		if !matchesResource(&rule.Spec, obj) {
			continue
		}

		// Detector match — empty list means suppress all.
		if !matchesDetector(rule.Spec.Detectors, detectorName) {
			continue
		}

		// Time window check — absent means 24/7.
		if rule.Spec.TimeWindow != nil {
			active, err := inTimeWindow(rule.Spec.TimeWindow, now)
			if err != nil {
				// Malformed time window — skip this rule rather than hard-failing.
				continue
			}
			if !active {
				continue
			}
		}

		return true, rule.Name, nil
	}

	return false, "", nil
}

// PatchMatchedStatus increments SuppressedCount and sets LastMatchedAt on the rule.
func PatchMatchedStatus(ctx context.Context, c client.Client, rule *v1alpha1.KubechanExclusionRule) error {
	patch := client.MergeFrom(rule.DeepCopy())
	now := metav1.Now()
	rule.Status.LastMatchedAt = &now
	rule.Status.SuppressedCount++
	if err := c.Status().Patch(ctx, rule, patch); err != nil {
		return fmt.Errorf("patching ExclusionRule %s status: %w", rule.Name, err)
	}
	return nil
}

// GetRule retrieves a single KubechanExclusionRule by name from controlNS.
// Returns nil (not an error) when the rule no longer exists.
func GetRule(ctx context.Context, reader client.Reader, controlNS, name string) (*v1alpha1.KubechanExclusionRule, error) {
	var rule v1alpha1.KubechanExclusionRule
	if err := reader.Get(ctx, client.ObjectKey{Namespace: controlNS, Name: name}, &rule); err != nil {
		return nil, client.IgnoreNotFound(err)
	}
	return &rule, nil
}

// ── internal helpers ──────────────────────────────────────────────────────────

func matchesResource(spec *v1alpha1.ExclusionRuleSpec, obj client.Object) bool {
	objKind := obj.GetObjectKind().GroupVersionKind().Kind
	objNS := obj.GetNamespace()
	objName := obj.GetName()

	// Check TargetResources (exact match).
	for _, ref := range spec.TargetResources {
		if ref.Kind == objKind && ref.Name == objName {
			// Namespace must match when specified.
			if ref.Namespace == "" || ref.Namespace == objNS {
				return true
			}
		}
	}

	// Check Selector.
	if sel := spec.Selector; sel != nil {
		// Namespace check within selector.
		if sel.Namespace != "" && sel.Namespace != objNS {
			return false
		}
		// Kind filter.
		if len(sel.Kinds) > 0 && !containsString(sel.Kinds, objKind) {
			return false
		}
		// Label subset match.
		objLabels := obj.GetLabels()
		for k, v := range sel.MatchLabels {
			if objLabels[k] != v {
				return false
			}
		}
		return true
	}

	return false
}

func matchesDetector(detectors []string, detectorName string) bool {
	if len(detectors) == 0 {
		return true
	}
	return containsString(detectors, detectorName)
}

// inTimeWindow reports whether now falls inside any period of the given time window.
func inTimeWindow(tw *v1alpha1.ExclusionTimeWindow, now time.Time) (bool, error) {
	loc, err := time.LoadLocation(tw.Timezone)
	if err != nil {
		return false, fmt.Errorf("invalid timezone %q: %w", tw.Timezone, err)
	}
	local := now.In(loc)
	dayName := local.Weekday().String()[:3] // "Mon", "Tue", ...

	hhmm := func(s string) (int, error) {
		parts := strings.SplitN(s, ":", 2)
		if len(parts) != 2 {
			return 0, fmt.Errorf("invalid time %q", s)
		}
		h, m := 0, 0
		if _, err := fmt.Sscanf(parts[0], "%d", &h); err != nil {
			return 0, err
		}
		if _, err := fmt.Sscanf(parts[1], "%d", &m); err != nil {
			return 0, err
		}
		return h*60 + m, nil
	}

	nowMins := local.Hour()*60 + local.Minute()

	for _, p := range tw.Periods {
		if !containsString(p.Days, dayName) {
			continue
		}
		startMins, err := hhmm(p.Start)
		if err != nil {
			continue
		}
		endMins, err := hhmm(p.End)
		if err != nil {
			continue
		}

		var active bool
		if endMins >= startMins {
			// Normal period: e.g. 09:00–18:00.
			active = nowMins >= startMins && nowMins < endMins
		} else {
			// Midnight-spanning period: e.g. 22:00–06:00.
			active = nowMins >= startMins || nowMins < endMins
		}
		if active {
			return true, nil
		}
	}
	return false, nil
}

func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
