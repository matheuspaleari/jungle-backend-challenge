package observability

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

type contextKey string

const correlationKey contextKey = "correlation_id"

var logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
	Level: slog.LevelInfo,
}))

type metricsState struct {
	mu sync.RWMutex

	wagerResults              map[string]uint64
	duplicates                uint64
	retries                   uint64
	dlq                       uint64
	concurrencyConflicts      uint64
	reconciliationDivergences uint64

	processingLatencyCount   uint64
	processingLatencySeconds float64

	outboxLagCount   uint64
	outboxLagSeconds float64
}

var metrics = metricsState{
	wagerResults: make(map[string]uint64),
}

func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, correlationKey, strings.TrimSpace(correlationID))
}

func CorrelationID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	value, _ := ctx.Value(correlationKey).(string)
	return strings.TrimSpace(value)
}

func Log(ctx context.Context, level slog.Level, message string, attrs ...slog.Attr) {
	if correlationID := CorrelationID(ctx); correlationID != "" {
		attrs = append(attrs, slog.String("correlationId", correlationID))
	}
	logger.LogAttrs(ctx, level, message, attrs...)
}

func Info(ctx context.Context, message string, attrs ...slog.Attr) {
	Log(ctx, slog.LevelInfo, message, attrs...)
}

func Error(ctx context.Context, message string, attrs ...slog.Attr) {
	Log(ctx, slog.LevelError, message, attrs...)
}

func IncWagerResult(status string) {
	status = normalizeLabel(status)
	metrics.mu.Lock()
	metrics.wagerResults[status]++
	metrics.mu.Unlock()
}

func IncDuplicate() {
	metrics.mu.Lock()
	metrics.duplicates++
	metrics.mu.Unlock()
}

func IncRetry() {
	metrics.mu.Lock()
	metrics.retries++
	metrics.mu.Unlock()
}

func IncDLQ() {
	metrics.mu.Lock()
	metrics.dlq++
	metrics.mu.Unlock()
}

func IncConcurrencyConflict() {
	metrics.mu.Lock()
	metrics.concurrencyConflicts++
	metrics.mu.Unlock()
}

// ObserveDatabaseConflict increments the concurrency-conflict metric only for
// PostgreSQL serialization failures and deadlocks. Row-lock waiting that
// resolves normally is not a conflict and must not inflate this counter.
func ObserveDatabaseConflict(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	if pgErr.Code != "40001" && pgErr.Code != "40P01" {
		return false
	}
	IncConcurrencyConflict()
	return true
}

func IncReconciliationDivergence() {
	metrics.mu.Lock()
	metrics.reconciliationDivergences++
	metrics.mu.Unlock()
}

func ObserveProcessingLatency(duration time.Duration) {
	if duration < 0 {
		return
	}
	metrics.mu.Lock()
	metrics.processingLatencyCount++
	metrics.processingLatencySeconds += duration.Seconds()
	metrics.mu.Unlock()
}

func ObserveOutboxLag(duration time.Duration) {
	if duration < 0 {
		return
	}
	metrics.mu.Lock()
	metrics.outboxLagCount++
	metrics.outboxLagSeconds += duration.Seconds()
	metrics.mu.Unlock()
}

func MetricsHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(RenderPrometheus()))
	})
}

func RenderPrometheus() string {
	metrics.mu.RLock()
	defer metrics.mu.RUnlock()

	var b strings.Builder

	b.WriteString("# HELP jungle_wager_results_total Wager processing results by durable status.\n")
	b.WriteString("# TYPE jungle_wager_results_total counter\n")
	statuses := make([]string, 0, len(metrics.wagerResults))
	for status := range metrics.wagerResults {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)
	for _, status := range statuses {
		fmt.Fprintf(&b, "jungle_wager_results_total{status=%s} %d\n", strconv.Quote(status), metrics.wagerResults[status])
	}

	writeCounter(&b, "jungle_duplicates_total", "Idempotent or inbox duplicate deliveries.", metrics.duplicates)
	writeCounter(&b, "jungle_retries_total", "Retries scheduled or observed by asynchronous workers.", metrics.retries)
	writeCounter(&b, "jungle_dlq_total", "Messages observed at the configured terminal receive threshold.", metrics.dlq)
	writeCounter(&b, "jungle_concurrency_conflicts_total", "Database serialization or deadlock conflicts.", metrics.concurrencyConflicts)
	writeCounter(&b, "jungle_reconciliation_divergences_total", "Wallet reconciliation divergences.", metrics.reconciliationDivergences)

	b.WriteString("# HELP jungle_processing_latency_seconds Processing latency accumulated across HTTP and SQS wager execution.\n")
	b.WriteString("# TYPE jungle_processing_latency_seconds summary\n")
	fmt.Fprintf(&b, "jungle_processing_latency_seconds_count %d\n", metrics.processingLatencyCount)
	fmt.Fprintf(&b, "jungle_processing_latency_seconds_sum %s\n", formatFloat(metrics.processingLatencySeconds))

	b.WriteString("# HELP jungle_outbox_lag_seconds Delay between event occurrence and successful publication attempt.\n")
	b.WriteString("# TYPE jungle_outbox_lag_seconds summary\n")
	fmt.Fprintf(&b, "jungle_outbox_lag_seconds_count %d\n", metrics.outboxLagCount)
	fmt.Fprintf(&b, "jungle_outbox_lag_seconds_sum %s\n", formatFloat(metrics.outboxLagSeconds))

	return b.String()
}

func writeCounter(b *strings.Builder, name, help string, value uint64) {
	fmt.Fprintf(b, "# HELP %s %s\n", name, help)
	fmt.Fprintf(b, "# TYPE %s counter\n", name)
	fmt.Fprintf(b, "%s %d\n", name, value)
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 6, 64)
}

func normalizeLabel(value string) string {
	value = strings.TrimSpace(strings.ToUpper(value))
	if value == "" {
		return "UNKNOWN"
	}
	return value
}

// JSONLine is intentionally small and useful in tests/documentation to prove
// that the logger output is structured JSON without exposing credentials.
func JSONLine(fields map[string]any) ([]byte, error) {
	return json.Marshal(fields)
}
