// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"database/sql"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
)

// Incidents holds dependencies for Incident handlers.
type Incidents struct {
	K8s              client.Client
	DB               *sql.DB
	DefaultNamespace string
}

// incidentView wraps an Incident CRD with optional ownership metadata.
type incidentView struct {
	v1alpha1.Incident `json:",inline"`
	OwnerUsername     string `json:"ownerUsername,omitempty"`
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

	userID, _, role := UserFromCtx(r.Context())

	// Build ownership map for manual incidents in one query.
	ownerMap := map[string]string{} // incident name → owner username
	ownerIDMap := map[string]string{} // incident name → owner user ID
	if h.DB != nil {
		rows, err := h.DB.QueryContext(r.Context(),
			`SELECT mio.incident_id, u.username, mio.owner_id
			 FROM manual_incident_owners mio
			 JOIN users u ON u.id = mio.owner_id`,
		)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var incID, username, ownerID string
				if rows.Scan(&incID, &username, &ownerID) == nil {
					ownerMap[incID] = username
					ownerIDMap[incID] = ownerID
				}
			}
		}
	}

	views := make([]incidentView, 0, len(items))
	for _, inc := range items {
		if inc.Spec.Source == "manual" {
			ownerID := ownerIDMap[inc.Name]
			// Viewers only see their own manual incidents.
			if role != "admin" && ownerID != userID {
				continue
			}
			views = append(views, incidentView{Incident: inc, OwnerUsername: ownerMap[inc.Name]})
		} else {
			views = append(views, incidentView{Incident: inc})
		}
	}

	writeJSON(w, http.StatusOK, views)
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

	userID, _, role := UserFromCtx(r.Context())
	view := incidentView{Incident: *inc}

	if inc.Spec.Source == "manual" && h.DB != nil {
		var ownerID, ownerUsername string
		_ = h.DB.QueryRowContext(r.Context(),
			`SELECT mio.owner_id, u.username FROM manual_incident_owners mio
			 JOIN users u ON u.id = mio.owner_id WHERE mio.incident_id = ?`, name,
		).Scan(&ownerID, &ownerUsername)

		if role != "admin" && ownerID != userID {
			writeError(w, http.StatusForbidden, "incident not found")
			return
		}
		view.OwnerUsername = ownerUsername
	}

	writeJSON(w, http.StatusOK, view)
}

// checkManualIncidentAccess returns false and writes a 403 if the caller is a
// non-admin viewer who does not own the manual incident. Always returns true for
// auto incidents.
func (h *Incidents) checkManualIncidentAccess(w http.ResponseWriter, r *http.Request, incName string) bool {
	if h.DB == nil {
		return true
	}
	userID, _, role := UserFromCtx(r.Context())
	if role == "admin" {
		return true
	}
	var ownerID string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT owner_id FROM manual_incident_owners WHERE incident_id = ?`, incName,
	).Scan(&ownerID)
	if err == sql.ErrNoRows {
		// Not a manual incident — auto incidents are accessible to all.
		return true
	}
	if err != nil || ownerID != userID {
		writeError(w, http.StatusForbidden, "incident not found")
		return false
	}
	return true
}

// Resolve handles POST /api/v1/incidents/{id}/resolve
// Marks the Incident CRD status as resolved. Only valid for open incidents.
func (h *Incidents) Resolve(w http.ResponseWriter, r *http.Request) {
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

	if inc.Spec.Source == "manual" && !h.checkManualIncidentAccess(w, r, name) {
		return
	}

	if inc.Status.State == v1alpha1.IncidentStateResolved {
		writeJSON(w, http.StatusOK, inc) // idempotent
		return
	}
	now := metav1.NewTime(time.Now().UTC())
	inc.Status.State = v1alpha1.IncidentStateResolved
	inc.Status.ResolvedAt = &now
	if err := h.K8s.Status().Update(r.Context(), inc); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, inc)
}


