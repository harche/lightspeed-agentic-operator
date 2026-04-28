package proposal

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentHTTPClient_QuerySuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/agent/query" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method: %s", r.Method)
		}

		var req agentQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Phase != "analysis" {
			t.Errorf("expected phase='analysis', got %q", req.Phase)
		}
		if req.Query != "check health" {
			t.Errorf("expected query='check health', got %q", req.Query)
		}
		if req.SystemPrompt != "You are an SRE agent" {
			t.Errorf("expected systemPrompt='You are an SRE agent', got %q", req.SystemPrompt)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"options": [{"title": "Fix it"}]}`))
	}))
	defer server.Close()

	client := NewAgentHTTPClient(server.URL)
	resp, err := client.Query(context.Background(), "analysis", "You are an SRE agent", "check health", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Response) == 0 {
		t.Error("expected non-empty response")
	}
}

func TestAgentHTTPClient_QueryHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewAgentHTTPClient(server.URL)
	_, err := client.Query(context.Background(), "execution", "", "test", nil, nil)
	if err == nil {
		t.Fatal("expected error for HTTP 500")
	}
}

func TestAgentHTTPClient_QueryConnectionError(t *testing.T) {
	client := NewAgentHTTPClient("http://127.0.0.1:1")
	_, err := client.Query(context.Background(), "verification", "", "test", nil, nil)
	if err == nil {
		t.Fatal("expected error for connection failure")
	}
}

func TestAgentHTTPClient_QueryWithContext(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req agentQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("failed to decode request: %v", err)
		}
		if req.Context == nil {
			t.Fatal("expected context to be set")
		}
		if len(req.Context.TargetNamespaces) != 1 || req.Context.TargetNamespaces[0] != "production" {
			t.Errorf("targetNamespaces = %v", req.Context.TargetNamespaces)
		}
		if req.Context.Attempt != 2 {
			t.Errorf("attempt = %d, want 2", req.Context.Attempt)
		}
		if len(req.Context.PreviousAttempts) != 1 {
			t.Fatalf("previousAttempts count = %d, want 1", len(req.Context.PreviousAttempts))
		}
		if req.Context.PreviousAttempts[0].FailureReason != "timeout" {
			t.Errorf("failureReason = %q", req.Context.PreviousAttempts[0].FailureReason)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"success": true}`))
	}))
	defer server.Close()

	client := NewAgentHTTPClient(server.URL)
	agentCtx := &agentContext{
		TargetNamespaces: []string{"production"},
		Attempt:          2,
		PreviousAttempts: []agentPreviousAttempt{{Attempt: 1, FailureReason: "timeout"}},
	}
	_, err := client.Query(context.Background(), "analysis", "", "test", nil, agentCtx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
