package observability

import (
	"strings"
	"testing"
	"time"
)

func TestMetricsRenderPrometheus(t *testing.T) {
	IncWagerResult("PROCESSED")
	IncDuplicate()
	IncRetry()
	IncDLQ()
	IncConcurrencyConflict()
	IncReconciliationDivergence()
	ObserveProcessingLatency(250 * time.Millisecond)
	ObserveOutboxLag(2 * time.Second)

	body := RenderPrometheus()
	required := []string{
		"jungle_wager_results_total",
		"jungle_duplicates_total",
		"jungle_retries_total",
		"jungle_dlq_total",
		"jungle_concurrency_conflicts_total",
		"jungle_reconciliation_divergences_total",
		"jungle_processing_latency_seconds_count",
		"jungle_outbox_lag_seconds_count",
	}
	for _, item := range required {
		if !strings.Contains(body, item) {
			t.Fatalf("metrics output missing %q", item)
		}
	}
}

func TestJSONLineIsValid(t *testing.T) {
	line, err := JSONLine(map[string]any{
		"correlationId": "corr-1",
		"providerId":    "provider-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(line), `"correlationId":"corr-1"`) {
		t.Fatalf("unexpected JSON: %s", line)
	}
}
