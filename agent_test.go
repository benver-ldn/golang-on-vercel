package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/option"
)

// TestRecommendToolLoop fakes AI Gateway: the first call returns a tool call,
// the second returns the final structured answer. It verifies the loop sends
// the tool result back and parses the final JSON.
func TestRecommendToolLoop(t *testing.T) {
	var calls int
	var secondRequest map[string]any

	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test_key" {
			t.Errorf("unexpected auth header %q", got)
		}
		body, _ := io.ReadAll(r.Body)

		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			io.WriteString(w, `{"id":"1","object":"chat.completion","model":"m","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","content":"","tool_calls":[{"id":"call_1","type":"function","function":{"name":"get_forecast","arguments":"{\"city\":\"Lisbon\"}"}}]}}]}`)
			return
		}
		json.Unmarshal(body, &secondRequest)
		io.WriteString(w, `{"id":"2","object":"chat.completion","model":"m","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"city\":\"Lisbon\",\"summary\":\"Warm and dry.\",\"recommendations\":[{\"name\":\"Time Out Market\",\"category\":\"food\",\"description\":\"Food hall.\",\"bestTime\":\"Lunch\",\"weatherDependent\":false}]}"}}]}`)
	}))
	defer gateway.Close()

	agent := NewAgent("test/model", option.WithBaseURL(gateway.URL))

	rec, err := agent.Recommend(context.Background(), "test_key", "Lisbon", 2, []string{"food"})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 gateway calls, got %d", calls)
	}
	if rec.City != "Lisbon" || len(rec.Recommendations) != 1 || rec.Recommendations[0].Category != "food" {
		t.Fatalf("unexpected recommendation: %+v", rec)
	}

	// The second request must carry the assistant's tool call and our tool result.
	msgs := secondRequest["messages"].([]any)
	last := msgs[len(msgs)-1].(map[string]any)
	if last["role"] != "tool" || last["tool_call_id"] != "call_1" {
		t.Fatalf("expected tool result as last message, got %v", last)
	}
	if !strings.Contains(last["content"].(string), "Lisbon") {
		t.Fatalf("tool result did not contain forecast: %v", last["content"])
	}
	if secondRequest["response_format"].(map[string]any)["type"] != "json_schema" {
		t.Fatalf("expected json_schema response_format")
	}
}

func TestRecommendRequiresCredential(t *testing.T) {
	agent := NewAgent("m")
	if _, err := agent.Recommend(context.Background(), "", "Lisbon", 1, nil); err == nil {
		t.Fatal("expected error without a credential")
	}
}

func TestGatewayCredentialPrefersHeaderOverEnvToken(t *testing.T) {
	t.Setenv("AI_GATEWAY_API_KEY", "")
	t.Setenv("VERCEL_OIDC_TOKEN", "env_token")
	r := httptest.NewRequest(http.MethodPost, "/recommendations", nil)
	if got := gatewayCredential(r); got != "env_token" {
		t.Fatalf("expected env fallback, got %q", got)
	}
	r.Header.Set("x-vercel-oidc-token", "header_token")
	if got := gatewayCredential(r); got != "header_token" {
		t.Fatalf("expected header token, got %q", got)
	}
	t.Setenv("AI_GATEWAY_API_KEY", "api_key")
	if got := gatewayCredential(r); got != "api_key" {
		t.Fatalf("expected API key to win, got %q", got)
	}
}
