package handler

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
)

type agentProgress struct {
	Version            int                    `json:"version"`
	Type               string                 `json:"type"`
	RunID              string                 `json:"runId"`
	DiagnosticRunID    string                 `json:"diagnosticRunId"`
	AnalysisRequestID  string                 `json:"analysisRequestId"`
	AttemptID          string                 `json:"attemptId"`
	Seq                int                    `json:"seq"`
	ProducerSeq        int                    `json:"producerSeq,omitempty"`
	Stage              string                 `json:"stage"`
	Phase              string                 `json:"phase"`
	OccurredAt         string                 `json:"occurredAt"`
	RecordedAt         string                 `json:"recordedAt"`
	BranchID           string                 `json:"branchId,omitempty"`
	ToolCallID         string                 `json:"toolCallId,omitempty"`
	ReturnedCount      int                    `json:"returnedCount,omitempty"`
	Turn               int                    `json:"turn,omitempty"`
	SnapshotHash       string                 `json:"snapshotHash,omitempty"`
	ReasonCode         string                 `json:"reasonCode,omitempty"`
	Target             *agentProgressTarget   `json:"target,omitempty"`
	PreviousLogs       bool                   `json:"previousLogs,omitempty"`
	Availability       string                 `json:"availability,omitempty"`
	ValidationDetail   string                 `json:"validationDetail,omitempty"`
	ValidationFields   []agentValidationField `json:"validationFields,omitempty"`
	ExecutionLimit     *agentExecutionLimit   `json:"executionLimit,omitempty"`
	Exposed            *agentExposure         `json:"exposed,omitempty"`
	InvestigationGraph *agentExposure         `json:"investigationGraph,omitempty"`
	TurnTokens         *agentTurnTokens       `json:"turnTokens,omitempty"`
	Draft              *agentDraft            `json:"draft,omitempty"`
	Change             *agentDraftChange      `json:"change,omitempty"`
}

type agentTurnTokens struct {
	Input  int `json:"input"`
	Output int `json:"output"`
}

type agentProgressTarget struct {
	NodeID   string               `json:"nodeId"`
	Resource v1alpha1.ResourceRef `json:"resource"`
}

type agentValidationField struct {
	Path string `json:"path"`
	Kind string `json:"kind"`
}

type agentExecutionLimit struct {
	Kind    string `json:"kind"`
	Used    int64  `json:"used"`
	Maximum int64  `json:"maximum"`
}

var agentFieldPath = regexp.MustCompile(`^(\$|[a-zA-Z?][a-zA-Z0-9_?]*(\.(?:[a-zA-Z?][a-zA-Z0-9_?]*|[0-9]+))*)$`)
var agentErrorCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// Keep these transport bounds aligned with llm-gateway's AgentBudget. Allow
// progress for every read, model request and bounded output/protocol repair.
const (
	agentMaxModelRequests  = 100
	agentMaxProgressEvents = 512
	agentRunTimeout        = 610 * time.Second
)

var agentStages = map[string]bool{
	"indexing": true, "model_request": true, "validating": true, "finalizing": true, "execution_limit": true,
	"get_graph_neighbors": true, "get_resource": true, "get_events": true,
	"get_logs": true, "read_sources": true,
	"context_delivered": true, "draft_updated": true, "draft_write": true, "memory_updated": true,
}

func coarseAgentState(status string) v1alpha1.DiagnosticRunAnalysisState {
	switch status {
	case "running":
		return v1alpha1.DiagnosticRunAnalysisStateInProgress
	case "completed", "partial":
		return v1alpha1.DiagnosticRunAnalysisStateCompleted
	default:
		return v1alpha1.DiagnosticRunAnalysisStateFailed
	}
}

func (h *Internal) broadcastAgent(event agentProgress) {
	if h.Hub != nil {
		body, _ := json.Marshal(event)
		h.Hub.Broadcast(body)
	}
}

// appendAgentEvent is called only in a transaction that owns the attempt update.
func appendAgentEvent(ctx context.Context, tx *sql.Tx, event agentProgress, eventID string) (agentProgress, error) {
	event.Version, event.Type = 1, "Analysis.Progress"
	event.DiagnosticRunID = event.RunID
	event.RecordedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if event.OccurredAt == "" {
		event.OccurredAt = event.RecordedAt
	}
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq), 0) + 1 FROM analysis_progress WHERE attempt_id = ?`, event.AttemptID).Scan(&event.Seq); err != nil {
		return event, err
	}
	body, err := json.Marshal(event)
	if err != nil {
		return event, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO analysis_progress(attempt_id, seq, event_id, payload) VALUES (?, ?, ?, ?)`, event.AttemptID, event.Seq, eventID, string(body))
	return event, err
}

func (h *Internal) persistAgentProgress(ctx context.Context, event agentProgress) error {
	if body, err := json.Marshal(event); err != nil || len(body) > 96_000 {
		return errors.New("agent event payload exceeds budget")
	}
	if !agentStages[event.Stage] || (event.Phase != "started" && event.Phase != "completed" && event.Phase != "failed") || event.Version != 1 || len(event.ToolCallID) > 128 || event.ReturnedCount < 0 || event.ReturnedCount > 25 || event.Turn < 0 || event.Turn > agentMaxModelRequests {
		return errors.New("invalid agent progress event")
	}
	if len(event.ValidationFields) > 8 || (event.Availability != "" && event.Availability != "collected" && event.Availability != "not_collected") || (event.ReasonCode != "" && !agentErrorCode.MatchString(event.ReasonCode)) {
		return errors.New("invalid agent progress details")
	}
	if event.ValidationDetail != "" && (event.Stage != "validating" || event.Phase != "failed" || event.ReasonCode != "draft_change_deferred" || len(event.ValidationDetail) > 240) {
		return errors.New("invalid agent validation detail")
	}
	for _, field := range event.ValidationFields {
		if len(field.Path) > 160 || !agentFieldPath.MatchString(field.Path) || !agentErrorCode.MatchString(field.Kind) {
			return errors.New("invalid agent validation field")
		}
	}
	if limit := event.ExecutionLimit; limit != nil {
		valid := map[string]bool{"requests": true, "input_tokens": true, "output_tokens": true, "tool_calls": true}
		if event.Stage != "execution_limit" || !valid[limit.Kind] || limit.Used < 0 || limit.Maximum <= 0 || limit.Used > 1_000_000_000 || limit.Maximum > 1_000_000_000 {
			return errors.New("invalid agent execution limit")
		}
	}
	if event.Target != nil {
		body, _ := json.Marshal(event.Target)
		if len(event.Target.NodeID) > 512 || len(body) > 1200 {
			return errors.New("invalid agent progress target")
		}
	}
	if event.ProducerSeq < 1 || event.ProducerSeq > agentMaxProgressEvents {
		return errors.New("agent progress budget exceeded")
	}
	if event.SnapshotHash != "" {
		if _, err := hex.DecodeString(event.SnapshotHash); err != nil || len(event.SnapshotHash) != 64 {
			return errors.New("invalid snapshot hash")
		}
	}
	if _, err := time.Parse(time.RFC3339Nano, event.OccurredAt); err != nil {
		return errors.New("invalid progress timestamp")
	}
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, `UPDATE analysis_attempts SET stage = ?, updated_at = ?, snapshot_hash = CASE WHEN snapshot_hash = '' THEN ? ELSE snapshot_hash END WHERE id = ? AND analysis_request_id = ? AND diagnostic_run_id = ? AND status = 'running' AND (? = '' OR snapshot_hash = '' OR snapshot_hash = ?)`, event.Stage, time.Now().UTC().Format(time.RFC3339Nano), event.SnapshotHash, event.AttemptID, event.AnalysisRequestID, event.RunID, event.SnapshotHash, event.SnapshotHash)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("agent attempt is no longer active")
	}
	event, err = appendAgentEvent(ctx, tx, event, fmt.Sprintf("gateway-%d", event.ProducerSeq))
	if err != nil {
		return err
	}
	if err = tx.Commit(); err == nil {
		h.broadcastAgent(event)
	}
	return err
}

func (h *Internal) dispatchSnapshotAgent(req llmAnalyzeRequest) {
	ctx, cancel := context.WithTimeout(context.Background(), agentRunTimeout)
	defer cancel()
	req.AttemptID, req.AnalysisMode = uuid.NewString(), "snapshot-agent-v1"
	if err := h.DB.QueryRowContext(ctx, `SELECT id FROM analysis_requests WHERE diagnostic_run_id = ? AND status = 'in_progress' ORDER BY requested_at DESC LIMIT 1`, req.DiagnosticRunID).Scan(&req.AnalysisRequestID); err != nil {
		h.patchAnalysisState(req.DiagnosticRunID, v1alpha1.DiagnosticRunAnalysisStateFailed, "")
		return
	}
	h.agentMu.Lock()
	if h.agentCancels == nil {
		h.agentCancels = make(map[string]context.CancelFunc)
	}
	h.agentCancels[req.AttemptID] = cancel
	h.agentMu.Unlock()
	defer func() { h.agentMu.Lock(); delete(h.agentCancels, req.AttemptID); h.agentMu.Unlock() }()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := h.DB.ExecContext(ctx, `INSERT INTO analysis_attempts(id, analysis_request_id, diagnostic_run_id, evidence_id, status, stage, created_at, updated_at) SELECT ?, ?, ?, ?, 'running', 'queued', ?, ? WHERE NOT EXISTS (SELECT 1 FROM analysis_results WHERE diagnostic_run_id = ?)`, req.AttemptID, req.AnalysisRequestID, req.DiagnosticRunID, req.EvidenceID, now, now, req.DiagnosticRunID)
	if err != nil {
		var active int
		if h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM analysis_attempts WHERE analysis_request_id = ? AND status = 'running'`, req.AnalysisRequestID).Scan(&active) == nil && active > 0 {
			return
		}
		h.patchAnalysisState(req.DiagnosticRunID, v1alpha1.DiagnosticRunAnalysisStateFailed, "")
		return
	} // An active duplicate must never dispatch a second paid run.
	var exists int
	if err = h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM analysis_attempts WHERE id = ?`, req.AttemptID).Scan(&exists); err != nil || exists != 1 {
		return
	}
	result, err := h.callSnapshotAgent(ctx, req)
	if err != nil {
		state, reason := "failed", "gateway_error"
		var failure *agentFailure
		if errors.As(err, &failure) {
			reason = failure.code
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || reason == "timeout" {
			state, reason = "timed_out", "timeout"
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			state, reason = "cancelled", "cancelled"
		}
		if state != "cancelled" {
			slog.Warn("snapshot agent stream stopped", "diagnosticRunId", req.DiagnosticRunID, "attemptId", req.AttemptID, "reason", reason, "error", err)
		}
		h.recordAgentFailure(req.AttemptID, state, reason)
		return
	}
	if err = h.completeAgentAttempt(req, result); err != nil {
		slog.Warn("snapshot agent result rejected", "diagnosticRunId", req.DiagnosticRunID, "attemptId", req.AttemptID, "error", err)
		h.recordAgentFailure(req.AttemptID, "failed", "result_storage_failed")
	}
}

type agentFailure struct{ code string }

func (e *agentFailure) Error() string { return e.code }

func (h *Internal) callSnapshotAgent(ctx context.Context, req llmAnalyzeRequest) (llmAnalyzeResponse, error) {
	var result llmAnalyzeResponse
	drafts := newAgentDraftValidator(req.Payload)
	// Only canonical snapshot metadata can become a display target. Never persist
	// labels supplied by a model or a gateway response as resource identity.
	var graph struct {
		Nodes []struct {
			ID       string               `json:"id"`
			Resource v1alpha1.ResourceRef `json:"resource"`
		} `json:"nodes"`
	}
	graphJSON, _ := json.Marshal(req.Payload["graph"])
	_ = json.Unmarshal(graphJSON, &graph)
	targets := make(map[string]v1alpha1.ResourceRef, len(graph.Nodes))
	for _, node := range graph.Nodes {
		targets[node.ID] = node.Resource
	}
	body, err := json.Marshal(req)
	if err != nil {
		return result, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, h.LLMGatewayURL+"/analyze/stream", bytes.NewReader(body))
	if err != nil {
		return result, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	response, err := (&http.Client{Timeout: agentRunTimeout + 5*time.Second}).Do(httpReq)
	if err != nil {
		return result, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return result, errors.New("gateway rejected agent request")
	}
	scanner := bufio.NewScanner(io.LimitReader(response.Body, 8<<20))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	producerSeq, gotResult := 0, false
	for scanner.Scan() {
		var frame struct {
			Type   string             `json:"type"`
			Event  agentProgress      `json:"event"`
			Result llmAnalyzeResponse `json:"result"`
			Code   string             `json:"code"`
		}
		if err = json.Unmarshal(scanner.Bytes(), &frame); err != nil {
			return result, err
		}
		switch frame.Type {
		case "progress":
			producerSeq++
			if gotResult || frame.Event.AttemptID != req.AttemptID || frame.Event.AnalysisRequestID != req.AnalysisRequestID || frame.Event.RunID != req.DiagnosticRunID || frame.Event.ProducerSeq != producerSeq {
				return result, errors.New("agent stream identity or sequence mismatch")
			}
			if frame.Event.Target != nil {
				resource, ok := targets[frame.Event.Target.NodeID]
				if !ok {
					return result, errors.New("unknown agent progress target")
				}
				frame.Event.Target.Resource = resource
			}
			if err = drafts.validate(frame.Event); err != nil {
				return result, err
			}
			if err = h.persistAgentProgress(ctx, frame.Event); err != nil {
				return result, fmt.Errorf("persisting agent progress frame %d: %w", producerSeq, err)
			}
		case "result":
			if gotResult {
				return result, errors.New("duplicate agent result")
			}
			result, gotResult = frame.Result, true
		case "error":
			allowed := map[string]bool{"timeout": true, "budget_exhausted": true, "validation_failed": true, "unsupported_request": true, "provider_error": true}
			if !allowed[frame.Code] {
				frame.Code = "gateway_error"
			}
			return result, &agentFailure{frame.Code}
		default:
			return result, errors.New("unexpected agent stream frame")
		}
	}
	if err = scanner.Err(); err != nil {
		return result, err
	}
	if !gotResult {
		return result, errors.New("agent stream ended without a validated result")
	}
	return result, nil
}

func (h *Internal) completeAgentAttempt(req llmAnalyzeRequest, result llmAnalyzeResponse) error {
	var execution struct {
		AttemptID    string `json:"attemptId"`
		RequestID    string `json:"analysisRequestId"`
		SnapshotHash string `json:"snapshotHash"`
		Status       string `json:"status"`
	}
	if json.Unmarshal(result.AgentExecution, &execution) != nil || execution.AttemptID != req.AttemptID || execution.RequestID != req.AnalysisRequestID || len(execution.SnapshotHash) != 64 || (execution.Status != "completed" && execution.Status != "partial") || result.EvidenceID != req.EvidenceID || result.AnalysisMode != "snapshot-agent-v1" || result.AssessmentVersion != 2 || len(result.Assessment) == 0 || string(result.Assessment) == "null" {
		return errors.New("invalid agent result identity or assessment contract")
	}
	ctx := context.Background()
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now, analysisID := time.Now().UTC().Format(time.RFC3339Nano), uuid.NewString()
	updated, err := tx.ExecContext(ctx, `UPDATE analysis_attempts SET status = ?, stage = ?, updated_at = ? WHERE id = ? AND status = 'running' AND snapshot_hash = ?`, execution.Status, execution.Status, now, req.AttemptID, execution.SnapshotHash)
	if err != nil {
		return err
	}
	if n, _ := updated.RowsAffected(); n != 1 {
		return errors.New("agent result arrived after settlement or for another snapshot")
	}
	payload, _ := json.Marshal(result)
	suggested, _ := json.Marshal(result.SuggestedResources)
	modelRuntime := req.Provider
	if modelRuntime == "" {
		modelRuntime = "bedrock"
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO analysis_results(id, incident_id, diagnostic_run_id, model, model_runtime, status, likely_root_cause, confidence, payload, prompt, needs_more_info, suggested_resources, created_at) VALUES (?, ?, ?, ?, ?, 'completed', ?, ?, ?, ?, ?, ?, ?)`, analysisID, nullStr(req.IncidentID), req.DiagnosticRunID, result.Model, modelRuntime, result.LikelyRootCause, result.Confidence, string(payload), nullStr(result.Prompt), result.NeedsMoreInfo, string(suggested), now)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE analysis_requests SET status = 'completed', completed_at = ? WHERE id = ?`, now, req.AnalysisRequestID); err != nil {
		return err
	}
	event, err := appendAgentEvent(ctx, tx, agentProgress{RunID: req.DiagnosticRunID, AnalysisRequestID: req.AnalysisRequestID, AttemptID: req.AttemptID, Stage: execution.Status, Phase: "completed"}, "terminal")
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	h.patchAnalysisState(req.DiagnosticRunID, v1alpha1.DiagnosticRunAnalysisStateCompleted, analysisID)
	h.broadcastAgent(event)
	if h.Hub != nil {
		body, _ := json.Marshal(map[string]any{"type": "Analysis.Completed", "diagnosticRunId": req.DiagnosticRunID, "analysisId": analysisID, "incidentId": req.IncidentID, "needsMoreInfo": result.NeedsMoreInfo, "suggestedResources": result.SuggestedResources})
		h.Hub.Broadcast(body)
	}
	return nil
}

func (h *Internal) recordAgentFailure(attemptID, state, reason string) {
	if err := h.failAgentAttempt(attemptID, state, reason); err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.Error("persisting snapshot agent terminal state", "attemptId", attemptID, "state", state, "reason", reason, "error", err)
	}
}

func (h *Internal) failAgentAttempt(attemptID, state, reason string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	event := agentProgress{AttemptID: attemptID, Stage: state, Phase: "failed", ReasonCode: reason}
	// Take the writer lock before reading. A read transaction can fail its
	// upgrade immediately if another writer committed since its snapshot.
	err = tx.QueryRowContext(ctx, `UPDATE analysis_attempts SET status = ?, stage = ?, reason_code = ?, updated_at = ?
		WHERE id = ? AND status = 'running' RETURNING diagnostic_run_id, analysis_request_id`,
		state, state, reason, now, attemptID).Scan(&event.RunID, &event.AnalysisRequestID)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE analysis_requests SET status = 'failed', completed_at = ? WHERE id = ?`, now, event.AnalysisRequestID); err != nil {
		return err
	}
	event, err = appendAgentEvent(ctx, tx, event, "terminal")
	if err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	h.patchAnalysisState(event.RunID, v1alpha1.DiagnosticRunAnalysisStateFailed, "")
	h.broadcastAgent(event)
	return nil
}

// GetAnalysisProgress returns persisted state, including after reconnect/reload.
func (h *DiagnosticRuns) GetAnalysisProgress(w http.ResponseWriter, r *http.Request) {
	_, runID := namespacedName(chi.URLParam(r, "id"), h.DefaultNamespace)
	tx, err := h.DB.BeginTx(r.Context(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read analysis progress")
		return
	}
	defer func() { _ = tx.Rollback() }()
	var attemptID, requestID, status, stage, reason, updatedAt string
	err = tx.QueryRowContext(r.Context(), `SELECT id, analysis_request_id, status, stage, reason_code, updated_at FROM analysis_attempts WHERE diagnostic_run_id = ? ORDER BY created_at DESC LIMIT 1`, runID).Scan(&attemptID, &requestID, &status, &stage, &reason, &updatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"runId": runID, "available": false, "events": []any{}})
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read analysis progress")
		return
	}
	after, err := strconv.Atoi(r.URL.Query().Get("afterSeq"))
	if r.URL.Query().Get("afterSeq") != "" && (err != nil || after < 0) {
		writeError(w, http.StatusBadRequest, "invalid afterSeq")
		return
	}
	if r.URL.Query().Get("attemptId") != attemptID {
		after = 0
	}
	rows, err := tx.QueryContext(r.Context(), `SELECT payload FROM analysis_progress WHERE attempt_id = ? AND seq > ? ORDER BY seq`, attemptID, after)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "cannot read analysis events")
		return
	}
	defer func() { _ = rows.Close() }()
	events := []json.RawMessage{}
	for rows.Next() {
		var body string
		if rows.Scan(&body) != nil {
			_ = rows.Close()
			writeError(w, http.StatusInternalServerError, "cannot read analysis event")
			return
		}
		events = append(events, json.RawMessage(body))
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "cannot read analysis events")
		return
	}
	_ = rows.Close()
	var lastSeq int
	if tx.QueryRowContext(r.Context(), `SELECT COALESCE(MAX(seq), 0) FROM analysis_progress WHERE attempt_id = ?`, attemptID).Scan(&lastSeq) != nil || tx.Commit() != nil {
		writeError(w, http.StatusInternalServerError, "cannot read analysis cursor")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"available": true, "runId": runID, "analysisRequestId": requestID, "attemptId": attemptID, "status": status, "stage": stage, "reasonCode": reason, "updatedAt": updatedAt, "events": events, "lastSeq": lastSeq})
}

func (h *Internal) CancelAnalysis(w http.ResponseWriter, r *http.Request) {
	_, runID := namespacedName(chi.URLParam(r, "id"), h.DefaultNamespace)
	var attemptID string
	if h.DB.QueryRowContext(r.Context(), `SELECT id FROM analysis_attempts WHERE diagnostic_run_id = ? AND status = 'running'`, runID).Scan(&attemptID) != nil {
		writeError(w, http.StatusConflict, "no running snapshot agent")
		return
	}
	if err := h.failAgentAttempt(attemptID, "cancelled", "cancelled"); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusConflict, "analysis already settled")
		} else {
			slog.Error("persisting snapshot agent cancellation", "attemptId", attemptID, "error", err)
			writeError(w, http.StatusServiceUnavailable, "cannot persist analysis cancellation")
		}
		return
	}
	h.agentMu.Lock()
	if cancel := h.agentCancels[attemptID]; cancel != nil {
		cancel()
	}
	h.agentMu.Unlock()
	writeJSON(w, http.StatusAccepted, map[string]string{"attemptId": attemptID, "status": "cancelled"})
}
