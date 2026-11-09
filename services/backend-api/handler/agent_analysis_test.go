package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type agentTestHub struct {
	mu     sync.Mutex
	events [][]byte
}

func (h *agentTestHub) Broadcast(body []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.events = append(h.events, append([]byte{}, body...))
}

func setupAgent(t *testing.T) (*Internal, llmAnalyzeRequest) {
	t.Helper()
	database := openDB(t)
	if _, err := database.Exec(`INSERT INTO analysis_requests(id, diagnostic_run_id, status) VALUES ('request-agent', 'run-agent', 'in_progress')`); err != nil {
		t.Fatal(err)
	}
	k8s := fake.NewClientBuilder().WithScheme(newTestScheme()).WithObjects(&v1alpha1.DiagnosticRun{ObjectMeta: metav1.ObjectMeta{Name: "run-agent", Namespace: "kubechan"}, Status: v1alpha1.DiagnosticRunStatus{AnalysisState: v1alpha1.DiagnosticRunAnalysisStateInProgress}}).WithStatusSubresource(&v1alpha1.DiagnosticRun{}).Build()
	return &Internal{DB: database, K8s: k8s, DefaultNamespace: "kubechan", AnalysisMode: "snapshot-agent-v1", Hub: &agentTestHub{}}, llmAnalyzeRequest{EvidenceID: "evidence-agent", DiagnosticRunID: "run-agent", Provider: "bedrock", Payload: map[string]any{}}
}

func writeAgentProgress(t *testing.T, encoder *json.Encoder, req llmAnalyzeRequest, seq int, stage string) {
	t.Helper()
	event := agentProgress{Version: 1, RunID: req.DiagnosticRunID, AnalysisRequestID: req.AnalysisRequestID, AttemptID: req.AttemptID, ProducerSeq: seq, Stage: stage, Phase: "completed", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), SnapshotHash: strings.Repeat("a", 64)}
	if err := encoder.Encode(map[string]any{"type": "progress", "event": event}); err != nil {
		t.Error(err)
	}
}

func testAgentResult(req llmAnalyzeRequest) llmAnalyzeResponse {
	execution, _ := json.Marshal(map[string]any{"version": 1, "attemptId": req.AttemptID, "analysisRequestId": req.AnalysisRequestID, "snapshotHash": strings.Repeat("a", 64), "status": "completed", "contextJournal": []any{}, "exposed": map[string]any{"nodeIds": []string{"entry"}}})
	return llmAnalyzeResponse{EvidenceID: req.EvidenceID, Model: "test-bedrock", LikelyRootCause: "Evidence is inconclusive", Confidence: 0.5, AnalysisMode: "snapshot-agent-v1", AssessmentVersion: 2, Assessment: json.RawMessage(`{"version":2,"summary":"Evidence is inconclusive","confidence":0.5,"claims":[]}`), AgentExecution: execution}
}

func TestSnapshotAgentPersistsProgressAndValidatedResult(t *testing.T) {
	h, req := setupAgent(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/analyze/stream" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var received llmAnalyzeRequest
		if json.NewDecoder(r.Body).Decode(&received) != nil {
			t.Error("missing agent request")
			return
		}
		encoder := json.NewEncoder(w)
		writeAgentProgress(t, encoder, received, 1, "indexing")
		writeAgentProgress(t, encoder, received, 2, "get_graph_neighbors")
		_ = encoder.Encode(map[string]any{"type": "result", "result": testAgentResult(received)})
	}))
	defer server.Close()
	h.LLMGatewayURL = server.URL
	h.dispatchSnapshotAgent(req)
	var status, payload string
	if err := h.DB.QueryRow(`SELECT status, payload FROM analysis_results WHERE diagnostic_run_id = 'run-agent'`).Scan(&status, &payload); err != nil {
		t.Fatal(err)
	}
	if status != "completed" || !strings.Contains(payload, "contextJournal") {
		t.Fatalf("stored result = %s %s", status, payload)
	}
	w := httptest.NewRecorder()
	(&DiagnosticRuns{DB: h.DB, DefaultNamespace: "kubechan"}).GetAnalysisProgress(w, withChiParam(httptest.NewRequest("GET", "/", nil), "id", "run-agent"))
	var progress struct {
		Status    string
		LastSeq   int
		AttemptID string
		Events    []agentProgress
	}
	if err := json.Unmarshal(w.Body.Bytes(), &progress); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || progress.Status != "completed" || progress.LastSeq != 3 || len(progress.Events) != 3 {
		t.Fatalf("progress = %s", w.Body.String())
	}
	for i, event := range progress.Events {
		if event.Seq != i+1 || event.Type != "Analysis.Progress" || event.RecordedAt == "" {
			t.Fatalf("event = %#v", event)
		}
	}
	w = httptest.NewRecorder()
	(&DiagnosticRuns{DB: h.DB, DefaultNamespace: "kubechan"}).GetAnalysisProgress(w, withChiParam(httptest.NewRequest("GET", "/?afterSeq=2&attemptId="+progress.AttemptID, nil), "id", "run-agent"))
	if !strings.Contains(w.Body.String(), `"seq":3`) || strings.Contains(w.Body.String(), `"seq":2`) {
		t.Fatalf("cursor response = %s", w.Body.String())
	}
}

func TestSnapshotAgentRejectsWrongAttemptLegacyAndMissingResult(t *testing.T) {
	for _, scenario := range []string{"wrong-attempt", "legacy", "no-result", "wrong-snapshot", "wrong-sequence", "unknown-target"} {
		t.Run(scenario, func(t *testing.T) {
			h, req := setupAgent(t)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var received llmAnalyzeRequest
				_ = json.NewDecoder(r.Body).Decode(&received)
				encoder := json.NewEncoder(w)
				seq := 1
				if scenario == "wrong-sequence" {
					seq = 2
				}
				writeAgentProgress(t, encoder, received, seq, "indexing")
				if scenario == "unknown-target" {
					_ = encoder.Encode(map[string]any{"type": "progress", "event": agentProgress{Version: 1, RunID: received.DiagnosticRunID, AnalysisRequestID: received.AnalysisRequestID, AttemptID: received.AttemptID, ProducerSeq: 2, Stage: "get_resource", Phase: "started", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano), Target: &agentProgressTarget{NodeID: "invented"}}})
				}
				if scenario == "no-result" {
					return
				}
				result := testAgentResult(received)
				if scenario == "wrong-attempt" {
					result.AgentExecution = json.RawMessage(`{"attemptId":"other"}`)
				}
				if scenario == "legacy" {
					result.AssessmentVersion = 1
				}
				if scenario == "wrong-snapshot" {
					result.AgentExecution = json.RawMessage(strings.ReplaceAll(string(result.AgentExecution), strings.Repeat("a", 64), strings.Repeat("b", 64)))
				}
				_ = encoder.Encode(map[string]any{"type": "result", "result": result})
			}))
			defer server.Close()
			h.LLMGatewayURL = server.URL
			h.dispatchSnapshotAgent(req)
			var status string
			var count int
			_ = h.DB.QueryRow(`SELECT status FROM analysis_attempts`).Scan(&status)
			_ = h.DB.QueryRow(`SELECT COUNT(*) FROM analysis_results`).Scan(&count)
			if status != "failed" || count != 0 {
				t.Fatalf("status=%s, results=%d", status, count)
			}
		})
	}
}

func TestSnapshotAgentAccepts40ModelRequestsAnd50Reads(t *testing.T) {
	h, req := setupAgent(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var received llmAnalyzeRequest
		_ = json.NewDecoder(r.Body).Decode(&received)
		encoder, seq := json.NewEncoder(w), 0
		emit := func(event agentProgress) {
			seq++
			event.Version, event.RunID, event.AnalysisRequestID, event.AttemptID = 1, received.DiagnosticRunID, received.AnalysisRequestID, received.AttemptID
			event.ProducerSeq, event.OccurredAt, event.SnapshotHash = seq, time.Now().UTC().Format(time.RFC3339Nano), strings.Repeat("a", 64)
			if err := encoder.Encode(map[string]any{"type": "progress", "event": event}); err != nil {
				t.Error(err)
			}
		}
		emit(agentProgress{Stage: "indexing", Phase: "completed"})
		for turn := 1; turn <= 40; turn++ {
			emit(agentProgress{Stage: "model_request", Phase: "started", Turn: turn})
			emit(agentProgress{Stage: "context_delivered", Phase: "completed", Turn: turn, Exposed: &agentExposure{}, TurnTokens: &agentTurnTokens{Input: 8000, Output: 150}})
			if turn > 37 {
				continue
			}
			reads := 1
			if turn <= 13 {
				reads++
			}
			for read := 0; read < reads; read++ {
				emit(agentProgress{Stage: "get_resource", Phase: "started"})
				emit(agentProgress{Stage: "get_resource", Phase: "completed", ReturnedCount: 1})
			}
		}
		_ = encoder.Encode(map[string]any{"type": "result", "result": testAgentResult(received)})
	}))
	defer server.Close()
	h.LLMGatewayURL = server.URL
	h.dispatchSnapshotAgent(req)
	w := httptest.NewRecorder()
	(&DiagnosticRuns{DB: h.DB, DefaultNamespace: "kubechan"}).GetAnalysisProgress(w, withChiParam(httptest.NewRequest("GET", "/", nil), "id", "run-agent"))
	var progress struct {
		Status string
		Events []agentProgress
	}
	if err := json.Unmarshal(w.Body.Bytes(), &progress); err != nil {
		t.Fatal(err)
	}
	if progress.Status != "completed" || len(progress.Events) != 182 {
		t.Fatalf("progress = %s", w.Body.String())
	}
	turns, reads := 0, 0
	for _, event := range progress.Events {
		if event.Stage == "model_request" {
			turns++
			if event.Turn != turns {
				t.Fatalf("model turn = %d, want %d", event.Turn, turns)
			}
		}
		if event.Stage == "get_resource" && event.Phase == "completed" {
			reads++
		}
	}
	if turns != 40 || reads != 50 {
		t.Fatalf("model requests=%d, reads=%d", turns, reads)
	}
}

func TestAgentProgressRejectsRequestsAndEventsBeyondRaisedBounds(t *testing.T) {
	h, _ := setupAgent(t)
	for _, test := range []struct {
		event agentProgress
		want  string
	}{
		{agentProgress{Turn: 41, ProducerSeq: 1}, "invalid agent progress event"},
		{agentProgress{Turn: 40, ProducerSeq: agentMaxProgressEvents + 1}, "agent progress budget exceeded"},
	} {
		event := test.event
		event.Version, event.Stage, event.Phase = 1, "model_request", "started"
		event.OccurredAt = time.Now().UTC().Format(time.RFC3339Nano)
		if err := h.persistAgentProgress(context.Background(), event); err == nil || err.Error() != test.want {
			t.Fatalf("persist progress = %v, want %q", err, test.want)
		}
	}
}

func TestAgentActivityAndFailureDetailsSurviveRESTAndBroadcast(t *testing.T) {
	h, req := setupAgent(t)
	req.Payload = map[string]any{"graph": map[string]any{"nodes": []any{map[string]any{"id": "gateway", "resource": v1alpha1.ResourceRef{Kind: "Deployment", Name: "gateway", Namespace: "demo"}}}}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var received llmAnalyzeRequest
		_ = json.NewDecoder(r.Body).Decode(&received)
		encoder := json.NewEncoder(w)
		writeAgentProgress(t, encoder, received, 1, "indexing")
		for i, event := range []agentProgress{
			{Stage: "get_events", Phase: "completed", Target: &agentProgressTarget{NodeID: "gateway", Resource: v1alpha1.ResourceRef{Name: "UNTRUSTED-LABEL"}}, Availability: "not_collected"},
			{Stage: "validating", Phase: "failed", ReasonCode: "output_schema_invalid", ValidationFields: []agentValidationField{{Path: "nodeInsights.4.comment", Kind: "missing"}}},
			{Stage: "execution_limit", Phase: "failed", ExecutionLimit: &agentExecutionLimit{Kind: "requests", Used: 8, Maximum: 8}},
		} {
			event.Version, event.RunID, event.AnalysisRequestID, event.AttemptID = 1, received.DiagnosticRunID, received.AnalysisRequestID, received.AttemptID
			event.ProducerSeq, event.OccurredAt = i+2, time.Now().UTC().Format(time.RFC3339Nano)
			_ = encoder.Encode(map[string]any{"type": "progress", "event": event})
		}
		_ = encoder.Encode(map[string]any{"type": "error", "code": "budget_exhausted"})
	}))
	defer server.Close()
	h.LLMGatewayURL = server.URL
	h.dispatchSnapshotAgent(req)
	w := httptest.NewRecorder()
	(&DiagnosticRuns{DB: h.DB, DefaultNamespace: "kubechan"}).GetAnalysisProgress(w, withChiParam(httptest.NewRequest("GET", "/", nil), "id", "run-agent"))
	var progress struct {
		Status, ReasonCode string
		Events             []agentProgress
	}
	if err := json.Unmarshal(w.Body.Bytes(), &progress); err != nil {
		t.Fatal(err)
	}
	if progress.Status != "failed" || progress.ReasonCode != "budget_exhausted" || len(progress.Events) != 5 {
		t.Fatalf("progress = %s", w.Body.String())
	}
	if progress.Events[1].Target.Resource.Name != "gateway" || progress.Events[1].Availability != "not_collected" || strings.Contains(w.Body.String(), "UNTRUSTED-LABEL") {
		t.Fatalf("noncanonical activity: %s", w.Body.String())
	}
	if progress.Events[2].ValidationFields[0].Path != "nodeInsights.4.comment" {
		t.Fatalf("lost validation detail: %s", w.Body.String())
	}
	if progress.Events[3].ExecutionLimit == nil || progress.Events[3].ExecutionLimit.Kind != "requests" {
		t.Fatal("lost execution limit detail")
	}
	hub := h.Hub.(*agentTestHub)
	var activity []agentProgress
	for _, body := range hub.events {
		var event agentProgress
		_ = json.Unmarshal(body, &event)
		if event.Type == "Analysis.Progress" {
			activity = append(activity, event)
		}
	}
	if len(activity) != 5 || len(activity[2].ValidationFields) != 1 {
		t.Fatalf("broadcasts = %q", hub.events)
	}
	for i, event := range activity {
		if event.Seq != i+1 {
			t.Fatal("broadcast order differs from persisted order")
		}
	}
}

func TestAgentProgressRejectsUnboundedOrMalformedDetails(t *testing.T) {
	h, _ := setupAgent(t)
	for _, event := range []agentProgress{
		{Availability: "arbitrary source text"},
		{ReasonCode: "PRIVATE source content"},
		{ValidationFields: []agentValidationField{{Path: "<script>private</script>", Kind: "missing"}}},
		{ValidationFields: make([]agentValidationField, 9)},
		{Target: &agentProgressTarget{NodeID: strings.Repeat("a", 513)}},
		{ExecutionLimit: &agentExecutionLimit{Kind: "raw model content", Used: 8, Maximum: 8}},
	} {
		event.Version, event.Stage, event.Phase, event.ProducerSeq = 1, "validating", "failed", 1
		event.OccurredAt = time.Now().UTC().Format(time.RFC3339Nano)
		if h.persistAgentProgress(context.Background(), event) == nil {
			t.Fatal("invalid progress details accepted")
		}
	}
}

func TestSnapshotAgentCancellationStopsRequestAndRejectsLateResult(t *testing.T) {
	h, req := setupAgent(t)
	started, stopped, finished := make(chan llmAnalyzeRequest, 1), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var received llmAnalyzeRequest
		_ = json.NewDecoder(r.Body).Decode(&received)
		writeAgentProgress(t, json.NewEncoder(w), received, 1, "indexing")
		w.(http.Flusher).Flush()
		started <- received
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	h.LLMGatewayURL = server.URL
	go func() { h.dispatchSnapshotAgent(req); close(finished) }()
	var received llmAnalyzeRequest
	select {
	case received = <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("agent did not start")
	}
	w := httptest.NewRecorder()
	h.CancelAnalysis(w, withChiParam(httptest.NewRequest("POST", "/", nil), "id", "run-agent"))
	if w.Code != http.StatusAccepted {
		t.Fatalf("cancel = %s", w.Body.String())
	}
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("gateway request was not cancelled")
	}
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("dispatch did not terminate")
	}
	if err := h.completeAgentAttempt(received, testAgentResult(received)); err == nil {
		t.Fatal("late result accepted")
	}
	var status string
	var count int
	_ = h.DB.QueryRow(`SELECT status FROM analysis_attempts`).Scan(&status)
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM analysis_progress WHERE event_id = 'terminal'`).Scan(&count)
	if status != "cancelled" || count != 1 {
		t.Fatalf("status=%s terminal events=%d", status, count)
	}
}

func TestSnapshotProgressCompatibilityAndPolicy(t *testing.T) {
	h, _ := setupAgent(t)
	w := httptest.NewRecorder()
	(&DiagnosticRuns{DB: h.DB, DefaultNamespace: "kubechan"}).GetAnalysisProgress(w, withChiParam(httptest.NewRequest("GET", "/", nil), "id", "old-run"))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"available":false`) {
		t.Fatalf("legacy progress = %s", w.Body.String())
	}
	if err := h.persistAgentProgress(context.Background(), agentProgress{Stage: "arbitrary model text", Phase: "started"}); err == nil {
		t.Fatal("arbitrary status accepted")
	}
}

func TestSnapshotEvidenceRetriesAreIdempotentAndCannotReplaceSnapshot(t *testing.T) {
	h, _ := setupAgent(t)
	// No gateway URL: exercise collection persistence without dispatching a model.
	for _, test := range []struct {
		payload string
		want    int
	}{{`{"fact":"original"}`, 201}, {`{"fact":"original"}`, 201}, {`{"fact":"changed"}`, 409}} {
		body, _ := json.Marshal(evidenceRequest{DiagnosticRunID: "run-agent", Payload: json.RawMessage(test.payload)})
		w := httptest.NewRecorder()
		h.ReceiveEvidence(w, httptest.NewRequest("POST", "/internal/evidence", bytes.NewReader(body)))
		if w.Code != test.want {
			t.Fatalf("status=%d want=%d body=%s", w.Code, test.want, w.Body.String())
		}
	}
	var count int
	var stored string
	_ = h.DB.QueryRow(`SELECT COUNT(*), payload FROM evidence WHERE diagnostic_run_id = 'run-agent'`).Scan(&count, &stored)
	if count != 1 || stored != `{"fact":"original"}` {
		t.Fatalf("count=%d payload=%s", count, stored)
	}
}

func TestAgentRunDeletionCleansProgressWithoutForeignKeyPragma(t *testing.T) {
	h, _ := setupAgent(t)
	if _, err := h.DB.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DB.Exec(`INSERT INTO analysis_attempts(id, analysis_request_id, diagnostic_run_id, evidence_id, status, stage, created_at, updated_at) VALUES ('attempt', 'request-agent', 'run-agent', 'evidence', 'failed', 'failed', '', '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DB.Exec(`INSERT INTO analysis_progress(attempt_id, seq, event_id, payload) VALUES ('attempt', 1, 'terminal', '{}')`); err != nil {
		t.Fatal(err)
	}
	tx, err := h.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err = (&DiagnosticRuns{DB: h.DB}).deleteRunFromDB(tx, "run-agent"); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"analysis_requests", "analysis_attempts", "analysis_progress"} {
		var count int
		if err = h.DB.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&count); err != nil || count != 0 {
			t.Fatalf("%s count=%d error=%v", table, count, err)
		}
	}
}

func TestInterruptedAgentOverridesStaleCRDForRESTPolling(t *testing.T) {
	h, _ := setupAgent(t)
	if _, err := h.DB.Exec(`INSERT INTO analysis_attempts(id, analysis_request_id, diagnostic_run_id, evidence_id, status, stage, created_at, updated_at) VALUES ('attempt', 'request-agent', 'run-agent', 'evidence', 'interrupted', 'interrupted', '', '')`); err != nil {
		t.Fatal(err)
	}
	runs := &DiagnosticRuns{DB: h.DB, K8s: h.K8s, DefaultNamespace: "kubechan"}
	w := httptest.NewRecorder()
	runs.Get(w, withChiParam(httptest.NewRequest("GET", "/", nil), "id", "run-agent"))
	var body struct {
		DiagnosticRun v1alpha1.DiagnosticRun `json:"diagnosticRun"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.DiagnosticRun.Status.AnalysisState != v1alpha1.DiagnosticRunAnalysisStateFailed {
		t.Fatalf("REST state=%s", body.DiagnosticRun.Status.AnalysisState)
	}
	w = httptest.NewRecorder()
	runs.List(w, httptest.NewRequest("GET", "/", nil))
	var list []runSummary
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Status != "failed" || list[0].AgentStatus != "interrupted" {
		t.Fatalf("list=%s", w.Body.String())
	}
}

func TestEvidenceAndAgentDispatchStateCommitAtomically(t *testing.T) {
	h, _ := setupAgent(t)
	h.LLMGatewayURL = "http://127.0.0.1:1"
	if _, err := h.DB.Exec(`UPDATE analysis_requests SET status = 'pending'`); err != nil {
		t.Fatal(err)
	}
	if _, err := h.DB.Exec(`CREATE TRIGGER reject_request_update BEFORE UPDATE ON analysis_requests BEGIN SELECT RAISE(ABORT, 'test rollback'); END`); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(evidenceRequest{DiagnosticRunID: "run-agent", Payload: json.RawMessage(`{"fact":"original"}`)})
	w := httptest.NewRecorder()
	h.ReceiveEvidence(w, httptest.NewRequest("POST", "/", bytes.NewReader(body)))
	if w.Code != 500 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var count int
	_ = h.DB.QueryRow(`SELECT COUNT(*) FROM evidence`).Scan(&count)
	if count != 0 {
		t.Fatalf("partial evidence committed: %d records", count)
	}
}
