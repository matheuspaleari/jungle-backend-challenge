package outboxpublisher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"

	postgresinfra "github.com/matheuspaleari/jungle-backend-challenge/internal/infrastructure/postgres"
	"github.com/matheuspaleari/jungle-backend-challenge/internal/observability"
)

type SQSAPI interface {
	SendMessage(context.Context, *sqs.SendMessageInput, ...func(*sqs.Options)) (*sqs.SendMessageOutput, error)
}

type OutboxStore interface {
	ClaimPending(context.Context, string, time.Time, time.Duration, int) ([]postgresinfra.OutboxRecord, error)
	ScheduleRetry(context.Context, string, string, time.Time, string) error
	MarkPublished(context.Context, string, string, time.Time) error
}

type Publisher struct {
	client SQSAPI
	cfg    Config
	outbox OutboxStore
	now    func() time.Time
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewPublisher(
	client *sqs.Client,
	cfg Config,
	outbox *postgresinfra.OutboxRepository,
	now func() time.Time,
) (*Publisher, error) {
	return newPublisherWithStore(client, cfg, outbox, now)
}

func newPublisherWithStore(client SQSAPI, cfg Config, outbox OutboxStore, now func() time.Time) (*Publisher, error) {
	if client == nil || outbox == nil || now == nil {
		return nil, errors.New("outbox publisher dependencies are required")
	}
	if cfg.QueueURL == "" || cfg.WorkerID == "" || cfg.BatchSize <= 0 ||
		cfg.PollInterval <= 0 || cfg.LeaseDuration <= 0 ||
		cfg.BaseBackoff <= 0 || cfg.MaxBackoff <= 0 {
		return nil, errors.New("invalid outbox publisher config")
	}
	return &Publisher{client: client, cfg: cfg, outbox: outbox, now: now}, nil
}

func (p *Publisher) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	p.cancel = cancel
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.loop(ctx)
	}()
}

func (p *Publisher) Stop(ctx context.Context) error {
	if p.cancel != nil {
		p.cancel()
	}
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (p *Publisher) loop(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()

	for {
		if ctx.Err() != nil {
			return
		}
		processed, err := p.PublishOnce(ctx)
		if err != nil {
			observability.Error(ctx, "outbox publisher cycle failed",
				slog.String("workerId", p.cfg.WorkerID),
				slog.String("error", err.Error()),
			)
		}
		if processed > 0 {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (p *Publisher) PublishOnce(ctx context.Context) (int, error) {
	now := p.now().UTC()
	records, err := p.outbox.ClaimPending(ctx, p.cfg.WorkerID, now, p.cfg.LeaseDuration, p.cfg.BatchSize)
	if err != nil {
		return 0, err
	}

	processed := 0
	for _, record := range records {
		if ctx.Err() != nil {
			return processed, ctx.Err()
		}
		if err := p.publishRecord(ctx, record); err != nil {
			observability.IncRetry()
			next := p.now().UTC().Add(p.backoff(record.RetryCount + 1))
			if retryErr := p.outbox.ScheduleRetry(ctx, record.ID, p.cfg.WorkerID, next, err.Error()); retryErr != nil {
				return processed, fmt.Errorf("publish event %s: %v; schedule retry: %w", record.ID, err, retryErr)
			}
			processed++
			continue
		}
		publishedAt := p.now().UTC()
		observability.ObserveOutboxLag(publishedAt.Sub(record.OccurredAt.UTC()))
		observability.Info(ctx, "outbox event published",
			slog.String("eventId", record.ID),
			slog.String("aggregateId", record.AggregateID),
			slog.String("workerId", p.cfg.WorkerID),
		)
		if err := p.outbox.MarkPublished(ctx, record.ID, p.cfg.WorkerID, publishedAt); err != nil {
			// If SQS accepted the event but this confirmation fails, the lease expires
			// and another publisher republishes the same stable eventId. Consumers must
			// therefore deduplicate integration events by eventId.
			return processed, fmt.Errorf("confirm outbox event %s: %w", record.ID, err)
		}
		processed++
	}
	return processed, nil
}

func (p *Publisher) publishRecord(ctx context.Context, record postgresinfra.OutboxRecord) error {
	envelope := NewEnvelope(record)
	body, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("marshal outbox envelope: %w", err)
	}

	groupID := record.AggregateID
	deduplicationID := record.ID
	_, err = p.client.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:               &p.cfg.QueueURL,
		MessageBody:            stringPtr(string(body)),
		MessageGroupId:         &groupID,
		MessageDeduplicationId: &deduplicationID,
	})
	if err != nil {
		return fmt.Errorf("send outbox event to SQS: %w", err)
	}
	return nil
}

func (p *Publisher) backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	exponent := attempt - 1
	if exponent > 20 {
		exponent = 20
	}
	factor := math.Pow(2, float64(exponent))
	delay := time.Duration(float64(p.cfg.BaseBackoff) * factor)
	if delay > p.cfg.MaxBackoff || delay < 0 {
		return p.cfg.MaxBackoff
	}
	return delay
}

func stringPtr(value string) *string { return &value }
