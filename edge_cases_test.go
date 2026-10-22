package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGatewayFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"empty choices", `{"choices":[]}`, 200},
		{"refusal", `{"choices":[{"finish_reason":"stop","message":{"role":"assistant","refusal":"No"}}]}`, 200},
		{"truncation", `{"choices":[{"finish_reason":"length","message":{"role":"assistant","content":"{}"}}]}`, 200},
		{"authentication", `{"error":{"message":"Unauthorized"}}`, 401},
		{"invalid response JSON", `{`, 200},
		{"oversized response", strings.Repeat("x", (1<<20)+1), 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			g := NewGateway("m")
			g.endpoint = srv.URL + "/chat/completions"
			if _, err := g.Recommend(context.Background(), "key", recommendRequest{}); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestCredentialPrecedence(t *testing.T) {
	t.Setenv("AI_GATEWAY_API_KEY", "")
	t.Setenv("VERCEL_OIDC_TOKEN", "environment-token")
	r := httptest.NewRequest("POST", "/recommendations", nil)
	if gatewayCredential(r) != "environment-token" {
		t.Fatal("missing environment fallback")
	}
	r.Header.Set("x-vercel-oidc-token", "header-token")
	if gatewayCredential(r) != "header-token" {
		t.Fatal("header should override environment token")
	}
	t.Setenv("AI_GATEWAY_API_KEY", "api-key")
	if gatewayCredential(r) != "api-key" {
		t.Fatal("API key should override token")
	}
}

func TestHTTPValidation(t *testing.T) {
	t.Setenv("AI_GATEWAY_API_KEY", "")
	t.Setenv("VERCEL_OIDC_TOKEN", "")
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"city":" "}`, 400},
		{`{"city":"Lisbon","days":-1}`, 400},
		{`{"city":"Lisbon","days":15}`, 400},
		{`{"city":"Lisbon"}{}`, 400},
		{`{"city":"Lisbon","extra":true}`, 400},
		{`null`, 400},
		{`{"city":"` + strings.Repeat("x", 16<<10) + `"}`, 400},
		{`{"city":"Lisbon"}`, 500},
	} {
		w := httptest.NewRecorder()
		recommendHandler(nil).ServeHTTP(w, httptest.NewRequest("POST", "/recommendations", strings.NewReader(tc.body)))
		if w.Code != tc.want {
			t.Errorf("body %.60q: got %d, want %d", tc.body, w.Code, tc.want)
		}
	}
}

func TestTooManyRecommendations(t *testing.T) {
	rec := Recommendation{Summary: "Trip ideas"}
	for range 9 {
		rec.Recommendations = append(rec.Recommendations, Activity{Name: "Place", Description: "Visit"})
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseRecommendation(string(data)); err == nil {
		t.Fatal("expected too many recommendations to fail")
	}
}

func TestCredentialRequired(t *testing.T) {
	if _, err := NewGateway("m").Recommend(context.Background(), "", recommendRequest{}); err == nil {
		t.Fatal("expected missing credential to fail")
	}
}

func TestGatewayHonorsDeadline(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	g := NewGateway("m")
	g.endpoint = srv.URL + "/chat/completions"
	if _, err := g.Recommend(ctx, "key", recommendRequest{}); err == nil || ctx.Err() != context.DeadlineExceeded {
		t.Fatalf("expected deadline error, got %v", err)
	}
}
