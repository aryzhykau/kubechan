// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"os"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	"github.com/org/kubechan/services/cluster-watcher/controllers"
	"github.com/org/kubechan/services/cluster-watcher/debounce"
	"github.com/org/kubechan/services/cluster-watcher/detector"
	"github.com/org/kubechan/services/cluster-watcher/watcherconfig"
)

var scheme = runtime.NewScheme()

func init() {
	// clientgoscheme includes all standard Kubernetes types (Pod, Deployment, Service,
	// EndpointSlice, Node, Event, ReplicaSet, …).
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	ctrl.SetLogger(zap.New(zap.UseDevMode(envBool("DEV_MODE"))))
	setupLog := ctrl.Log.WithName("setup")

	// Seed initial thresholds from env vars (fallback if backend-api is not yet reachable).
	initDebounce := envDuration("DEBOUNCE_WINDOW_SECS", 30) * time.Second
	initPending := envDuration("PENDING_THRESHOLD_SECS", 300) * time.Second
	initUnavailable := envDuration("UNAVAILABLE_THRESHOLD_SECS", 300) * time.Second

	// Live-reloadable config — overridden by backend-api settings when available.
	wcfg := watcherconfig.New(initDebounce, initPending, initUnavailable)
	backendURL := os.Getenv("BACKEND_API_URL")
	if backendURL != "" {
		logFn := func(msg string, args ...any) { setupLog.Info(msg, args...) }
		wcfg.StartPolling(context.Background(), backendURL, 60*time.Second, logFn)
	} else {
		setupLog.Info("BACKEND_API_URL not set; using env-var thresholds only")
	}

	controlNamespace := os.Getenv("CONTROL_NAMESPACE")
	if controlNamespace == "" {
		controlNamespace = "kubechan"
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		HealthProbeBindAddress: ":8081",
		LeaderElection:         false,
	})
	if err != nil {
		setupLog.Error(err, "unable to create manager")
		os.Exit(1)
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	debouncerInst := debounce.New(wcfg.DebounceWindow)

	// All detectors — referenced by multiple reconcilers.
	podDetectors := []detector.Detector{
		&detector.CrashLoopBackOffDetector{},
		&detector.ImagePullBackOffDetector{},
		&detector.PendingTooLongDetector{Threshold: wcfg.PendingThreshold},
	}
	deployDetectors := []detector.Detector{
		&detector.DeploymentUnavailableDetector{Threshold: wcfg.UnavailableThreshold},
	}
	svcDetectors := []detector.Detector{
		&detector.ServiceNoEndpointsDetector{},
	}
	allDetectors := append(append(podDetectors, deployDetectors...), svcDetectors...)

	if err := (&controllers.PodReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Detectors:        podDetectors,
		Debouncer:        debouncerInst,
		ControlNamespace: controlNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to set up PodReconciler")
		os.Exit(1)
	}

	if err := (&controllers.DeploymentReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Detectors:        deployDetectors,
		Debouncer:        debouncerInst,
		ControlNamespace: controlNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to set up DeploymentReconciler")
		os.Exit(1)
	}

	if err := (&controllers.ServiceReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		Detectors:        svcDetectors,
		Debouncer:        debouncerInst,
		ControlNamespace: controlNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to set up ServiceReconciler")
		os.Exit(1)
	}

	if err := (&controllers.NodeReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to set up NodeReconciler")
		os.Exit(1)
	}

	if err := (&controllers.EventReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to set up EventReconciler")
		os.Exit(1)
	}

	if err := (&controllers.ProblemCaseReconciler{
		Client:    mgr.GetClient(),
		Scheme:    mgr.GetScheme(),
		Detectors: allDetectors,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to set up ProblemCaseReconciler")
		os.Exit(1)
	}

	if err := (&controllers.CorrelationReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		ControlNamespace: controlNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to set up CorrelationReconciler")
		os.Exit(1)
	}

	if err := (&controllers.ExclusionRuleReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		ControlNamespace: controlNamespace,
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to set up ExclusionRuleReconciler")
		os.Exit(1)
	}

	setupLog.Info("starting manager",
		"debounceWindow", wcfg.DebounceWindow(),
		"pendingThreshold", wcfg.PendingThreshold(),
		"unavailableThreshold", wcfg.UnavailableThreshold(),
		"controlNamespace", controlNamespace,
	)
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

// envDuration reads an env var as seconds; returns defaultSecs if unset or invalid.
func envDuration(key string, defaultSecs int64) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return time.Duration(defaultSecs)
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return time.Duration(defaultSecs)
	}
	return time.Duration(n)
}

// envBool reads an env var as a boolean.
func envBool(key string) bool {
	v := os.Getenv(key)
	b, _ := strconv.ParseBool(v)
	return b
}
