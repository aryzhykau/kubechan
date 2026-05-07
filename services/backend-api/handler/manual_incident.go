// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
)

// ManualIncident holds dependencies for the manual incident creation handler.
type ManualIncident struct {
	K8s              client.Client
	DB               *sql.DB
	DefaultNamespace string
}

// manualIncidentRequest is the body for POST /api/v1/incidents/manual.
type manualIncidentRequest struct {
	Namespace        string              `json:"namespace"`
	ResourceKind     string              `json:"resourceKind"`
	ResourceName     string              `json:"resourceName"`
	UserMessage      string              `json:"userMessage"`
	RelatedResources []relatedResourceIn `json:"relatedResources"`
}

type relatedResourceIn struct {
	Kind           string   `json:"kind"`
	Name           string   `json:"name"`
	Namespace      string   `json:"namespace"`
	APIGroup       string   `json:"apiGroup,omitempty"`
	EvidenceSlices []string `json:"evidenceSlices,omitempty"`
}

// manualIncidentResponse is returned by POST /api/v1/incidents/manual.
type manualIncidentResponse struct {
	IncidentID        string `json:"incidentId"`
	DiagnosticRunID   string `json:"diagnosticRunId"`
	AnalysisRequestID string `json:"analysisRequestId"`
}

// Create handles POST /api/v1/incidents/manual
func (h *ManualIncident) Create(w http.ResponseWriter, r *http.Request) {
	var req manualIncidentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Validate required fields.
	if req.ResourceKind == "" {
		writeError(w, http.StatusBadRequest, "resourceKind is required")
		return
	}
	if req.ResourceName == "" {
		writeError(w, http.StatusBadRequest, "resourceName is required")
		return
	}
	if strings.TrimSpace(req.UserMessage) == "" {
		writeError(w, http.StatusBadRequest, "userMessage is required")
		return
	}
	if len(strings.TrimSpace(req.UserMessage)) < 10 {
		writeError(w, http.StatusBadRequest, "userMessage must be at least 10 characters")
		return
	}

	// The Incident and DiagnosticRun CRDs always live in the operator namespace.
	// The root resource namespace (from the request) goes only into the ResourceRef.
	incNS := h.DefaultNamespace

	rootNS := req.Namespace
	if rootNS == "" {
		rootNS = incNS
	}

	// Build related resources for the CRD spec.
	relatedRefs := make([]v1alpha1.ResourceRef, 0, len(req.RelatedResources))
	for _, rr := range req.RelatedResources {
		if rr.Kind == "" || rr.Name == "" {
			continue
		}
		rrNS := rr.Namespace
		if rrNS == "" {
			rrNS = rootNS
		}
		relatedRefs = append(relatedRefs, v1alpha1.ResourceRef{
			Kind:           rr.Kind,
			Name:           rr.Name,
			Namespace:      rrNS,
			APIGroup:       rr.APIGroup,
			EvidenceSlices: rr.EvidenceSlices,
		})
	}

	// Create the Incident CRD.
	now := metav1.Now()
	inc := &v1alpha1.Incident{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "manual-",
			Namespace:    incNS,
		},
		Spec: v1alpha1.IncidentSpec{
			Source:      "manual",
			UserMessage: req.UserMessage,
			RootResource: v1alpha1.ResourceRef{
				Kind:      req.ResourceKind,
				Name:      req.ResourceName,
				Namespace: rootNS,
			},
			RelatedResources: relatedRefs,
		},
	}
	if err := h.K8s.Create(r.Context(), inc); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("creating incident: %s", err))
		return
	}

	// Patch status to set openedAt and state.
	statusPatch := client.MergeFrom(inc.DeepCopy())
	inc.Status.State = v1alpha1.IncidentStateOpen
	inc.Status.OpenedAt = &now
	_ = h.K8s.Status().Patch(r.Context(), inc, statusPatch)

	// Create the DiagnosticRun CRD to kick off evidence collection.
	dr := &v1alpha1.DiagnosticRun{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: fmt.Sprintf("dr-%s-", inc.Name),
			Namespace:    incNS,
		},
		Spec: v1alpha1.DiagnosticRunSpec{
			IncidentRef: inc.Name,
			RequestedAt: &now,
		},
	}
	if err := h.K8s.Create(r.Context(), dr); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("creating diagnostic run: %s", err))
		return
	}

	// Initialise DiagnosticRun status to pending so the reconciler picks it up.
	drStatusPatch := client.MergeFrom(dr.DeepCopy())
	dr.Status.State = v1alpha1.DiagnosticRunStatePending
	if err := h.K8s.Status().Patch(r.Context(), dr, drStatusPatch); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("patching diagnostic run status: %s", err))
		return
	}

	// Record analysis_request row so the UI can poll status.
	reqID := uuid.New().String()
	userID, _, _ := UserFromCtx(r.Context())
	_, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO analysis_requests(id, incident_id, diagnostic_run_id, requested_at, status, source, triggered_by)
		 VALUES (?, ?, ?, ?, 'pending', 'manual', ?)`,
		reqID, inc.Name, dr.Name, time.Now().UTC().Format(time.RFC3339), userID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("inserting analysis request: %s", err))
		return
	}

	// Record ownership of the manual incident.
	if userID != "" {
		_, err = h.DB.ExecContext(r.Context(),
			`INSERT INTO manual_incident_owners(incident_id, namespace, owner_id) VALUES (?, ?, ?)`,
			inc.Name, incNS, userID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("recording incident owner: %s", err))
			return
		}
	}

	writeJSON(w, http.StatusCreated, manualIncidentResponse{
		IncidentID:        incNS + "/" + inc.Name,
		DiagnosticRunID:   dr.Name,
		AnalysisRequestID: reqID,
	})
}
