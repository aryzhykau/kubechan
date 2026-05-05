// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
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

	debounceWindow := envDuration("DEBOUNCE_WINDOW_SECS", 30) * time.Second
	pendingThreshold := envDuration("PENDING_THRESHOLD_SECS", 300) * time.Second
	unavailableThreshold := envDuration("UNAVAILABLE_THRESHOLD_SECS", 300) * time.Second

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

	debouncerInst := debounce.New(debounceWindow)

	// All detectors — referenced by multiple reconcilers.
	podDetectors := []detector.Detector{
		&detector.CrashLoopBackOffDetector{},
		&detector.ImagePullBackOffDetector{},
		&detector.PendingTooLongDetector{Threshold: pendingThreshold},
	}
	deployDetectors := []detector.Detector{
		&detector.DeploymentUnavailableDetector{Threshold: unavailableThreshold},
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

	setupLog.Info("starting manager", "debounceWindow", debounceWindow, "pendingThreshold", pendingThreshold, "controlNamespace", controlNamespace)
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
