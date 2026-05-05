// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package k8s

import (
	"context"
	"log/slog"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	kubews "github.com/org/kubechan/services/backend-api/ws"
)

const moodSingletonName = "kubechan"

// pokeTTL is how long a poke streak stays active after the last poke.
const pokeTTL = 8 * time.Second

// MoodSyncer keeps the KubeChanState singleton up to date.
// It is called by incident event handlers and the poke HTTP endpoint.
type MoodSyncer struct {
	Client    client.Client
	Hub       *kubews.Hub
	Namespace string
	Logger    *slog.Logger
}

// EnsureState creates the KubeChanState singleton if it does not exist.
// Safe to call multiple times (idempotent).
func (s *MoodSyncer) EnsureState(ctx context.Context) error {
	state := &v1alpha1.KubeChanState{}
	err := s.Client.Get(ctx, client.ObjectKey{Namespace: s.Namespace, Name: moodSingletonName}, state)
	if err == nil {
		return nil // already exists
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	state = &v1alpha1.KubeChanState{
		ObjectMeta: metav1.ObjectMeta{
			Name:      moodSingletonName,
			Namespace: s.Namespace,
		},
	}
	if createErr := s.Client.Create(ctx, state); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
		return createErr
	}
	return nil
}

// SyncFromIncidents recomputes mood from the current open incident count.
// Called by the incident informer handler on every add/update/delete event.
func (s *MoodSyncer) SyncFromIncidents(ctx context.Context) {
	list := &v1alpha1.IncidentList{}
	if err := s.Client.List(ctx, list, client.InNamespace(s.Namespace)); err != nil {
		s.Logger.Error("moodsyncer: list incidents", "err", err)
		return
	}
	openCount := 0
	for _, inc := range list.Items {
		if inc.Status.State != v1alpha1.IncidentStateResolved {
			openCount++
		}
	}

	state := &v1alpha1.KubeChanState{}
	if err := s.Client.Get(ctx, client.ObjectKey{Namespace: s.Namespace, Name: moodSingletonName}, state); err != nil {
		s.Logger.Error("moodsyncer: get state", "err", err)
		return
	}

	patch := client.MergeFrom(state.DeepCopy())
	state.Status.OpenIncidentCount = openCount
	state.Status.MoodLevel = computeMoodLevel(openCount, effectivePokeCount(state))
	now := metav1.NewTime(time.Now().UTC())
	state.Status.UpdatedAt = &now

	if err := s.Client.Status().Patch(ctx, state, patch); err != nil {
		s.Logger.Error("moodsyncer: patch state", "err", err)
		return
	}
	s.broadcast(state)
}

// Poke increments the poke streak counter, refreshes the expiry, and recomputes mood.
// Returns the updated state.
func (s *MoodSyncer) Poke(ctx context.Context) (*v1alpha1.KubeChanState, error) {
	state := &v1alpha1.KubeChanState{}
	if err := s.Client.Get(ctx, client.ObjectKey{Namespace: s.Namespace, Name: moodSingletonName}, state); err != nil {
		return nil, err
	}

	patch := client.MergeFrom(state.DeepCopy())

	// Reset if previous streak already expired.
	if state.Status.PokeExpiresAt != nil && time.Now().After(state.Status.PokeExpiresAt.Time) {
		state.Status.PokeCount = 0
	}
	state.Status.PokeCount++
	expiry := metav1.NewTime(time.Now().UTC().Add(pokeTTL))
	state.Status.PokeExpiresAt = &expiry
	state.Status.MoodLevel = computeMoodLevel(state.Status.OpenIncidentCount, state.Status.PokeCount)
	now := metav1.NewTime(time.Now().UTC())
	state.Status.UpdatedAt = &now

	if err := s.Client.Status().Patch(ctx, state, patch); err != nil {
		return nil, err
	}
	s.broadcast(state)
	return state, nil
}

// GetMoodLevel returns the current mood level. Returns 0 (calm) on any error.
func (s *MoodSyncer) GetMoodLevel(ctx context.Context) int {
	state := &v1alpha1.KubeChanState{}
	if err := s.Client.Get(ctx, client.ObjectKey{Namespace: s.Namespace, Name: moodSingletonName}, state); err != nil {
		return 0
	}
	return int(state.Status.MoodLevel)
}

// effectivePokeCount returns the current poke count if the streak is still active, else 0.
func effectivePokeCount(state *v1alpha1.KubeChanState) int {
	if state.Status.PokeExpiresAt == nil || time.Now().After(state.Status.PokeExpiresAt.Time) {
		return 0
	}
	return state.Status.PokeCount
}

// computeMoodLevel derives the mood level from open incidents and active poke count.
func computeMoodLevel(openCount, activePokeCount int) v1alpha1.KubeChanMoodLevel {
	if openCount >= 3 || activePokeCount >= 5 {
		return v1alpha1.MoodRage
	}
	if openCount >= 1 || activePokeCount >= 2 {
		return v1alpha1.MoodIrritated
	}
	return v1alpha1.MoodCalm
}

func (s *MoodSyncer) broadcast(state *v1alpha1.KubeChanState) {
	if s.Hub == nil {
		return
	}
	s.Hub.Broadcast(kubews.Marshal(kubews.KubeChanStateEvent{
		BaseEvent:         kubews.BaseEvent{Type: kubews.EventKubeChanStateUpdated},
		MoodLevel:         int(state.Status.MoodLevel),
		OpenIncidentCount: state.Status.OpenIncidentCount,
		PokeCount:         state.Status.PokeCount,
	}))
}
