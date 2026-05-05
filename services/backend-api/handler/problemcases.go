// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
)

// ProblemCases holds dependencies for ProblemCase handlers.
type ProblemCases struct {
	K8s              client.Client
	DefaultNamespace string
}

// List handles GET /api/v1/problemcases
func (h *ProblemCases) List(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")
	if ns == "" {
		ns = h.DefaultNamespace
	}
	severity := r.URL.Query().Get("severity")
	state := r.URL.Query().Get("state")

	list := &v1alpha1.ProblemCaseList{}
	opts := []client.ListOption{client.InNamespace(ns)}
	if err := h.K8s.List(r.Context(), list, opts...); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := list.Items
	if severity != "" || state != "" {
		filtered := items[:0]
		for _, pc := range items {
			if severity != "" && string(pc.Spec.Severity) != severity {
				continue
			}
			if state != "" && string(pc.Status.State) != state {
				continue
			}
			filtered = append(filtered, pc)
		}
		items = filtered
	}
	writeJSON(w, http.StatusOK, items)
}

// Get handles GET /api/v1/problemcases/{id}
func (h *ProblemCases) Get(w http.ResponseWriter, r *http.Request) {
	ns, name := namespacedName(chi.URLParam(r, "id"), h.DefaultNamespace)
	pc := &v1alpha1.ProblemCase{}
	if err := h.K8s.Get(r.Context(), client.ObjectKey{Namespace: ns, Name: name}, pc); err != nil {
		if apierrors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "problemcase not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pc)
}


