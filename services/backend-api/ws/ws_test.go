// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package ws

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	gorillaws "github.com/gorilla/websocket"
	"github.com/golang-jwt/jwt/v5"
)

// ---- Hub tests ----

func TestNewHub_NotNil(t *testing.T) {
	t.Parallel()
	h := NewHub(slog.Default())
	if h == nil {
		t.Fatal("expected non-nil Hub")
	}
}

func TestHub_Broadcast_EmptyHub_NoPanic(t *testing.T) {
	t.Parallel()
	h := NewHub(slog.Default())
	// Broadcast to an empty hub must not panic.
	h.Broadcast([]byte(`{"type":"test"}`))
}

// ---- Event / Marshal tests ----

func TestEventConstants_NonEmpty(t *testing.T) {
	t.Parallel()
	constants := []string{
		EventProblemCaseCreated,
		EventProblemCaseUpdated,
		EventProblemCaseResolved,
		EventIncidentCreated,
		EventIncidentUpdated,
		EventIncidentResolved,
		EventDiagnosticRunStateChanged,
		EventAnalysisResultCompleted,
		EventAnalysisResultFailed,
		EventKubeChanStateUpdated,
	}
	for _, c := range constants {
		if c == "" {
			t.Errorf("unexpected empty event constant")
		}
	}
}

func TestMarshal_ProblemCaseEvent(t *testing.T) {
	t.Parallel()
	ev := ProblemCaseEvent{
		BaseEvent: BaseEvent{Type: EventProblemCaseCreated},
		Namespace: "default",
		Name:      "pc-1",
		Severity:  "high",
		State:     "active",
		Detector:  "crash-loop",
	}
	b := Marshal(ev)
	if len(b) == 0 {
		t.Fatal("expected non-empty JSON")
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["type"] != EventProblemCaseCreated {
		t.Errorf("type mismatch: got %v", m["type"])
	}
	if m["namespace"] != "default" {
		t.Errorf("namespace mismatch: got %v", m["namespace"])
	}
}

func TestMarshal_IncidentEvent(t *testing.T) {
	t.Parallel()
	ev := IncidentEvent{
		BaseEvent:        BaseEvent{Type: EventIncidentCreated},
		Namespace:        "prod",
		Name:             "inc-1",
		State:            "open",
		RootResourceKind: "Deployment",
		RootResourceName: "myapp",
	}
	b := Marshal(ev)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if m["rootResourceKind"] != "Deployment" {
		t.Errorf("rootResourceKind mismatch: got %v", m["rootResourceKind"])
	}
}

func TestMarshal_KubeChanStateEvent(t *testing.T) {
	t.Parallel()
	ev := KubeChanStateEvent{
		BaseEvent:         BaseEvent{Type: EventKubeChanStateUpdated},
		MoodLevel:         2,
		OpenIncidentCount: 5,
		PokeCount:         3,
	}
	b := Marshal(ev)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if int(m["moodLevel"].(float64)) != 2 {
		t.Errorf("moodLevel mismatch: got %v", m["moodLevel"])
	}
}

// ---- validateWSToken tests ----

func signTestToken(t *testing.T, secret string, ttl time.Duration) string {
	t.Helper()
	claims := jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return tok
}

func TestValidateWSToken_InvalidToken(t *testing.T) {
	t.Setenv("JWT_SECRET", "testsecret32bytesfortestingonly!!")
	err := validateWSToken("not.a.valid.token")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestValidateWSToken_ValidToken(t *testing.T) {
	secret := "testsecret32bytesfortestingonly!!"
	t.Setenv("JWT_SECRET", secret)
	tok := signTestToken(t, secret, time.Hour)
	if err := validateWSToken(tok); err != nil {
		t.Errorf("expected valid token to pass: %v", err)
	}
}

func TestValidateWSToken_ExpiredToken(t *testing.T) {
	secret := "testsecret32bytesfortestingonly!!"
	t.Setenv("JWT_SECRET", secret)
	tok := signTestToken(t, secret, -time.Minute)
	if err := validateWSToken(tok); err == nil {
		t.Error("expected error for expired token")
	}
}

func TestValidateWSToken_WrongSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "correct-secret-32bytes-for-test!!")
	tok := signTestToken(t, "wrong-secret-32bytes-for-test!!!", time.Hour)
	if err := validateWSToken(tok); err == nil {
		t.Error("expected error for token signed with wrong secret")
	}
}

// ---- ServeWSWithAuth HTTP gate tests ----

func TestServeWSWithAuth_MissingToken_Returns401(t *testing.T) {
	t.Setenv("JWT_SECRET", "testsecret32bytesfortestingonly!!")
	h := NewHub(slog.Default())
	handler := ServeWSWithAuth(h, slog.Default())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 got %d", rec.Code)
	}
}

func TestServeWSWithAuth_InvalidToken_Returns401(t *testing.T) {
	t.Setenv("JWT_SECRET", "testsecret32bytesfortestingonly!!")
	h := NewHub(slog.Default())
	handler := ServeWSWithAuth(h, slog.Default())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws?token=badtoken", nil)
	handler(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 got %d", rec.Code)
	}
}

// ---- ServeWS full connection tests (register/readPump/writePump) ----

func TestServeWS_Connect_RegistersClient(t *testing.T) {
	hub := NewHub(slog.Default())
	srv := httptest.NewServer(ServeWS(hub, slog.Default()))
	defer srv.Close()

	u := "ws" + srv.URL[4:] // replace "http" with "ws"
	conn, _, err := connectWS(u)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	// Give readPump/writePump a moment to start.
	time.Sleep(20 * time.Millisecond)

	hub.mu.RLock()
	count := len(hub.clients)
	hub.mu.RUnlock()
	if count != 1 {
		t.Errorf("expected 1 registered client, got %d", count)
	}
}

func TestServeWS_Broadcast_DeliveredToClient(t *testing.T) {
	hub := NewHub(slog.Default())
	srv := httptest.NewServer(ServeWS(hub, slog.Default()))
	defer srv.Close()

	u := "ws" + srv.URL[4:]
	conn, _, err := connectWS(u)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()

	time.Sleep(20 * time.Millisecond)

	msg := []byte(`{"type":"test-broadcast"}`)
	hub.Broadcast(msg)

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, got, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(msg) {
		t.Errorf("got %q, want %q", got, msg)
	}
}

func TestServeWS_ClientDisconnect_Unregisters(t *testing.T) {
	hub := NewHub(slog.Default())
	srv := httptest.NewServer(ServeWS(hub, slog.Default()))
	defer srv.Close()

	u := "ws" + srv.URL[4:]
	conn, _, err := connectWS(u)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	// Close the connection and wait for readPump to unregister.
	_ = conn.Close()
	time.Sleep(100 * time.Millisecond)

	hub.mu.RLock()
	count := len(hub.clients)
	hub.mu.RUnlock()
	if count != 0 {
		t.Errorf("expected 0 clients after disconnect, got %d", count)
	}
}

func TestServeWSWithAuth_ValidToken_Connects(t *testing.T) {
	secret := "testsecret32bytesfortestingonly!!"
	t.Setenv("JWT_SECRET", secret)

	hub := NewHub(slog.Default())
	srv := httptest.NewServer(ServeWSWithAuth(hub, slog.Default()))
	defer srv.Close()

	tok := signTestToken(t, secret, time.Hour)
	u := "ws" + srv.URL[4:] + "?token=" + tok
	conn, _, err := connectWS(u)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(20 * time.Millisecond)

	hub.mu.RLock()
	count := len(hub.clients)
	hub.mu.RUnlock()
	if count != 1 {
		t.Errorf("expected 1 registered client, got %d", count)
	}
}

// connectWS is a helper for the WS integration tests above.
func connectWS(u string) (*gorillaws.Conn, *http.Response, error) {
	return gorillaws.DefaultDialer.Dial(u, nil)
}

// TestMain ensures JWT_SECRET env is not leaked from non-t.Setenv tests.
func TestMain(m *testing.M) {
	// Unset any stray env so parallel tests that don't call t.Setenv start clean.
	os.Unsetenv("JWT_SECRET") //nolint:errcheck
	os.Exit(m.Run())
}
