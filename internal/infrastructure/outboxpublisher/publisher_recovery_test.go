package outboxpublisher

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	postgresinfra "github.com/matheuspaleari/jungle-backend-challenge/internal/infrastructure/postgres"
)

type recoverySQS struct {
	inputs []*sqs.SendMessageInput
	err    error
}

func (f *recoverySQS) SendMessage(_ context.Context, in *sqs.SendMessageInput, _ ...func(*sqs.Options)) (*sqs.SendMessageOutput, error) {
	f.inputs = append(f.inputs, in)
	if f.err != nil {
		return nil, f.err
	}
	return &sqs.SendMessageOutput{}, nil
}

type recoveryStore struct {
	record        postgresinfra.OutboxRecord
	claims        int
	markErr       error
	marked        int
	retries       int
	nextAttemptAt time.Time
}

func (s *recoveryStore) ClaimPending(context.Context, string, time.Time, time.Duration, int) ([]postgresinfra.OutboxRecord, error) {
	s.claims++
	return []postgresinfra.OutboxRecord{s.record}, nil
}
func (s *recoveryStore) ScheduleRetry(_ context.Context, _ string, _ string, next time.Time, _ string) error {
	s.retries++
	s.nextAttemptAt = next
	return nil
}
func (s *recoveryStore) MarkPublished(context.Context, string, string, time.Time) error {
	s.marked++
	return s.markErr
}

func recoveryConfig() Config {
	return Config{
		QueueURL: "http://local/queue.fifo", WorkerID: "worker-a", BatchSize: 10,
		PollInterval: time.Second, LeaseDuration: 30 * time.Second,
		BaseBackoff: 100 * time.Millisecond, MaxBackoff: time.Second,
	}
}

func recoveryRecord() postgresinfra.OutboxRecord {
	return postgresinfra.OutboxRecord{
		ID: "11111111-1111-4111-8111-111111111111", EventType: "WagerTransactionProcessed",
		AggregateID: "wallet-1", CorrelationID: "corr-1",
		OccurredAt: time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC), EventVersion: 1,
		Payload: []byte(`{"transactionId":"tx-1"}`),
	}
}

func TestPublisherPreservesEventIDWhenConfirmationFailsAndEventIsRepublished(t *testing.T) {
	client := &recoverySQS{}
	store := &recoveryStore{record: recoveryRecord(), markErr: errors.New("database unavailable after publish")}
	now := time.Date(2026, 10, 3, 12, 0, 5, 0, time.UTC)
	publisher, err := newPublisherWithStore(client, recoveryConfig(), store, func() time.Time { return now })
	if err != nil {
		t.Fatalf("new publisher: %v", err)
	}

	if _, err := publisher.PublishOnce(context.Background()); err == nil {
		t.Fatal("first PublishOnce() error = nil, want confirmation failure")
	}
	if len(client.inputs) != 1 {
		t.Fatalf("first send count=%d want=1", len(client.inputs))
	}

	// Simulate lease expiry/recovery: another cycle can claim the same durable row.
	store.markErr = nil
	if _, err := publisher.PublishOnce(context.Background()); err != nil {
		t.Fatalf("recovered PublishOnce(): %v", err)
	}
	if len(client.inputs) != 2 {
		t.Fatalf("send count=%d want=2", len(client.inputs))
	}
	for i, in := range client.inputs {
		if in.MessageDeduplicationId == nil || *in.MessageDeduplicationId != store.record.ID {
			t.Fatalf("send %d dedup id=%v want=%s", i, in.MessageDeduplicationId, store.record.ID)
		}
		if in.MessageBody == nil || !containsEventID(*in.MessageBody, store.record.ID) {
			t.Fatalf("send %d body does not preserve eventId: %v", i, in.MessageBody)
		}
	}
}

func TestPublisherSchedulesBackoffWhenSQSPublishFails(t *testing.T) {
	client := &recoverySQS{err: errors.New("temporary SQS failure")}
	store := &recoveryStore{record: recoveryRecord()}
	now := time.Date(2026, 10, 3, 12, 0, 5, 0, time.UTC)
	publisher, err := newPublisherWithStore(client, recoveryConfig(), store, func() time.Time { return now })
	if err != nil {
		t.Fatalf("new publisher: %v", err)
	}

	processed, err := publisher.PublishOnce(context.Background())
	if err != nil {
		t.Fatalf("PublishOnce(): %v", err)
	}
	if processed != 1 {
		t.Fatalf("processed=%d want=1", processed)
	}
	if store.retries != 1 {
		t.Fatalf("scheduled retries=%d want=1", store.retries)
	}
	if !store.nextAttemptAt.After(now) {
		t.Fatalf("next attempt=%s want after %s", store.nextAttemptAt, now)
	}
	if store.marked != 0 {
		t.Fatalf("marked=%d want=0", store.marked)
	}
}

func containsEventID(body, eventID string) bool {
	needle := `"eventId":"` + eventID + `"`
	for i := 0; i+len(needle) <= len(body); i++ {
		if body[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
