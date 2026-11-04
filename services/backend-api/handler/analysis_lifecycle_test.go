package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	v1alpha1 "github.com/org/kubechan/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type analysisLifecycleHub struct{ messages [][]byte }

func (h *analysisLifecycleHub) Broadcast(message []byte) {
	h.messages = append(h.messages, message)
}

func TestDiagnosticRunListUsesSettledCRDStateForOldRequests(t *testing.T) {
	db := openDB(t)
	if _, err := db.Exec(`INSERT INTO analysis_requests(id, diagnostic_run_id, status) VALUES ('request-old', 'run-old', 'pending')`); err != nil {
		t.Fatal(err)
	}
	k8s := fake.NewClientBuilder().WithScheme(newTestScheme()).
		WithObjects(&v1alpha1.DiagnosticRun{
			ObjectMeta: metav1.ObjectMeta{Name: "run-old", Namespace: "kubechan"},
			Status: v1alpha1.DiagnosticRunStatus{
				AnalysisState: v1alpha1.DiagnosticRunAnalysisStateFailed,
			},
		}).WithStatusSubresource(&v1alpha1.DiagnosticRun{}).Build()
	h := &DiagnosticRuns{K8s: k8s, DB: db, DefaultNamespace: "kubechan"}
	w := httptest.NewRecorder()
	h.List(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d: %s", w.Code, w.Body.String())
	}
	var runs []struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &runs); err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].Status != "failed" {
		t.Errorf("list = %#v, want one failed run", runs)
	}
}

func TestAnalysisResponsesUseValidatedConfidenceForOlderRuns(t *testing.T) {
	db := openDB(t)
	if _, err := db.Exec(`INSERT INTO analysis_requests(id, diagnostic_run_id, status) VALUES ('request-confidence', 'run-confidence', 'completed')`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO analysis_results(id, diagnostic_run_id, model, status, confidence, payload)
		VALUES ('result-confidence', 'run-confidence', 'test', 'completed', 0.95, '{"assessment":{"confidence":0.69}}')`); err != nil {
		t.Fatal(err)
	}
	k8s := fake.NewClientBuilder().WithScheme(newTestScheme()).Build()
	list := httptest.NewRecorder()
	(&DiagnosticRuns{K8s: k8s, DB: db, DefaultNamespace: "kubechan"}).List(list, httptest.NewRequest(http.MethodGet, "/", nil))
	var runs []struct {
		Confidence float64 `json:"confidence"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &runs); err != nil {
		t.Fatal(err)
	}
	if list.Code != http.StatusOK || len(runs) != 1 || runs[0].Confidence != 0.69 {
		t.Errorf("list status = %d, runs = %#v", list.Code, runs)
	}

	for _, test := range []struct {
		name, id string
		get      func(http.ResponseWriter, *http.Request)
	}{
		{"by run", "run-confidence", (&DiagnosticRuns{K8s: k8s, DB: db, DefaultNamespace: "kubechan"}).GetAnalysisResult},
		{"by result", "result-confidence", (&Analysis{K8s: k8s, DB: db, DefaultNamespace: "kubechan"}).GetAnalysisResult},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			test.get(w, withChiParam(httptest.NewRequest(http.MethodGet, "/", nil), "id", test.id))
			var result struct {
				Confidence float64 `json:"confidence"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || result.Confidence != 0.69 {
				t.Errorf("status = %d, confidence = %v", w.Code, result.Confidence)
			}
		})
	}
}

func TestPatchAnalysisStateUpdatesRunListAndFailureEvent(t *testing.T) {
	db := openDB(t)
	for _, id := range []string{"run-failed", "run-completed"} {
		if _, err := db.Exec(`INSERT INTO analysis_requests(id, diagnostic_run_id, status) VALUES (?, ?, 'pending')`, id, id); err != nil {
			t.Fatal(err)
		}
	}
	k8s := fake.NewClientBuilder().WithScheme(newTestScheme()).
		WithObjects(
			&v1alpha1.DiagnosticRun{ObjectMeta: metav1.ObjectMeta{Name: "run-failed", Namespace: "kubechan"}},
			&v1alpha1.DiagnosticRun{ObjectMeta: metav1.ObjectMeta{Name: "run-completed", Namespace: "kubechan"}},
		).
		WithStatusSubresource(&v1alpha1.DiagnosticRun{}).Build()
	hub := &analysisLifecycleHub{}
	h := &Internal{K8s: k8s, DB: db, DefaultNamespace: "kubechan", Hub: hub}

	h.patchAnalysisState("run-failed", v1alpha1.DiagnosticRunAnalysisStateFailed, "")
	h.patchAnalysisState("run-completed", v1alpha1.DiagnosticRunAnalysisStateCompleted, "result-1")

	for _, test := range []struct {
		id, status, resultRef string
	}{
		{"run-failed", "failed", ""},
		{"run-completed", "completed", "result-1"},
	} {
		var status string
		if err := db.QueryRow(`SELECT status FROM analysis_requests WHERE diagnostic_run_id = ?`, test.id).Scan(&status); err != nil {
			t.Fatal(err)
		}
		if status != test.status {
			t.Errorf("%s request status = %q, want %q", test.id, status, test.status)
		}
		run := &v1alpha1.DiagnosticRun{}
		if err := k8s.Get(context.Background(), client.ObjectKey{Namespace: "kubechan", Name: test.id}, run); err != nil {
			t.Fatal(err)
		}
		if string(run.Status.AnalysisState) != test.status || run.Status.AnalysisResultRef != test.resultRef {
			t.Errorf("%s run status = %q, result = %q", test.id, run.Status.AnalysisState, run.Status.AnalysisResultRef)
		}
	}
	if len(hub.messages) != 1 {
		t.Fatalf("failure events = %d, want 1", len(hub.messages))
	}
	var event map[string]string
	if err := json.Unmarshal(hub.messages[0], &event); err != nil {
		t.Fatal(err)
	}
	if event["type"] != "Analysis.Failed" || event["diagnosticRunId"] != "run-failed" {
		t.Errorf("failure event = %#v", event)
	}
}
