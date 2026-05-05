// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package k8s

import (
	"context"
	"log/slog"

	"sigs.k8s.io/controller-runtime/pkg/cache"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	kubews "github.com/org/kubechan/services/backend-api/ws"
)

// Watcher starts informers for ProblemCase, Incident, and DiagnosticRun and
// broadcasts change events through the WebSocket hub.
type Watcher struct {
	Cache      cache.Cache
	Hub        *kubews.Hub
	MoodSyncer *MoodSyncer
	Logger     *slog.Logger
}

// Start registers informers and blocks until ctx is cancelled.
func (w *Watcher) Start(ctx context.Context) error {
	if err := w.watchProblemCases(ctx); err != nil {
		return err
	}
	if err := w.watchIncidents(ctx); err != nil {
		return err
	}
	if err := w.watchDiagnosticRuns(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}

func (w *Watcher) watchProblemCases(ctx context.Context) error {
	inf, err := w.Cache.GetInformer(ctx, &v1alpha1.ProblemCase{})
	if err != nil {
		return err
	}
	inf.AddEventHandler(newProblemCaseHandler(w.Hub, w.Logger))
	return nil
}

func (w *Watcher) watchIncidents(ctx context.Context) error {
	inf, err := w.Cache.GetInformer(ctx, &v1alpha1.Incident{})
	if err != nil {
		return err
	}
	inf.AddEventHandler(newIncidentHandler(w.Hub, w.MoodSyncer, w.Logger))
	return nil
}

func (w *Watcher) watchDiagnosticRuns(ctx context.Context) error {
	inf, err := w.Cache.GetInformer(ctx, &v1alpha1.DiagnosticRun{})
	if err != nil {
		return err
	}
	inf.AddEventHandler(newDiagnosticRunHandler(w.Hub, w.Logger))
	return nil
}
