// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
)

// DiagnosticRuns holds dependencies for DiagnosticRun handlers.
type DiagnosticRuns struct {
	K8s              client.Client
	DB               *sql.DB
	DefaultNamespace string
}

// Get handles GET /api/v1/diagnosticruns/{id}
func (h *DiagnosticRuns) Get(w http.ResponseWriter, r *http.Request) {
	ns, name := namespacedName(chi.URLParam(r, "id"), h.DefaultNamespace)
	dr := &v1alpha1.DiagnosticRun{}
	if err := h.K8s.Get(r.Context(), client.ObjectKey{Namespace: ns, Name: name}, dr); err != nil {
		if apierrors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, "diagnosticrun not found")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Resolve triggered_by attribution from analysis_requests.
	type triggeredByInfo struct {
		UserID   string `json:"userId"`
		Username string `json:"username"`
	}
	var attribution *triggeredByInfo
	var triggeredByUserID, triggeredByUsername sql.NullString
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT ar.triggered_by, u.username
		 FROM analysis_requests ar
		 LEFT JOIN users u ON u.id = ar.triggered_by
		 WHERE ar.diagnostic_run_id = ?
		 LIMIT 1`, name,
	).Scan(&triggeredByUserID, &triggeredByUsername)
	if triggeredByUserID.Valid && triggeredByUserID.String != "" {
		attribution = &triggeredByInfo{
			UserID:   triggeredByUserID.String,
			Username: triggeredByUsername.String,
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"diagnosticRun": dr,
		"triggeredBy":   attribution,
	})
}

// runSummary is returned by List.
type runSummary struct {
	DiagnosticRunID    string              `json:"diagnosticRunId"`
	IncidentID         string              `json:"incidentId"`
	RequestedAt        string              `json:"requestedAt"`
	Status             string              `json:"status"`
	AnalysisResultID   *string             `json:"analysisResultId"`
	LikelyRootCause    *string             `json:"likelyRootCause"`
	Confidence         *float64            `json:"confidence"`
	Model              *string             `json:"model"`
	AnalysisCreatedAt  *string             `json:"analysisCreatedAt"`
	NeedsMoreInfo      *bool               `json:"needsMoreInfo,omitempty"`
	SuggestedResources json.RawMessage     `json:"suggestedResources,omitempty"`
}

// List handles GET /api/v1/diagnosticruns?incidentId=
func (h *DiagnosticRuns) List(w http.ResponseWriter, r *http.Request) {
	incidentID := r.URL.Query().Get("incidentId")

	const baseQuery = `
		SELECT ar.diagnostic_run_id, ar.incident_id, ar.requested_at, ar.status,
		       res.id, res.likely_root_cause, res.confidence, res.model, res.created_at,
		       res.needs_more_info, res.suggested_resources
		FROM analysis_requests ar
		LEFT JOIN analysis_results res ON res.id = (
		    SELECT id FROM analysis_results WHERE diagnostic_run_id = ar.diagnostic_run_id
		    ORDER BY created_at DESC LIMIT 1
		)`

	var (
		rows *sql.Rows
		err  error
	)
	if incidentID != "" {
		rows, err = h.DB.QueryContext(r.Context(),
			baseQuery+` WHERE ar.incident_id = ? ORDER BY ar.requested_at DESC LIMIT 100`, incidentID)
	} else {
		rows, err = h.DB.QueryContext(r.Context(),
			baseQuery+` ORDER BY ar.requested_at DESC LIMIT 100`)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	summaries := []runSummary{}
	for rows.Next() {
		var (
			s                                                   runSummary
			incidentNullStr, requestedAt, status               sql.NullString
			analysisID, rootCause, model, analysisCreatedAt    sql.NullString
			suggestedResources                                  sql.NullString
			confidence                                          sql.NullFloat64
			needsMoreInfoInt                                    sql.NullInt64
		)
		if err := rows.Scan(
			&s.DiagnosticRunID, &incidentNullStr, &requestedAt, &status,
			&analysisID, &rootCause, &confidence, &model, &analysisCreatedAt,
			&needsMoreInfoInt, &suggestedResources,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.IncidentID = incidentNullStr.String
		s.RequestedAt = requestedAt.String
		s.Status = status.String
		if analysisID.Valid {
			s.AnalysisResultID = &analysisID.String
		}
		if rootCause.Valid {
			s.LikelyRootCause = &rootCause.String
		}
		if confidence.Valid {
			s.Confidence = &confidence.Float64
		}
		if model.Valid {
			s.Model = &model.String
		}
		if analysisCreatedAt.Valid {
			s.AnalysisCreatedAt = &analysisCreatedAt.String
		}
		if needsMoreInfoInt.Valid {
			v := needsMoreInfoInt.Int64 != 0
			s.NeedsMoreInfo = &v
		}
		if suggestedResources.Valid && suggestedResources.String != "" {
			s.SuggestedResources = json.RawMessage(suggestedResources.String)
		}
		summaries = append(summaries, s)
	}

	writeJSON(w, http.StatusOK, summaries)
}

// GetEvidence handles GET /api/v1/diagnosticruns/{id}/evidence
func (h *DiagnosticRuns) GetEvidence(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	row := h.DB.QueryRowContext(r.Context(),
		`SELECT id, diagnostic_run_id, incident_id, collected_at, collector_version,
		        payload, collection_errors, created_at
		 FROM evidence WHERE diagnostic_run_id = ? ORDER BY created_at DESC LIMIT 1`, id)

	var (
		evidenceID, diagnosticRunID, collectedAt, collectorVersion, payload, createdAt string
		incidentID, collectionErrors                                                    sql.NullString
	)
	if err := row.Scan(&evidenceID, &diagnosticRunID, &incidentID, &collectedAt,
		&collectorVersion, &payload, &collectionErrors, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "no evidence found for this diagnostic run")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var payloadObj any
	_ = json.Unmarshal([]byte(payload), &payloadObj)

	writeJSON(w, http.StatusOK, map[string]any{
		"id":               evidenceID,
		"diagnosticRunId":  diagnosticRunID,
		"incidentId":       incidentID.String,
		"collectedAt":      collectedAt,
		"collectorVersion": collectorVersion,
		"payload":          payloadObj,
		"createdAt":        createdAt,
	})
}

// GetAnalysisResult handles GET /api/v1/diagnosticruns/{id}/analysisresult
func (h *DiagnosticRuns) GetAnalysisResult(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	row := h.DB.QueryRowContext(r.Context(),
		`SELECT id, incident_id, diagnostic_run_id, model, status, likely_root_cause,
		        confidence, payload, created_at, COALESCE(user_rating, '') as user_rating
		 FROM analysis_results WHERE diagnostic_run_id = ? ORDER BY created_at DESC LIMIT 1`, id)

	var (
		resultID, diagnosticRunID, model, status, payload, createdAt, userRating string
		incidentID, likelyRootCause                                               sql.NullString
		confidence                                                                 sql.NullFloat64
	)
	if err := row.Scan(&resultID, &incidentID, &diagnosticRunID, &model, &status,
		&likelyRootCause, &confidence, &payload, &createdAt, &userRating); err != nil {
		if err == sql.ErrNoRows {
			writeError(w, http.StatusNotFound, "no analysis result found for this diagnostic run")
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

// deleteRunFromDB removes all DB records for a single diagnostic run ID.
func (h *DiagnosticRuns) deleteRunFromDB(tx *sql.Tx, runID string) error {
	for _, stmt := range []string{
		`DELETE FROM analysis_results  WHERE diagnostic_run_id = ?`,
		`DELETE FROM analysis_requests WHERE diagnostic_run_id = ?`,
		`DELETE FROM evidence          WHERE diagnostic_run_id = ?`,
	} {
		if _, err := tx.Exec(stmt, runID); err != nil {
			return err
		}
	}
	return nil
}

// Delete handles DELETE /api/v1/diagnosticruns/{id}
func (h *DiagnosticRuns) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback() //nolint:errcheck

	if err := h.deleteRunFromDB(tx, id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// bulkDeleteRequest is the body for DELETE /api/v1/diagnosticruns.
type bulkDeleteRequest struct {
	IDs []string `json:"ids"`
}

// BulkDelete handles DELETE /api/v1/diagnosticruns  (body: {"ids": [...]})
func (h *DiagnosticRuns) BulkDelete(w http.ResponseWriter, r *http.Request) {
	var req bulkDeleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "body must be {\"ids\":[\"...\"]}")
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer tx.Rollback() //nolint:errcheck

	for _, id := range req.IDs {
		if err := h.deleteRunFromDB(tx, id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"deleted": len(req.IDs)})
}
