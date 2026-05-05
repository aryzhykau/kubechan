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

// Analysis holds dependencies for analysis-related handlers.
type Analysis struct {
	K8s              client.Client
	DB               *sql.DB
	DefaultNamespace string
}

// analyzeResponse is returned by POST /api/v1/incidents/{id}/analyze.
type analyzeResponse struct {
	DiagnosticRunID   string `json:"diagnosticRunId"`
	AnalysisRequestID string `json:"analysisRequestId"`
}

// Analyze handles POST /api/v1/incidents/{id}/analyze
// It creates a DiagnosticRun CRD and records an analysis_request row.
func (h *Analysis) Analyze(w http.ResponseWriter, r *http.Request) {
	ns, name := namespacedName(chi.URLParam(r, "id"), h.DefaultNamespace)

	// Verify incident exists.
	inc := &v1alpha1.Incident{}
	if err := h.K8s.Get(r.Context(), client.ObjectKey{Namespace: ns, Name: name}, inc); err != nil {
		writeError(w, http.StatusNotFound, "incident not found")
		return
	}

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

	// Initialise status to pending.
	statusPatch := client.MergeFrom(dr.DeepCopy())
	dr.Status.State = v1alpha1.DiagnosticRunStatePending
	if err := h.K8s.Status().Patch(r.Context(), dr, statusPatch); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("patching DiagnosticRun status: %s", err))
		return
	}

	reqID := uuid.New().String()
	_, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO analysis_requests(id, incident_id, diagnostic_run_id, requested_at, status)
		 VALUES (?, ?, ?, ?, 'pending')`,
		reqID, name, dr.Name, time.Now().UTC().Format(time.RFC3339),
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

// GetAnalysisResult handles GET /api/v1/analysisresults/{id}
func (h *Analysis) GetAnalysisResult(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	row := h.DB.QueryRowContext(r.Context(),
		`SELECT id, incident_id, diagnostic_run_id, model, status, likely_root_cause,
		        confidence, payload, created_at, COALESCE(user_rating, '') as user_rating
		 FROM analysis_results WHERE id = ?`, id)

	var (
		resultID, diagnosticRunID, model, status, payload, createdAt, userRating string
		incidentID, likelyRootCause                                               sql.NullString
		confidence                                                                 sql.NullFloat64
	)
	if err := row.Scan(&resultID, &incidentID, &diagnosticRunID, &model, &status,
		&likelyRootCause, &confidence, &payload, &createdAt, &userRating); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "analysis result not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var payloadObj any
	_ = json.Unmarshal([]byte(payload), &payloadObj)

	writeJSON(w, http.StatusOK, map[string]any{
		"id":              resultID,
		"incidentId":      incidentID.String,
		"diagnosticRunId": diagnosticRunID,
		"model":           model,
		"status":          status,
		"likelyRootCause": likelyRootCause.String,
		"confidence":      confidence.Float64,
		"payload":         payloadObj,
		"userRating":      userRating,
		"createdAt":       createdAt,
	})
}

// GetEvidence handles GET /api/v1/incidents/{id}/evidence
func (h *Analysis) GetEvidence(w http.ResponseWriter, r *http.Request) {
	_, name := namespacedName(chi.URLParam(r, "id"), h.DefaultNamespace)
	row := h.DB.QueryRowContext(r.Context(),
		`SELECT id, diagnostic_run_id, collected_at, collector_version, payload, created_at
		 FROM evidence WHERE incident_id = ? ORDER BY created_at DESC LIMIT 1`, name)

	var id, diagnosticRunID, collectedAt, collectorVersion, payload, createdAt string
	if err := row.Scan(&id, &diagnosticRunID, &collectedAt, &collectorVersion, &payload, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "no evidence found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var payloadObj any
	_ = json.Unmarshal([]byte(payload), &payloadObj)

	writeJSON(w, http.StatusOK, map[string]any{
		"id":               id,
		"diagnosticRunId":  diagnosticRunID,
		"collectedAt":      collectedAt,
		"collectorVersion": collectorVersion,
		"payload":          payloadObj,
		"createdAt":        createdAt,
	})
}

// RateAnalysisResult handles POST /api/v1/analysisresults/{id}/rate
// Body: {"rating": "up"|"down"}
func (h *Analysis) RateAnalysisResult(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var body struct {
		Rating string `json:"rating"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Rating != "up" && body.Rating != "down" {
		writeError(w, http.StatusBadRequest, "rating must be 'up' or 'down'")
		return
	}

	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE analysis_results SET user_rating = ? WHERE id = ?`,
		body.Rating, id,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		writeError(w, http.StatusNotFound, "analysis result not found")
		return
	}

	// Return updated record with rating included.
	row := h.DB.QueryRowContext(r.Context(),
		`SELECT id, incident_id, likely_root_cause, confidence, user_rating
		 FROM analysis_results WHERE id = ?`, id)
	var (
		resultID, userRating       string
		incidentID, likelyRootCause sql.NullString
		confidence                  sql.NullFloat64
	)
	_ = row.Scan(&resultID, &incidentID, &likelyRootCause, &confidence, &userRating)

	writeJSON(w, http.StatusOK, map[string]any{
		"id":              resultID,
		"incidentId":      incidentID.String,
		"likelyRootCause": likelyRootCause.String,
		"confidence":      confidence.Float64,
		"userRating":      userRating,
	})
}
