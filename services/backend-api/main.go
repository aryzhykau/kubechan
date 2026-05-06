// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	"github.com/org/kubechan/services/backend-api/db"
	"github.com/org/kubechan/services/backend-api/handler"
	k8sclient "github.com/org/kubechan/services/backend-api/k8s"
	"github.com/org/kubechan/services/backend-api/startup"
	"github.com/org/kubechan/services/backend-api/ws"
	ctrlcfg "sigs.k8s.io/controller-runtime/pkg/client/config"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	// ── Database ──────────────────────────────────────────────────────────────
	dbPath := envOr("DB_PATH", "/data/kubechan.db")
	database, err := db.Open(dbPath, logger)
	if err != nil {
		logger.Error("opening database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := startup.RecoverPendingRequests(ctx, database, logger); err != nil {
		logger.Error("startup recovery failed", "error", err)
		os.Exit(1)
	}
	db.StartPruner(ctx, database, logger)

	// ── Kubernetes client ─────────────────────────────────────────────────────
	k8s, err := k8sclient.NewClient()
	if err != nil {
		logger.Error("creating k8s client", "error", err)
		os.Exit(1)
	}

	// ── Controller-runtime cache for informers ────────────────────────────────
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = v1alpha1.AddToScheme(scheme)

	cfg, err := ctrlcfg.GetConfig()
	if err != nil {
		logger.Error("getting kubeconfig", "error", err)
		os.Exit(1)
	}
	informerCache, err := cache.New(cfg, cache.Options{Scheme: scheme})
	if err != nil {
		logger.Error("creating informer cache", "error", err)
		os.Exit(1)
	}
	go func() {
		if err := informerCache.Start(ctx); err != nil {
			logger.Error("informer cache error", "error", err)
		}
	}()
	informerCache.WaitForCacheSync(ctx)

	// ── WebSocket hub ─────────────────────────────────────────────────────────
	hub := ws.NewHub(logger)

	defaultNS := envOr("DEFAULT_NAMESPACE", "kubechan")

	// ── Mood syncer — singleton KubeChanState CRD ─────────────────────────────
	moodSyncer := &k8sclient.MoodSyncer{
		Client:    k8s,
		Hub:       hub,
		Namespace: defaultNS,
		Logger:    logger,
	}
	if err := moodSyncer.EnsureState(ctx); err != nil {
		logger.Error("ensuring KubeChanState singleton", "error", err)
		// non-fatal — mood will degrade gracefully to 0
	}

	watcher := &k8sclient.Watcher{Cache: informerCache, Hub: hub, MoodSyncer: moodSyncer, Logger: logger}
	go func() {
		if err := watcher.Start(ctx); err != nil {
			logger.Error("watcher error", "error", err)
		}
	}()

	// ── Handlers ──────────────────────────────────────────────────────────────

	incidents := &handler.Incidents{K8s: k8s, DefaultNamespace: defaultNS}
	problemcases := &handler.ProblemCases{K8s: k8s, DefaultNamespace: defaultNS}
	diagnosticruns := &handler.DiagnosticRuns{K8s: k8s, DB: database, DefaultNamespace: defaultNS}
	analysis := &handler.Analysis{K8s: k8s, DB: database, DefaultNamespace: defaultNS}
	settings := &handler.Settings{DB: database}
	kubechan := &handler.KubeChan{MoodSyncer: moodSyncer}
	resources := &handler.Resources{K8s: k8s}
	manualIncident := &handler.ManualIncident{K8s: k8s, DB: database, DefaultNamespace: defaultNS}
	augment := &handler.Augment{K8s: k8s, DB: database, DefaultNamespace: defaultNS}
	internal := &handler.Internal{
		K8s:              k8s,
		DB:               database,
		DefaultNamespace: defaultNS,
		LLMGatewayURL:    envOr("LLM_GATEWAY_URL", ""),
		Hub:              hub,
		MoodSyncer:       moodSyncer,
		Logger:           logger,
	}

	// ── Router ────────────────────────────────────────────────────────────────
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(structuredLogger(logger))

	r.Get("/healthz", healthz)
	r.Get("/readyz", readyz(database, k8s))
	r.Get("/ws", ws.ServeWS(hub, logger))

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/incidents", incidents.List)
		r.Get("/incidents/{id}", incidents.Get)
		r.Post("/incidents/manual", manualIncident.Create)
		r.Post("/incidents/{id}/analyze", analysis.Analyze)
		r.Post("/incidents/{id}/augment", augment.Augment)
		r.Post("/incidents/{id}/resolve", incidents.Resolve)
		r.Get("/incidents/{id}/evidence", analysis.GetEvidence)

		r.Get("/problemcases", problemcases.List)
		r.Get("/problemcases/{id}", problemcases.Get)

		r.Get("/diagnosticruns", diagnosticruns.List)
		r.Delete("/diagnosticruns", diagnosticruns.BulkDelete)
		r.Get("/diagnosticruns/{id}", diagnosticruns.Get)
		r.Delete("/diagnosticruns/{id}", diagnosticruns.Delete)
		r.Get("/diagnosticruns/{id}/evidence", diagnosticruns.GetEvidence)
		r.Get("/diagnosticruns/{id}/analysisresult", diagnosticruns.GetAnalysisResult)
		r.Get("/analysisresults/{id}", analysis.GetAnalysisResult)
		r.Post("/analysisresults/{id}/rate", analysis.RateAnalysisResult)

		r.Get("/settings", settings.Get)
		r.Put("/settings", settings.Update)
		r.Get("/persona/idle-message", settings.IdleMessage)

		r.Get("/namespaces", resources.ListNamespaces)
		r.Get("/namespaces/{ns}/resources", resources.ListResources)

		r.Get("/kubechan/state", kubechan.GetState)
		r.Post("/kubechan/poke", kubechan.Poke)
	})

	r.Route("/internal", func(r chi.Router) {
		r.Post("/evidence", internal.ReceiveEvidence)
	})

	// ── HTTP server ───────────────────────────────────────────────────────────
	port := envOr("PORT", "8080")
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("backend-api starting", "port", port, "version", "0.2.0")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		logger.Error("graceful shutdown error", "error", err)
	}
	logger.Info("backend-api stopped")
}

func healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func readyz(database interface{ Ping() error }, k8s interface{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := database.Ping(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "db unavailable"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}

// structuredLogger returns a chi middleware that logs requests via slog.
func structuredLogger(logger *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			logger.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", ww.Status(),
				"bytes", ww.BytesWritten(),
				"duration", time.Since(start).String(),
				"requestId", middleware.GetReqID(r.Context()),
			)
		})
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
