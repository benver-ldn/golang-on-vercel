package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

type recommendRequest struct {
	City      string   `json:"city"`
	Days      int      `json:"days"`
	Interests []string `json:"interests"`
}

func recommendHandler(agent *Agent) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req recommendRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if req.City == "" {
			writeError(w, http.StatusBadRequest, "city is required")
			return
		}
		if req.Days <= 0 {
			req.Days = 3
		}
		if req.Days > 14 {
			writeError(w, http.StatusBadRequest, "days must be 14 or fewer")
			return
		}

		rec, err := agent.Recommend(r.Context(), req.City, req.Days, req.Interests)
		if err != nil {
			log.Printf("recommend %q: %v", req.City, err)
			writeError(w, http.StatusBadGateway, "recommendation agent failed")
			return
		}
		writeJSON(w, http.StatusOK, rec)
	}
}

func main() {
	// Model IDs resolve through AI Gateway. List current IDs with:
	// curl -s https://ai-gateway.vercel.sh/v1/models
	model := os.Getenv("AI_MODEL")
	if model == "" {
		model = "anthropic/claude-haiku-5.5"
	}

	agent, err := NewAgent(model)
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "model": model})
	})
	mux.HandleFunc("POST /recommendations", recommendHandler(agent))

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
