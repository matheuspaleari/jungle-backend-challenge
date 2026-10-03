package outboxpublisher

import (
	"encoding/json"
	"testing"
	"time"

	postgresinfra "github.com/matheuspaleari/jungle-backend-challenge/internal/infrastructure/postgres"
)

func TestNewEnvelopePreservesStableEventIdentityAndTypedData(t *testing.T) {
	occurredAt := time.Date(2026, 10, 3, 12, 0, 0, 123000000, time.UTC)
	record := postgresinfra.OutboxRecord{
		ID:            "0199-event-id",
		EventType:     "WalletBalanceChanged",
		AggregateID:   "wallet-123",
		CorrelationID: "corr-123",
		CausationID:   "tx-123",
		OccurredAt:    occurredAt,
		EventVersion:  2,
		Payload:       json.RawMessage(`{"walletId":"wallet-123","walletVersion":2}`),
	}

	envelope := NewEnvelope(record)
	if envelope.EventID != record.ID || envelope.EventType != record.EventType || envelope.Version != 2 {
		t.Fatalf("unexpected envelope identity: %+v", envelope)
	}
	if envelope.OccurredAt != occurredAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected occurredAt: %s", envelope.OccurredAt)
	}
	if envelope.CausationID == nil || *envelope.CausationID != "tx-123" {
		t.Fatalf("unexpected causationId: %#v", envelope.CausationID)
	}
	var data map[string]any
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if data["walletId"] != "wallet-123" {
		t.Fatalf("unexpected typed data: %#v", data)
	}
}

func TestBackoffIsExponentialAndCapped(t *testing.T) {
	publisher := &Publisher{cfg: Config{BaseBackoff: 500 * time.Millisecond, MaxBackoff: 5 * time.Second}}
	cases := []struct {
		attempt int
		want    time.Duration
	}{{1, 500 * time.Millisecond}, {2, time.Second}, {3, 2 * time.Second}, {4, 4 * time.Second}, {5, 5 * time.Second}, {20, 5 * time.Second}}
	for _, tc := range cases {
		if got := publisher.backoff(tc.attempt); got != tc.want {
			t.Fatalf("attempt %d: got %s want %s", tc.attempt, got, tc.want)
		}
	}
}
