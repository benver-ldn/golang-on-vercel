package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const validRecommendation = `{"summary":"Ideas for a short Lisbon trip.","recommendations":[
{"name":"Time Out Market","description":"Explore the food stalls."},
{"name":"Mosteiro dos Jeronimos","description":"Visit the monastery."},
{"name":"Museu Nacional do Azulejo","description":"See Portuguese tilework."}]}`

func TestRecommend(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test_key" {
			t.Errorf("unexpected authorization: %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("unexpected content type: %q", got)
		}
		var body struct {
			MaxTokens      int    `json:"max_tokens"`
			Model          string `json:"model"`
			ResponseFormat struct {
				Type       string `json:"type"`
				JSONSchema struct {
					Strict bool           `json:"strict"`
					Schema map[string]any `json:"schema"`
				} `json:"json_schema"`
			} `json:"response_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		if body.MaxTokens != 4096 || body.Model != "test/model" || body.ResponseFormat.Type != "json_schema" ||
			!body.ResponseFormat.JSONSchema.Strict || body.ResponseFormat.JSONSchema.Schema["type"] != "object" {
			t.Errorf("unexpected model or response format: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id": "test", "object": "chat.completion", "model": "test/model",
			"choices": []any{map[string]any{
				"index": 0, "finish_reason": "stop",
				"message": map[string]string{"role": "assistant", "content": validRecommendation},
			}},
		})
	}))
	defer gateway.Close()
	client := NewGateway("test/model")
	client.endpoint = gateway.URL + "/chat/completions"
	rec, err := client.Recommend(context.Background(), "test_key", recommendRequest{City: "Lisbon", Days: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.Recommendations) != 3 || rec.Recommendations[0].Name != "Time Out Market" {
		t.Fatalf("unexpected recommendation: %+v", rec)
	}
}

func TestParseRecommendationRejectsInvalidOutput(t *testing.T) {
	for name, content := range map[string]string{
		"invalid JSON":  "{",
		"null":          "null",
		"empty object":  "{}",
		"too few":       `{"summary":"Trip ideas","recommendations":[]}`,
		"empty field":   strings.Replace(validRecommendation, "Time Out Market", " ", 1),
		"unknown field": strings.Replace(validRecommendation, `"summary":`, `"extra":true,"summary":`, 1),
		"trailing JSON": validRecommendation + `{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := parseRecommendation(content); err == nil {
				t.Fatal("expected invalid output to be rejected")
			}
		})
	}
}
