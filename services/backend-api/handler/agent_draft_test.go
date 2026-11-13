package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func draftFixture() (map[string]any, agentExposure, *agentDraft) {
	payload := map[string]any{"graph": map[string]any{"nodes": []any{map[string]any{"id": "entry"}}, "edges": []any{}, "candidates": []any{}}, "sources": []any{map[string]any{"id": "field:entry", "resource": map[string]any{"kind": "Deployment"}}}}
	exposed := agentExposure{NodeIDs: []string{"entry"}, SourceIDs: []string{"field:entry"}}
	draft := &agentDraft{Version: 1, Revision: 1, HasConclusion: true, Graph: agentExposure{NodeIDs: []string{"entry"}}, Assessment: json.RawMessage(`{"version":2,"summary":"Inspected","confidence":0.5,"claims":[{"id":"c1","statement":"Observed","verdict":"supported","citations":[{"sourceId":"field:entry"}]}],"nodeInsights":[{"nodeId":"entry","relevance":"relevant","comment":"Observed state","claimIds":["c1"],"sourceIds":["field:entry"]}]}`)}
	return payload, exposed, draft
}

func TestLiveInvestigationGraphCanGrowWithoutReviews(t *testing.T) {
	payload, _, _ := draftFixture()
	payload["graph"] = map[string]any{"entryResourceId": "entry", "nodes": []any{
		map[string]any{"id": "entry"}, map[string]any{"id": "missing"},
	}, "edges": []any{map[string]any{"id": "ref", "from": "entry", "to": "missing"}}}
	v := newAgentDraftValidator(payload)
	root := agentExposure{NodeIDs: []string{"entry"}}
	full := agentExposure{NodeIDs: []string{"entry", "missing"}, EdgeIDs: []string{"ref"}, SourceIDs: []string{"field:entry"}}
	for _, event := range []agentProgress{
		{Stage: "indexing", Phase: "completed", InvestigationGraph: &root},
		{Stage: "get_resource", Phase: "completed", InvestigationGraph: &full},
	} {
		if err := v.validate(event); err != nil {
			t.Fatal(err)
		}
	}
	if v.validate(agentProgress{Stage: "get_resource", Phase: "completed", InvestigationGraph: &root}) == nil {
		t.Fatal("accepted graph regression")
	}
	full.NodeIDs = append(full.NodeIDs, "unknown")
	if v.validate(agentProgress{Stage: "get_resource", Phase: "completed", InvestigationGraph: &full}) == nil {
		t.Fatal("accepted a node outside the snapshot")
	}
}

func TestModelDrawingRequiresDeliveredProofForMissingEndpoint(t *testing.T) {
	payload, exposed, draft := draftFixture()
	payload["graph"] = map[string]any{
		"entryResourceId": "entry",
		"nodes": []any{
			map[string]any{"id": "entry"},
			map[string]any{"id": "missing", "type": "reference", "state": "missing"},
			map[string]any{"id": "unread"},
		},
		"edges": []any{map[string]any{
			"id": "ref", "from": "entry", "to": "missing", "origin": "observed", "state": "broken", "sourceIds": []string{"field:entry"},
		}},
	}
	exposed.NodeIDs = []string{"entry", "missing", "unread"}
	exposed.EdgeIDs = []string{"ref"}
	v := newAgentDraftValidator(payload)
	root := agentExposure{NodeIDs: []string{"entry"}}
	if err := v.validate(agentProgress{Stage: "indexing", Phase: "completed", InvestigationGraph: &root}); err != nil {
		t.Fatal(err)
	}
	drawing := agentExposure{NodeIDs: []string{"entry", "missing"}, EdgeIDs: []string{"ref"}}
	event := agentProgress{Stage: "draft_updated", Phase: "completed", Draft: draft, Change: &agentDraftChange{Kind: "graph", ID: "drawing"}, InvestigationGraph: &drawing}
	withoutProof := exposed
	withoutProof.SourceIDs = nil
	if err := v.validate(agentProgress{Stage: "context_delivered", Exposed: &withoutProof}); err != nil {
		t.Fatal(err)
	}
	if v.validate(event) == nil {
		t.Fatal("accepted a missing endpoint before its source was delivered")
	}
	if err := v.validate(agentProgress{Stage: "context_delivered", Exposed: &exposed}); err != nil {
		t.Fatal(err)
	}
	drawing.NodeIDs = append(drawing.NodeIDs, "unread")
	if v.validate(event) == nil {
		t.Fatal("accepted an unrelated, unread node in the drawing")
	}
	drawing.NodeIDs = drawing.NodeIDs[:2]
	if err := v.validate(event); err != nil {
		t.Fatal(err)
	}
}

func TestDraftValidatorRejectsUnknownUnreadAndRegressingState(t *testing.T) {
	for _, scenario := range []string{"unknown-node", "unread-source", "unknown-edge", "revision", "model-quote", "missing-review", "regression"} {
		t.Run(scenario, func(t *testing.T) {
			payload, exposed, draft := draftFixture()
			v := newAgentDraftValidator(payload)
			if scenario != "unread-source" {
				if err := v.validate(agentProgress{Stage: "context_delivered", Exposed: &exposed}); err != nil {
					t.Fatal(err)
				}
			}
			event := agentProgress{Stage: "draft_updated", Draft: draft, Change: &agentDraftChange{Kind: "review", ID: "entry"}}
			switch scenario {
			case "unknown-node":
				draft.Graph.NodeIDs = []string{"invented"}
			case "unknown-edge":
				draft.Graph.EdgeIDs = []string{"invented"}
			case "revision":
				draft.Revision = 2
			case "model-quote":
				draft.Assessment = json.RawMessage(`{"version":2,"summary":"x","confidence":0.5,"claims":[{"id":"c1","verdict":"supported","citations":[{"sourceId":"field:entry","quote":"model text"}]}]}`)
			case "missing-review":
				draft.Assessment = json.RawMessage(`{"version":2,"summary":"x","confidence":0.5}`)
			case "regression":
				event = agentProgress{Stage: "context_delivered", Exposed: &agentExposure{}}
			}
			if v.validate(event) == nil {
				t.Fatal("invalid live draft accepted")
			}
		})
	}
}

func TestBranchLinksRequireDeliveredRelationshipAndSupportedFinding(t *testing.T) {
	payload, exposed, draft := draftFixture()
	payload["graph"] = map[string]any{"entryResourceId": "entry", "nodes": []any{
		map[string]any{"id": "entry"}, map[string]any{"id": "dependency"},
	}, "edges": []any{map[string]any{"id": "ref", "from": "entry", "to": "dependency"}}}
	exposed.NodeIDs = append(exposed.NodeIDs, "dependency")
	exposed.EdgeIDs = []string{"ref"}
	draft.Branches = []agentBranch{
		{ID: "a", NodeID: "entry", EdgeID: "ref", FindingID: "c1", Question: "Does the dependency exist?",
			Status: "confirmed", Outcome: "Dependency is absent.", SourceIDs: []string{"field:entry"}},
		{ID: "b", NodeID: "entry", FindingID: "c1", Question: "Why is the workload unavailable?",
			Status: "confirmed", Outcome: "The required dependency is absent.", SourceIDs: []string{"field:entry"}},
	}
	v := newAgentDraftValidator(payload)
	event := agentProgress{Stage: "draft_updated", Draft: draft, Change: &agentDraftChange{Kind: "branch", ID: "a"}}
	unread := exposed
	unread.EdgeIDs = nil
	if err := v.validate(agentProgress{Stage: "context_delivered", Exposed: &unread}); err != nil {
		t.Fatal(err)
	}
	if err := v.validate(event); err == nil || err.Error() != "invalid investigation branch relationship" {
		t.Fatalf("expected rejection for undelivered edge, got %v", err)
	}
	if err := v.validate(agentProgress{Stage: "context_delivered", Exposed: &exposed}); err != nil {
		t.Fatal(err)
	}
	draft.Branches[1].FindingID = "invented"
	if err := v.validate(event); err == nil || err.Error() != "invalid investigation branch finding" {
		t.Fatalf("expected rejection for unknown finding, got %v", err)
	}
	draft.Branches[1].FindingID = "c1"
	if err := v.validate(event); err != nil {
		t.Fatal(err)
	}
}

func TestLiveDraftAndReviewsSurviveFailureRESTAndBroadcast(t *testing.T) {
	h, req := setupAgent(t)
	payload, exposed, draft := draftFixture()
	req.Payload = payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var received llmAnalyzeRequest
		_ = json.NewDecoder(r.Body).Decode(&received)
		encoder := json.NewEncoder(w)
		writeAgentProgress(t, encoder, received, 1, "indexing")
		for i, event := range []agentProgress{{Stage: "context_delivered", Exposed: &exposed}, {Stage: "draft_updated", Draft: draft, Change: &agentDraftChange{Kind: "review", ID: "entry"}}} {
			event.Version, event.RunID, event.AnalysisRequestID, event.AttemptID = 1, received.DiagnosticRunID, received.AnalysisRequestID, received.AttemptID
			event.Phase, event.ProducerSeq, event.OccurredAt = "completed", i+2, time.Now().UTC().Format(time.RFC3339Nano)
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
		Status string
		Events []agentProgress
	}
	if err := json.Unmarshal(w.Body.Bytes(), &progress); err != nil {
		t.Fatal(err)
	}
	if progress.Status != "failed" || len(progress.Events) != 4 || progress.Events[2].Draft == nil || progress.Events[2].Draft.Revision != 1 {
		t.Fatalf("lost failed-run draft: %s", w.Body.String())
	}
	if len(h.Hub.(*agentTestHub).events) < 4 {
		t.Fatal("missing live broadcasts")
	}
}
