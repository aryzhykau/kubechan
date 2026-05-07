// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	k8s "github.com/org/kubechan/services/backend-api/k8s"
)

// evidenceRequest is the body accepted by POST /internal/evidence.
type evidenceRequest struct {
	DiagnosticRunID  string          `json:"diagnosticRunId"`
	ProblemCaseID    string          `json:"problemCaseId,omitempty"`
	IncidentID       string          `json:"incidentId,omitempty"`
	CollectedAt      time.Time       `json:"collectedAt"`
	CollectorVersion string          `json:"collectorVersion"`
	Payload          json.RawMessage `json:"payload"`
	PayloadBytes     int             `json:"payloadBytes"`
	LogTruncated     bool            `json:"logTruncated"`
	RedactionSummary *struct {
		PatternsApplied int      `json:"patternsApplied"`
		RedactedFields  []string `json:"redactedFields"`
	} `json:"redactionSummary,omitempty"`
	LogTruncationInfo *struct {
		Truncated      bool  `json:"truncated"`
		OriginalBytes  int64 `json:"originalBytes"`
		TruncatedBytes int64 `json:"truncatedBytes"`
	} `json:"logTruncationInfo,omitempty"`
	CollectionErrors []string `json:"collectionErrors,omitempty"`
}

// Internal holds dependencies for internal (service-to-service) handlers.
type Internal struct {
	K8s              client.Client
	DB               *sql.DB
	DefaultNamespace string
	LLMGatewayURL    string
	Hub              interface{ Broadcast(msg []byte) }
	MoodSyncer       *k8s.MoodSyncer
	Logger           *slog.Logger
}

// ReceiveEvidence handles POST /internal/evidence
func (h *Internal) ReceiveEvidence(w http.ResponseWriter, r *http.Request) {
	var req evidenceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.DiagnosticRunID == "" {
		writeError(w, http.StatusBadRequest, "diagnosticRunId is required")
		return
	}

	evidenceID := uuid.New().String()
	redactionJSON, _ := json.Marshal(req.RedactionSummary)
	truncationJSON, _ := json.Marshal(req.LogTruncationInfo)
	errorsJSON, _ := json.Marshal(req.CollectionErrors)
	logTruncated := 0
	if req.LogTruncated {
		logTruncated = 1
	}

	_, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO evidence
		 (id, diagnostic_run_id, problem_case_id, incident_id, collected_at, collector_version,
		  log_truncated, payload, payload_bytes, redaction_summary, log_truncation_info,
		  collection_errors, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		evidenceID,
		req.DiagnosticRunID,
		req.ProblemCaseID,
		nullStr(req.IncidentID),
		req.CollectedAt.UTC().Format(time.RFC3339),
		req.CollectorVersion,
		logTruncated,
		string(req.Payload),
		req.PayloadBytes,
		nullBytes(redactionJSON),
		nullBytes(truncationJSON),
		nullBytes(errorsJSON),
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("inserting evidence: %s", err))
		return
	}

	// Patch the DiagnosticRun CRD status.
	dr := &v1alpha1.DiagnosticRun{}
	if err := h.K8s.Get(r.Context(),
		client.ObjectKey{Namespace: h.DefaultNamespace, Name: req.DiagnosticRunID}, dr); err == nil {

		statusPatch := client.MergeFrom(dr.DeepCopy())
		now := metav1.Now()
		dr.Status.CollectedAt = &now
		dr.Status.CollectorVersion = req.CollectorVersion
		dr.Status.EvidenceRef = evidenceID
		dr.Status.State = v1alpha1.DiagnosticRunStateCompleted
		if len(req.CollectionErrors) > 0 {
			dr.Status.CollectionErrors = req.CollectionErrors
		}
		if req.RedactionSummary != nil {
			dr.Status.RedactionSummary = &v1alpha1.RedactionSummary{
				PatternsApplied: req.RedactionSummary.PatternsApplied,
				RedactedFields:  req.RedactionSummary.RedactedFields,
			}
		}
		if req.LogTruncationInfo != nil {
			dr.Status.LogTruncationInfo = &v1alpha1.LogTruncationInfo{
				Truncated:      req.LogTruncationInfo.Truncated,
				OriginalBytes:  req.LogTruncationInfo.OriginalBytes,
				TruncatedBytes: req.LogTruncationInfo.TruncatedBytes,
			}
		}
		_ = h.K8s.Status().Patch(r.Context(), dr, statusPatch)
	}

	writeJSON(w, http.StatusCreated, map[string]string{"evidenceId": evidenceID})

	// Dispatch analysis asynchronously so we don't block the response.
	if h.LLMGatewayURL != "" {
		go h.dispatchAnalysis(evidenceID, req)
	}
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullBytes(b []byte) any {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	return string(b)
}

// llmAnalyzeRequest is the body sent to llm-gateway POST /analyze.
type llmAnalyzeRequest struct {
	EvidenceID      string           `json:"evidenceId"`
	DiagnosticRunID string           `json:"diagnosticRunId"`
	IncidentID      string           `json:"incidentId,omitempty"`
	ReanalysisCount int              `json:"reanalysisCount"`
	MoodLevel       int              `json:"moodLevel"`
	PriorDiagnoses  []priorDiagnosis `json:"priorDiagnoses,omitempty"`
	UserMessage     string           `json:"userMessage,omitempty"`
	Provider        string           `json:"provider,omitempty"`
	Credentials     map[string]any   `json:"credentials,omitempty"`
	Payload         map[string]any   `json:"payload"`
}

// priorDiagnosis is a previous analysis attempt for the same incident, with its user rating.
type priorDiagnosis struct {
	Attempt         int    `json:"attempt"`
	LikelyRootCause string `json:"likelyRootCause"`
	UserRating      string `json:"userRating"` // "up", "down", or ""
}

// suggestedResource mirrors the LLM gateway's SuggestedResource model.
type suggestedResource struct {
	Kind   string `json:"kind"`
	Reason string `json:"reason"`
}

// llmAnalyzeResponse is the response from llm-gateway POST /analyze.
type llmAnalyzeResponse struct {
	EvidenceID         string              `json:"evidenceId"`
	Model              string              `json:"model"`
	OpeningRant        string              `json:"openingRant"`
	LikelyRootCause    string              `json:"likelyRootCause"`
	EvidenceChain      string              `json:"evidenceChain"`
	Recommendation     string              `json:"recommendation"`
	ClosingInsult      string              `json:"closingInsult"`
	Confidence         float64             `json:"confidence"`
	NeedsMoreInfo      bool                `json:"needsMoreInfo"`
	SuggestedResources []suggestedResource `json:"suggestedResources,omitempty"`
	ThinkingBudget     int                 `json:"thinkingBudgetUsed"`
	Prompt             string              `json:"prompt,omitempty"`
}

func (h *Internal) dispatchAnalysis(evidenceID string, req evidenceRequest) {
	logger := h.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// Decode raw payload into a map for forwarding.
	var payloadMap map[string]any
	if err := json.Unmarshal(req.Payload, &payloadMap); err != nil {
		logger.Error("dispatchAnalysis: unmarshal payload", "err", err)
		return
	}

	// Count how many prior completed analyses exist for this incident.
	var priorCount int
	var priorDiagnoses []priorDiagnosis
	if req.IncidentID != "" {
		_ = h.DB.QueryRow(
			`SELECT COUNT(*) FROM analysis_results WHERE incident_id = ?`,
			req.IncidentID,
		).Scan(&priorCount)

		rows, err := h.DB.Query(
			`SELECT likely_root_cause, COALESCE(user_rating, '') FROM analysis_results
			 WHERE incident_id = ? AND status = 'completed'
			 ORDER BY created_at ASC`,
			req.IncidentID,
		)
		if err == nil {
			defer func() { _ = rows.Close() }()
			attempt := 1
			for rows.Next() {
				var rootCause, rating string
				if err := rows.Scan(&rootCause, &rating); err == nil {
					priorDiagnoses = append(priorDiagnoses, priorDiagnosis{
						Attempt:         attempt,
						LikelyRootCause: rootCause,
						UserRating:      rating,
					})
					attempt++
				}
			}
		}
	}

	// Read current mood from the KubeChanState singleton (fast cache read).
	moodLevel := 0
	if h.MoodSyncer != nil {
		moodLevel = h.MoodSyncer.GetMoodLevel(context.Background())
	}

	// Extract userMessage from the payload (set for manual incidents).
	userMessage, _ := payloadMap["userMessage"].(string)

	// Look up the triggering user's LLM provider + credentials.
	provider := ""
	var credentials map[string]any
	var triggeredBy sql.NullString
	_ = h.DB.QueryRow(
		`SELECT triggered_by FROM analysis_requests WHERE diagnostic_run_id = ? LIMIT 1`,
		req.DiagnosticRunID,
	).Scan(&triggeredBy)
	if triggeredBy.Valid && triggeredBy.String != "" {
		var credsJSON string
		err := h.DB.QueryRow(
			`SELECT provider, credentials FROM user_llm_settings WHERE user_id = ?`,
			triggeredBy.String,
		).Scan(&provider, &credsJSON)
		if err == nil {
			_ = json.Unmarshal([]byte(credsJSON), &credentials)
		}
	}

	body, err := json.Marshal(llmAnalyzeRequest{
		EvidenceID:      evidenceID,
		DiagnosticRunID: req.DiagnosticRunID,
		IncidentID:      req.IncidentID,
		ReanalysisCount: priorCount,
		MoodLevel:       moodLevel,
		PriorDiagnoses:  priorDiagnoses,
		UserMessage:     userMessage,
		Provider:        provider,
		Credentials:     credentials,
		Payload:         payloadMap,
	})
	if err != nil {
		logger.Error("dispatchAnalysis: marshal request", "err", err)
		return
	}

	httpClient := &http.Client{Timeout: 5 * time.Minute}
	resp, err := httpClient.Post(h.LLMGatewayURL+"/analyze", "application/json", bytes.NewReader(body))
	if err != nil {
		logger.Error("dispatchAnalysis: llm-gateway call failed", "err", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		logger.Error("dispatchAnalysis: llm-gateway non-200", "status", resp.StatusCode)
		return
	}

	var result llmAnalyzeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		logger.Error("dispatchAnalysis: decode response", "err", err)
		return
	}

	// Store in analysis_results.
	analysisID := uuid.New().String()
	resultPayload, _ := json.Marshal(result)
	needsMoreInfoInt := 0
	if result.NeedsMoreInfo {
		needsMoreInfoInt = 1
	}
	suggJSON, _ := json.Marshal(result.SuggestedResources)
	modelRuntime := provider
	if modelRuntime == "" {
		modelRuntime = "bedrock"
	}
	_, err = h.DB.Exec(
		`INSERT INTO analysis_results
		 (id, incident_id, diagnostic_run_id, model, model_runtime, status,
		  likely_root_cause, confidence, payload, prompt, needs_more_info, suggested_resources, created_at)
		 VALUES (?, ?, ?, ?, ?, 'completed', ?, ?, ?, ?, ?, ?, datetime('now'))`,
		analysisID,
		nullStr(req.IncidentID),
		req.DiagnosticRunID,
		result.Model,
		modelRuntime,
		result.LikelyRootCause,
		result.Confidence,
		string(resultPayload),
		nullStr(result.Prompt),
		needsMoreInfoInt,
		nullBytes(suggJSON),
	)
	if err != nil {
		logger.Error("dispatchAnalysis: insert analysis_result", "err", err)
		return
	}

	logger.Info("analysis completed", "evidenceId", evidenceID, "analysisId", analysisID, "confidence", result.Confidence)

	// Broadcast WS event so the frontend can update.
	if h.Hub != nil {
		msg, _ := json.Marshal(map[string]any{
			"type":               "Analysis.Completed",
			"analysisId":         analysisID,
			"incidentId":         req.IncidentID,
			"rootCause":          result.LikelyRootCause,
			"confidence":         result.Confidence,
			"needsMoreInfo":      result.NeedsMoreInfo,
			"suggestedResources": result.SuggestedResources,
		})
		h.Hub.Broadcast(msg)
	}
}
