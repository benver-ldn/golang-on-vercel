package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

type recommendRequest struct {
	City      string   `json:"city"`
	Days      int      `json:"days"`
	Interests []string `json:"interests"`
}

func gatewayCredential(r *http.Request) string {
	if key := os.Getenv("AI_GATEWAY_API_KEY"); key != "" {
		return key
	}
	if token := r.Header.Get("x-vercel-oidc-token"); token != "" {
		return token
	}
	return os.Getenv("VERCEL_OIDC_TOKEN")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func recommendHandler(gateway *Gateway) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		defer r.Body.Close()
		var req recommendRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "expected a JSON object of at most 16 KiB")
			return
		}
		if err := dec.Decode(new(any)); err != io.EOF {
			writeError(w, http.StatusBadRequest, "expected one JSON object")
			return
		}
		req.City = strings.TrimSpace(req.City)
		if req.City == "" {
			writeError(w, http.StatusBadRequest, "city is required")
			return
		}
		if req.Days == 0 {
			req.Days = 3
		}
		if req.Days < 1 || req.Days > 14 {
			writeError(w, http.StatusBadRequest, "days must be between 1 and 14")
			return
		}
		credential := gatewayCredential(r)
		if credential == "" {
			writeError(w, http.StatusInternalServerError, "server is not configured for AI Gateway")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
		defer cancel()
		rec, err := gateway.Recommend(ctx, credential, req)
		if err != nil {
			log.Printf("recommend: %v", err)
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				writeError(w, http.StatusGatewayTimeout, "recommendation timed out")
			} else {
				writeError(w, http.StatusBadGateway, "recommendation failed")
			}
			return
		}
		writeJSON(w, http.StatusOK, rec)
	}
}

func main() {
	model := os.Getenv("AI_MODEL")
	if model == "" {
		model = "anthropic/claude-haiku-5.5"
	}
	gateway := NewGateway(model)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /recommendations", recommendHandler(gateway))
	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	server := &http.Server{
		Addr: ":" + port, Handler: mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("listening on :%s", port)
	log.Fatal(server.ListenAndServe())
}
