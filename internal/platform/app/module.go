package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.uber.org/fx"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/application"
	"github.com/matheuspaleari/jungle-backend-challenge/internal/auth"
	oidcinfra "github.com/matheuspaleari/jungle-backend-challenge/internal/infrastructure/oidc"
	"github.com/matheuspaleari/jungle-backend-challenge/internal/infrastructure/outboxpublisher"
	postgresinfra "github.com/matheuspaleari/jungle-backend-challenge/internal/infrastructure/postgres"
	"github.com/matheuspaleari/jungle-backend-challenge/internal/infrastructure/sqsconsumer"
	"github.com/matheuspaleari/jungle-backend-challenge/internal/observability"
	"github.com/matheuspaleari/jungle-backend-challenge/internal/platform/config"
	"github.com/matheuspaleari/jungle-backend-challenge/internal/platform/httpserver"
	"github.com/matheuspaleari/jungle-backend-challenge/internal/transport/httpapi"
)

const (
	pendingReferenceBatchSize    = 32
	pendingReferencePollInterval = time.Second
)

var Module = fx.Module(
	"jungle-api",
	fx.Provide(
		config.Load,
		newPostgresPool,
		postgresinfra.NewTransactionManager,
		postgresinfra.NewWalletRepository,
		postgresinfra.NewWagerRepository,
		newLedgerRepository,
		newOutboxRepository,
		postgresinfra.NewInboxRepository,
		provideApplicationIDGenerator,
		provideHTTPIDGenerator,
		provideClock,
		application.NewWagerProcessor,
		application.NewWalletService,
		newPendingReferenceWorker,
		oidcinfra.NewAuthenticator,
		provideTokenVerifier,
		httpapi.NewHandler,
		newHTTPHandler,
		httpserver.New,
		sqsconsumer.LoadConfig,
		sqsconsumer.NewClient,
		sqsconsumer.NewConsumer,
		outboxpublisher.LoadConfig,
		outboxpublisher.NewPublisher,
	),
	fx.Invoke(registerLifecycle),
)

func newPostgresPool(cfg config.Config) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return postgresinfra.NewPool(ctx, cfg.DatabaseURL)
}

func newLedgerRepository(pool *pgxpool.Pool) *postgresinfra.LedgerRepository {
	return postgresinfra.NewLedgerRepository(pool)
}

func newOutboxRepository(pool *pgxpool.Pool) *postgresinfra.OutboxRepository {
	return postgresinfra.NewOutboxRepository(pool)
}

func newPendingReferenceWorker(
	txManager *postgresinfra.TransactionManager,
	wallets *postgresinfra.WalletRepository,
	wagers *postgresinfra.WagerRepository,
	ledger *postgresinfra.LedgerRepository,
	outbox *postgresinfra.OutboxRepository,
	newID application.IDGenerator,
	now func() time.Time,
) (*application.PendingReferenceWorker, error) {
	return application.NewPendingReferenceWorker(
		txManager,
		wallets,
		wagers,
		ledger,
		outbox,
		newID,
		now,
		pendingReferenceBatchSize,
	)
}

func provideApplicationIDGenerator() application.IDGenerator { return application.IDGenerator(newUUID) }
func provideHTTPIDGenerator() func() string                  { return newUUID }
func provideClock() func() time.Time                         { return func() time.Time { return time.Now().UTC() } }
func provideTokenVerifier(authenticator *oidcinfra.Authenticator) auth.TokenVerifier {
	return authenticator
}

func newHTTPHandler(
	handler *httpapi.Handler,
	verifier auth.TokenVerifier,
	pool *pgxpool.Pool,
	sqsClient *sqs.Client,
) http.Handler {
	health := observability.NewHealthHandler(pool, sqsClient)
	business := httpapi.AuthenticationMiddleware(verifier, handler.Routes())

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", health.Live)
	mux.HandleFunc("GET /health/ready", health.Ready)
	mux.Handle("GET /metrics", observability.MetricsHandler())
	mux.Handle("/", business)
	return mux
}

func registerLifecycle(
	lifecycle fx.Lifecycle,
	pool *pgxpool.Pool,
	server *httpserver.Server,
	consumer *sqsconsumer.Consumer,
	publisher *outboxpublisher.Publisher,
	pendingReferences *application.PendingReferenceWorker,
) {
	var consumerCancel context.CancelFunc
	var publisherCancel context.CancelFunc
	var pendingReferenceCancel context.CancelFunc
	var pendingReferenceWG sync.WaitGroup

	lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := server.Start(ctx); err != nil {
				return err
			}

			consumerCtx, cancelConsumer := context.WithCancel(context.Background())
			consumerCancel = cancelConsumer
			consumer.Start(consumerCtx)

			publisherCtx, cancelPublisher := context.WithCancel(context.Background())
			publisherCancel = cancelPublisher
			publisher.Start(publisherCtx)

			pendingCtx, cancelPending := context.WithCancel(context.Background())
			pendingReferenceCancel = cancelPending
			pendingReferenceWG.Add(1)
			go func() {
				defer pendingReferenceWG.Done()
				runPendingReferenceLoop(pendingCtx, pendingReferences)
			}()

			return nil
		},
		OnStop: func(ctx context.Context) error {
			if consumerCancel != nil {
				consumerCancel()
			}
			if publisherCancel != nil {
				publisherCancel()
			}
			if pendingReferenceCancel != nil {
				pendingReferenceCancel()
			}

			if err := waitGroupWithContext(ctx, &pendingReferenceWG); err != nil {
				return fmt.Errorf("stop pending reference worker: %w", err)
			}

			consumerErr := consumer.Stop(ctx)
			publisherErr := publisher.Stop(ctx)
			serverErr := server.Stop(ctx)
			pool.Close()

			if consumerErr != nil {
				return consumerErr
			}
			if publisherErr != nil {
				return publisherErr
			}
			return serverErr
		},
	})
}

func runPendingReferenceLoop(
	ctx context.Context,
	worker *application.PendingReferenceWorker,
) {
	run := func() {
		processed, err := worker.RunOnce(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.Error(
					"pending reference worker iteration failed",
					"error", err,
				)
			}
			return
		}
		if processed > 0 {
			slog.Info(
				"pending reference worker processed batch",
				"processed", processed,
			)
		}
	}

	run()

	ticker := time.NewTicker(pendingReferencePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}

func waitGroupWithContext(ctx context.Context, wg *sync.WaitGroup) error {
	done := make(chan struct{})
	go func() {
		defer close(done)
		wg.Wait()
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func newUUID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(fmt.Sprintf("generate uuid: %v", err))
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(raw[:])
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32]
}
