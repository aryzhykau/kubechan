// Copyright 2024 The KubeChan Authors.
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// injectUser injects user context values into a request (simulates RequireAuth).
func injectUser(r *http.Request, userID, username, role string) *http.Request {
	ctx := context.WithValue(r.Context(), ctxKeyUserID{}, userID)
	ctx = context.WithValue(ctx, ctxKeyUsername{}, username)
	ctx = context.WithValue(ctx, ctxKeyRole{}, role)
	return r.WithContext(ctx)
}

func TestLLMSettings_Get_NoSettings_ReturnsDefault(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &LLMSettings{DB: db}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/me/llm-settings", nil)
	r = injectUser(r, "uid-1", "alice", "viewer")
	w := httptest.NewRecorder()
	h.Get(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	var resp llmSettingsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Provider != "bedrock" {
		t.Errorf("default provider = %q, want bedrock", resp.Provider)
	}
	if resp.Configured {
		t.Error("expected Configured=false for empty settings")
	}
}

func TestLLMSettings_Put_InvalidJSON(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &LLMSettings{DB: db}

	r := httptest.NewRequest(http.MethodPut, "/", bytes.NewBufferString("not json"))
	r = injectUser(r, "uid-1", "alice", "viewer")
	w := httptest.NewRecorder()
	h.Put(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestLLMSettings_Put_InvalidProvider(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &LLMSettings{DB: db}

	body, _ := json.Marshal(llmSettingsPutRequest{Provider: "openai", Credentials: map[string]any{}})
	r := httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(body))
	r = injectUser(r, "uid-1", "alice", "viewer")
	w := httptest.NewRecorder()
	h.Put(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestLLMSettings_Put_Success(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	insertTestUser(t, db, "uid-1")
	h := &LLMSettings{DB: db}

	body, _ := json.Marshal(llmSettingsPutRequest{
		Provider:    "bedrock",
		Credentials: map[string]any{"region": "us-east-1"},
	})
	r := httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(body))
	r = injectUser(r, "uid-1", "alice", "viewer")
	w := httptest.NewRecorder()
	h.Put(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
}

func TestLLMSettings_Get_AfterPut_ShowsConfigured(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &LLMSettings{DB: db}
	userID := "uid-put-get"
	insertTestUser(t, db, userID)

	// First PUT some settings.
	putBody, _ := json.Marshal(llmSettingsPutRequest{
		Provider:    "copilot",
		Credentials: map[string]any{"token": "secret-token"},
	})
	putReq := httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(putBody))
	putReq = injectUser(putReq, userID, "alice", "viewer")
	h.Put(httptest.NewRecorder(), putReq)

	// Now GET and verify configured=true, token is masked.
	getReq := httptest.NewRequest(http.MethodGet, "/", nil)
	getReq = injectUser(getReq, userID, "alice", "viewer")
	w := httptest.NewRecorder()
	h.Get(w, getReq)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	var resp llmSettingsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Configured {
		t.Error("expected Configured=true after PUT")
	}
	if token, ok := resp.CredFields["token"]; ok && token != "***" {
		t.Errorf("token should be masked as ***, got %v", token)
	}
}

func TestLLMSettings_Put_MergesExistingSecrets(t *testing.T) {
	t.Parallel()
	db := openDB(t)
	h := &LLMSettings{DB: db}
	userID := "uid-merge"
	insertTestUser(t, db, userID)

	// Initial PUT with a secret.
	putBody1, _ := json.Marshal(llmSettingsPutRequest{
		Provider:    "bedrock",
		Credentials: map[string]any{"accessKeyId": "key-abc", "region": "us-east-1"},
	})
	r1 := injectUser(httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(putBody1)), userID, "u", "v")
	h.Put(httptest.NewRecorder(), r1)

	// Second PUT omits the secret field → should preserve it.
	putBody2, _ := json.Marshal(llmSettingsPutRequest{
		Provider:    "bedrock",
		Credentials: map[string]any{"region": "eu-west-1"},
	})
	r2 := injectUser(httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(putBody2)), userID, "u", "v")
	w2 := httptest.NewRecorder()
	h.Put(w2, r2)

	if w2.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w2.Code)
	}

	// Verify the secret is still masked (meaning it was kept).
	getReq := injectUser(httptest.NewRequest(http.MethodGet, "/", nil), userID, "u", "v")
	wGet := httptest.NewRecorder()
	h.Get(wGet, getReq)
	var resp llmSettingsResponse
	if err := json.NewDecoder(wGet.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.CredFields["accessKeyId"] != "***" {
		t.Errorf("expected accessKeyId to be masked, got %v", resp.CredFields["accessKeyId"])
	}
}
