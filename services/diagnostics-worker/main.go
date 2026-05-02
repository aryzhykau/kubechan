// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"log/slog"
	"os"
	"strconv"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	"github.com/org/kubechan/services/diagnostics-worker/controllers"
)

const version = "0.1.0"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		logger.Error("add clientgo scheme", "err", err)
		os.Exit(1)
	}
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		logger.Error("add v1alpha1 scheme", "err", err)
		os.Exit(1)
	}

	backendURL := envOr("BACKEND_API_URL", "http://kubechan-backend-api:8080")
	logTailLines := envInt("LOG_TAIL_LINES", 200)
	prevLogLines := envInt("PREV_LOG_TAIL_LINES", 100)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: ":8081",
		LeaderElection:         false,
	})
	if err != nil {
		logger.Error("create manager", "err", err)
		os.Exit(1)
	}

	if err := (&controllers.DiagnosticRunReconciler{
		Client:           mgr.GetClient(),
		Logger:           logger,
		BackendAPIURL:    backendURL,
		LogTailLines:     logTailLines,
		PrevLogLines:     prevLogLines,
		CollectorVersion: version,
	}).SetupWithManager(mgr); err != nil {
		logger.Error("setup DiagnosticRunReconciler", "err", err)
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		logger.Error("add healthz", "err", err)
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		logger.Error("add readyz", "err", err)
		os.Exit(1)
	}

	logger.Info("diagnostics-worker starting", "version", version, "backendURL", backendURL)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error("manager exited", "err", err)
		os.Exit(1)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

