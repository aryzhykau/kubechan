// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package problemcase

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	"github.com/org/kubechan/services/cluster-watcher/detector"
)

// CreateOrUpdate ensures an open ProblemCase exists for the given resource + detector.
// If one already exists its status (lastSeen, symptoms) is patched.
// If none exists a new ProblemCase is created in controlNamespace (the KubeChan control plane namespace).
func CreateOrUpdate(
	ctx context.Context,
	c client.Client,
	controlNamespace string,
	ref v1alpha1.ResourceRef,
	severity v1alpha1.ProblemCaseSeverity,
	detectorName string,
	symptoms []detector.Symptom,
) error {
	existing, err := FindOpen(ctx, c, controlNamespace, ref, detectorName)
	if err != nil {
		return err
	}

	now := metav1.Now()
	msgs := make([]string, len(symptoms))
	for i, s := range symptoms {
		msgs[i] = s.Message
	}

	if existing != nil {
		return patchLastSeen(ctx, c, existing, now, msgs)
	}
	return createNew(ctx, c, controlNamespace, ref, severity, detectorName, msgs, now)
}

// Resolve marks an open ProblemCase as resolved.
func Resolve(ctx context.Context, c client.Client, pc *v1alpha1.ProblemCase) error {
	patch := client.MergeFrom(pc.DeepCopy())
	now := metav1.Now()
	pc.Status.State = v1alpha1.ProblemCaseStateResolved
	pc.Status.ResolvedAt = &now
	if err := c.Status().Patch(ctx, pc, patch); err != nil {
		return fmt.Errorf("resolving ProblemCase %s/%s: %w", pc.Namespace, pc.Name, err)
	}
	return nil
}

func createNew(
	ctx context.Context,
	c client.Client,
	controlNamespace string,
	ref v1alpha1.ResourceRef,
	severity v1alpha1.ProblemCaseSeverity,
	detectorName string,
	msgs []string,
	now metav1.Time,
) error {
	labelValue := AffectedResourceLabelValue(ref)

	pc := &v1alpha1.ProblemCase{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: strings.ToLower(fmt.Sprintf("%s-%s-", detectorName, ref.Kind)),
			Namespace:    controlNamespace,
			Labels: map[string]string{
				LabelAffectedResource: labelValue,
				LabelDetector:         detectorName,
			},
		},
		Spec: v1alpha1.ProblemCaseSpec{
			AffectedResource: ref,
			Detector:         detectorName,
			Severity:         severity,
			Symptoms:         msgs,
		},
	}

	if err := c.Create(ctx, pc); err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("creating ProblemCase: %w", err)
		}
		// Another reconciler created it first — find and patch instead.
		existing, lookupErr := FindOpen(ctx, c, controlNamespace, ref, detectorName)
		if lookupErr != nil {
			return lookupErr
		}
		if existing != nil {
			return patchLastSeen(ctx, c, existing, now, msgs)
		}
	}

	// Set initial status (requires a separate status update after create).
	patch := client.MergeFrom(pc.DeepCopy())
	pc.Status.State = v1alpha1.ProblemCaseStateOpen
	pc.Status.FirstSeen = &now
	pc.Status.LastSeen = &now
	if err := c.Status().Patch(ctx, pc, patch); err != nil {
		return fmt.Errorf("setting initial ProblemCase status: %w", err)
	}
	return nil
}

func patchLastSeen(ctx context.Context, c client.Client, pc *v1alpha1.ProblemCase, now metav1.Time, msgs []string) error {
	// Update spec.symptoms via main resource patch.
	specPatch := client.MergeFrom(pc.DeepCopy())
	pc.Spec.Symptoms = msgs
	if err := c.Patch(ctx, pc, specPatch); err != nil {
		return fmt.Errorf("patching ProblemCase spec %s/%s: %w", pc.Namespace, pc.Name, err)
	}

	// Update status.lastSeen via status subresource patch.
	statusPatch := client.MergeFrom(pc.DeepCopy())
	pc.Status.LastSeen = &now
	if err := c.Status().Patch(ctx, pc, statusPatch); err != nil {
		return fmt.Errorf("patching ProblemCase status %s/%s: %w", pc.Namespace, pc.Name, err)
	}
	return nil
}
