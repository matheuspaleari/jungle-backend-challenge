package observability

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HealthHandler struct {
	db  *pgxpool.Pool
	sqs *sqs.Client
}

func NewHealthHandler(db *pgxpool.Pool, sqsClient *sqs.Client) *HealthHandler {
	return &HealthHandler{db: db, sqs: sqsClient}
}

func (h *HealthHandler) Live(w http.ResponseWriter, _ *http.Request) {
	writeHealth(w, http.StatusOK, map[string]any{
		"status": "ok",
		"checks": map[string]string{"process": "ok"},
	})
}

func (h *HealthHandler) Ready(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{
		"postgres": "ok",
		"sqs":      "ok",
	}
	status := http.StatusOK

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if h.db == nil || h.db.Ping(ctx) != nil {
		checks["postgres"] = "unavailable"
		status = http.StatusServiceUnavailable
	}

	if h.sqs == nil {
		checks["sqs"] = "unavailable"
		status = http.StatusServiceUnavailable
	} else {
		maxResults := int32(1)
		if _, err := h.sqs.ListQueues(ctx, &sqs.ListQueuesInput{MaxResults: &maxResults}); err != nil {
			checks["sqs"] = "unavailable"
			status = http.StatusServiceUnavailable
		}
	}

	overall := "ready"
	if status != http.StatusOK {
		overall = "not_ready"
	}

	writeHealth(w, status, map[string]any{
		"status": overall,
		"checks": checks,
	})
}

func writeHealth(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
