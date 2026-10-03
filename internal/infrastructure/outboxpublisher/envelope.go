package outboxpublisher

import (
	"encoding/json"
	"time"

	postgresinfra "github.com/matheuspaleari/jungle-backend-challenge/internal/infrastructure/postgres"
)

type Envelope struct {
	EventID       string          `json:"eventId"`
	EventType     string          `json:"eventType"`
	AggregateID   string          `json:"aggregateId"`
	CorrelationID string          `json:"correlationId"`
	CausationID   *string         `json:"causationId,omitempty"`
	OccurredAt    string          `json:"occurredAt"`
	Version       uint64          `json:"version"`
	Data          json.RawMessage `json:"data"`
}

func NewEnvelope(record postgresinfra.OutboxRecord) Envelope {
	var causationID *string
	if record.CausationID != "" {
		value := record.CausationID
		causationID = &value
	}
	return Envelope{
		EventID:       record.ID,
		EventType:     record.EventType,
		AggregateID:   record.AggregateID,
		CorrelationID: record.CorrelationID,
		CausationID:   causationID,
		OccurredAt:    record.OccurredAt.UTC().Format(time.RFC3339Nano),
		Version:       record.EventVersion,
		Data:          append(json.RawMessage(nil), record.Payload...),
	}
}
