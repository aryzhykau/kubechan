// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
)

// Augment holds dependencies for the augment-incident handler.
type Augment struct {
	K8s              client.Client
	DB               *sql.DB
	DefaultNamespace string
}

// augmentRequest is the body for POST /api/v1/incidents/{id}/augment.
type augmentRequest struct {
	RelatedResources []relatedResourceIn `json:"relatedResources"`
}

// Augment handles POST /api/v1/incidents/{id}/augment.
// It merges new related resources into the existing Incident CRD, then creates a
// fresh DiagnosticRun so the evidence is re-collected with the extra context.
func (h *Augment) Augment(w http.ResponseWriter, r *http.Request) {
	ns, name := namespacedName(chi.URLParam(r, "id"), h.DefaultNamespace)

	var req augmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(req.RelatedResources) == 0 {
		writeError(w, http.StatusBadRequest, "relatedResources must not be empty")
		return
	}

	// Fetch existing incident.
	inc := &v1alpha1.Incident{}
	if err := h.K8s.Get(r.Context(), client.ObjectKey{Namespace: ns, Name: name}, inc); err != nil {
		writeError(w, http.StatusNotFound, "incident not found")
		return
	}

	// Merge new resources (deduplicate by kind+name+namespace).
	type rrKey struct{ kind, ns, name string }
	existing := map[rrKey]bool{}
	for _, rr := range inc.Spec.RelatedResources {
		existing[rrKey{rr.Kind, rr.Namespace, rr.Name}] = true
	}

	patch := client.MergeFrom(inc.DeepCopy())
	for _, rr := range req.RelatedResources {
		k := rrKey{rr.Kind, rr.Namespace, rr.Name}
		if !existing[k] {
			existing[k] = true
			inc.Spec.RelatedResources = append(inc.Spec.RelatedResources, v1alpha1.ResourceRef{
				Kind:      rr.Kind,
				Name:      rr.Name,
				Namespace: rr.Namespace,
			})
		}
	}
	if err := h.K8s.Patch(r.Context(), inc, patch); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("patching incident: %s", err))
		return
	}

	// Create a new DiagnosticRun for the updated incident.
	now := metav1.Now()
	dr := &v1alpha1.DiagnosticRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("dr-%s-", name),
			Namespace:    ns,
		},
		Spec: v1alpha1.DiagnosticRunSpec{
			IncidentRef: name,
			RequestedAt: &now,
		},
	}
	if err := h.K8s.Create(r.Context(), dr); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("creating DiagnosticRun: %s", err))
		return
	}

	statusPatch := client.MergeFrom(dr.DeepCopy())
	dr.Status.State = v1alpha1.DiagnosticRunStatePending
	if err := h.K8s.Status().Patch(r.Context(), dr, statusPatch); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("patching DiagnosticRun status: %s", err))
		return
	}

	userID, _, _ := UserFromCtx(r.Context())
	reqID := uuid.New().String()
	_, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO analysis_requests(id, incident_id, diagnostic_run_id, requested_at, status, triggered_by)
		 VALUES (?, ?, ?, ?, 'pending', ?)`,
		reqID, name, dr.Name, time.Now().UTC().Format(time.RFC3339), userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("inserting analysis_request: %s", err))
		return
	}

	writeJSON(w, http.StatusAccepted, analyzeResponse{
		DiagnosticRunID:   dr.Name,
		AnalysisRequestID: reqID,
	})
}
