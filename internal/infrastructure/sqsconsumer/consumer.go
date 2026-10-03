package sqsconsumer

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/jackc/pgx/v5"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/application"
	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
	postgresinfra "github.com/matheuspaleari/jungle-backend-challenge/internal/infrastructure/postgres"
	"github.com/matheuspaleari/jungle-backend-challenge/internal/observability"
)

type Consumer struct {
	client       *sqs.Client
	cfg          Config
	inbox        *postgresinfra.InboxRepository
	transactions *postgresinfra.TransactionManager
	processor    *application.WagerProcessor
	newID        application.IDGenerator
	now          func() time.Time

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func NewConsumer(client *sqs.Client, cfg Config, inbox *postgresinfra.InboxRepository, transactions *postgresinfra.TransactionManager, processor *application.WagerProcessor, newID application.IDGenerator, now func() time.Time) (*Consumer, error) {
	if client == nil || inbox == nil || transactions == nil || processor == nil || newID == nil || now == nil {
		return nil, errors.New("sqs consumer dependencies are required")
	}
	return &Consumer{client: client, cfg: cfg, inbox: inbox, transactions: transactions, processor: processor, newID: newID, now: now}, nil
}

func (c *Consumer) Start(parent context.Context) {
	ctx, cancel := context.WithCancel(parent)
	c.cancel = cancel
	c.wg.Add(1)
	go func() { defer c.wg.Done(); c.loop(ctx) }()
}

func (c *Consumer) Stop(ctx context.Context) error {
	if c.cancel != nil {
		c.cancel()
	}
	done := make(chan struct{})
	go func() { c.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Consumer) loop(ctx context.Context) {
	for ctx.Err() == nil {
		out, err := c.client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            &c.cfg.QueueURL,
			MaxNumberOfMessages: 10,
			WaitTimeSeconds:     c.cfg.WaitTimeSeconds,
			VisibilityTimeout:   c.cfg.VisibilityTimeout,
			MessageSystemAttributeNames: []types.MessageSystemAttributeName{
				types.MessageSystemAttributeNameApproximateReceiveCount,
			},
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(time.Second)
			continue
		}
		for _, message := range out.Messages {
			if ctx.Err() != nil {
				return
			}
			if err := c.handle(ctx, message); err != nil {
				observability.Error(ctx, "sqs message processing failed",
					slog.String("messageId", stringValue(message.MessageId)),
					slog.String("error", err.Error()),
				)
			}
		}
	}
}

func (c *Consumer) handle(ctx context.Context, message types.Message) error {
	if message.Body == nil || message.ReceiptHandle == nil {
		return errors.New("SQS message body/receipt handle missing")
	}

	receiveCount := approximateReceiveCount(message)
	if receiveCount > 1 {
		observability.IncRetry()
	}
	if receiveCount >= 5 {
		observability.IncDLQ()
	}

	envelope, err := DecodeEnvelope(*message.Body)
	if err != nil {
		return err
	}
	businessHash, err := application.WagerPayloadHash(envelope.BusinessPayload())
	if err != nil {
		return err
	}
	money, err := domain.NewMoney(envelope.Data.Money.Amount, envelope.Data.Money.Currency)
	if err != nil {
		return err
	}
	wager, err := domain.NewExternalWagerTransaction(domain.NewExternalWagerTransactionParams{
		ID: c.newID(), ExternalTransactionID: envelope.Data.ExternalTransactionID,
		ProviderID: envelope.Data.ProviderID, IdempotencyKey: strings.TrimSpace(envelope.Data.IdempotencyKey),
		PayloadHash: businessHash, WalletID: envelope.Data.WalletID, PlayerID: envelope.Data.PlayerID,
		RoundID: envelope.Data.RoundID, GameID: envelope.Data.GameID,
		Kind: domain.WagerKind(strings.ToUpper(strings.TrimSpace(envelope.Data.Kind))), Money: money,
		ReferenceExternalID: envelope.Data.ReferenceExternalTransactionID, CreatedAt: c.now().UTC(),
	})
	if err != nil {
		return err
	}

	started := time.Now()
	var result *application.ProcessWagerResult
	var alreadyCompleted bool

	// Challenge invariant: inbox receive/completion and every durable domain,
	// wallet, ledger and outbox write share this single SQL transaction.
	err = c.transactions.WithinTransaction(ctx, func(txCtx context.Context, tx pgx.Tx) error {
		receipt, receiveErr := c.inbox.ReceiveTx(txCtx, tx, c.newID(), c.cfg.ConsumerName, envelope.MessageID, businessHash, c.now())
		if receiveErr != nil {
			return receiveErr
		}
		if receipt.Completed {
			alreadyCompleted = true
			return nil
		}

		processed, processErr := c.processor.ProcessInTransaction(txCtx, tx, wager, envelope.MessageID)
		if processErr != nil {
			return processErr
		}
		result = processed

		// A persisted PENDING_REFERENCE is a durable completion of this input.
		// The reference worker owns continuation from this point onward.
		return c.inbox.CompleteTx(txCtx, tx, c.cfg.ConsumerName, envelope.MessageID, c.now())
	})
	observability.ObserveProcessingLatency(time.Since(started))
	if err != nil {
		observability.ObserveDatabaseConflict(err)
		return err
	}

	if alreadyCompleted {
		observability.IncDuplicate()
		observability.Info(observability.WithCorrelationID(ctx, envelope.MessageID), "sqs duplicate completed message",
			slog.String("messageId", envelope.MessageID), slog.String("providerId", envelope.Data.ProviderID),
			slog.String("walletId", envelope.Data.WalletID))
		return c.delete(ctx, *message.ReceiptHandle)
	}
	if result == nil {
		return errors.New("SQS processing committed without result")
	}
	observability.IncWagerResult(string(result.Transaction.Status()))
	if result.Replay {
		observability.IncDuplicate()
	}
	observability.Info(observability.WithCorrelationID(ctx, envelope.MessageID), "sqs wager processed",
		slog.String("messageId", envelope.MessageID), slog.String("transactionId", result.Transaction.ID()),
		slog.String("walletId", envelope.Data.WalletID), slog.String("providerId", envelope.Data.ProviderID),
		slog.String("status", string(result.Transaction.Status())))
	return c.delete(ctx, *message.ReceiptHandle)
}

func (c *Consumer) delete(ctx context.Context, receiptHandle string) error {
	_, err := c.client.DeleteMessage(ctx, &sqs.DeleteMessageInput{QueueUrl: &c.cfg.QueueURL, ReceiptHandle: &receiptHandle})
	return err
}

func approximateReceiveCount(message types.Message) int {
	raw := message.Attributes[string(types.MessageSystemAttributeNameApproximateReceiveCount)]
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 1 {
		return 1
	}
	return value
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
