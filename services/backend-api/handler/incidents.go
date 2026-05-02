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

// Incidents holds dependencies for Incident handlers.
type Incidents struct {
	K8s              client.Client
	DefaultNamespace string
}

// List handles GET /api/v1/incidents
func (h *Incidents) List(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("namespace")
	if ns == "" {
		ns = h.DefaultNamespace
	}
	state := r.URL.Query().Get("state")

	list := &v1alpha1.IncidentList{}
	opts := []client.ListOption{client.InNamespace(ns)}
	if err := h.K8s.List(r.Context(), list, opts...); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items := list.Items
	if state != "" {
		filtered := items[:0]
		for _, inc := range items {
			if string(inc.Status.State) == state {
				filtered = append(filtered, inc)
			}
		}
		items = filtered
	}
	writeJSON(w, http.StatusOK, items)
}

// Get handles GET /api/v1/incidents/{id}
// id format: "namespace/name" or just "name" (uses default namespace)
func (h *Incidents) Get(w http.ResponseWriter, r *http.Request) {
	ns, name := namespacedName(chi.URLParam(r, "id"), h.DefaultNamespace)
	inc := &v1alpha1.Incident{}
	if err := h.K8s.Get(r.Context(), client.ObjectKey{Namespace: ns, Name: name}, inc); err != nil {
		if apierrors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "incident not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, inc)
}
