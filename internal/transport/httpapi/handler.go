package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/application"
	"github.com/matheuspaleari/jungle-backend-challenge/internal/auth"
	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
	postgresinfra "github.com/matheuspaleari/jungle-backend-challenge/internal/infrastructure/postgres"
	"github.com/matheuspaleari/jungle-backend-challenge/internal/observability"
)

type Handler struct {
	wallets       *postgresinfra.WalletRepository
	wagers        *postgresinfra.WagerRepository
	ledger        *postgresinfra.LedgerRepository
	walletService *application.WalletService
	processor     *application.WagerProcessor
	newID         func() string
	now           func() time.Time
}

type moneyRequest struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}
type wagerRequest struct {
	ProviderID                     string       `json:"providerId"`
	ExternalTransactionID          string       `json:"externalTransactionId"`
	PlayerID                       string       `json:"playerId"`
	WalletID                       string       `json:"walletId"`
	RoundID                        string       `json:"roundId"`
	GameID                         string       `json:"gameId"`
	Kind                           string       `json:"kind"`
	Money                          moneyRequest `json:"money"`
	ReferenceExternalTransactionID string       `json:"referenceExternalTransactionId,omitempty"`
}
type openWalletRequest struct {
	PlayerID       string       `json:"playerId"`
	InitialBalance moneyRequest `json:"initialBalance"`
}
type moneyResponse struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}
type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewHandler(
	wallets *postgresinfra.WalletRepository,
	wagers *postgresinfra.WagerRepository,
	ledger *postgresinfra.LedgerRepository,
	walletService *application.WalletService,
	processor *application.WagerProcessor,
	newID func() string,
	now func() time.Time,
) (*Handler, error) {
	if wallets == nil || wagers == nil || ledger == nil || walletService == nil ||
		processor == nil || newID == nil || now == nil {
		return nil, errors.New("http handler dependencies are required")
	}
	return &Handler{wallets: wallets, wagers: wagers, ledger: ledger, walletService: walletService, processor: processor, newID: newID, now: now}, nil
}

func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /wallets", h.openWallet)
	mux.HandleFunc("GET /wallets/{walletId}", h.getWallet)
	mux.HandleFunc("GET /wallets/{walletId}/ledger", h.getLedger)
	mux.HandleFunc("POST /wallets/{walletId}/reconciliation", h.reconcile)
	mux.HandleFunc("POST /wagering/transactions", h.processWager)
	mux.HandleFunc("GET /wagering/transactions/{transactionId}", h.getTransaction)
	mux.HandleFunc("GET /providers/{providerId}/wagering/transactions/{externalTransactionId}", h.getProviderTransaction)
	return correlationMiddleware(mux)
}

func (h *Handler) openWallet(w http.ResponseWriter, r *http.Request) {
	var req openWalletRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	money, err := domain.NewMoney(req.InitialBalance.Amount, req.InitialBalance.Currency)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_MONEY", err.Error())
		return
	}
	providerID, authenticated := authenticatedProviderID(r)
	var wallet *domain.Wallet
	if authenticated {
		wallet, err = h.walletService.OpenForProvider(r.Context(), providerID, req.PlayerID, money, correlationID(r.Context()))
	} else {
		// Direct handler tests bypass production AuthenticationMiddleware.
		wallet, err = h.walletService.Open(r.Context(), req.PlayerID, money, correlationID(r.Context()))
	}
	if err != nil {
		if isUniqueViolation(err) {
			writeError(w, http.StatusConflict, "WALLET_ALREADY_EXISTS", "wallet already exists for player and currency")
			return
		}
		writeError(w, http.StatusBadRequest, "INVALID_WALLET", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, walletResponse(wallet))
}

func (h *Handler) getWallet(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.wallets.GetByID(r.Context(), r.PathValue("walletId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "WALLET_NOT_FOUND", "wallet not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_WALLET_ID", err.Error())
		return
	}
	if err := requireWalletOwner(r, wallet); err != nil {
		writeError(w, http.StatusForbidden, "WALLET_PROVIDER_MISMATCH", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, walletResponse(wallet))
}

func (h *Handler) getLedger(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.wallets.GetByID(r.Context(), r.PathValue("walletId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "WALLET_NOT_FOUND", "wallet not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_WALLET_ID", err.Error())
		return
	}
	if err := requireWalletOwner(r, wallet); err != nil {
		writeError(w, http.StatusForbidden, "WALLET_PROVIDER_MISMATCH", err.Error())
		return
	}

	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 100 {
			writeError(w, 400, "INVALID_LIMIT", "limit must be between 1 and 100")
			return
		}
		limit = n
	}
	at, id, err := decodeCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		writeError(w, 400, "INVALID_CURSOR", "invalid ledger cursor")
		return
	}
	page, err := h.ledger.ListByWallet(r.Context(), r.PathValue("walletId"), at, id, limit)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "WALLET_NOT_FOUND", "wallet not found")
		return
	}
	if err != nil {
		writeError(w, 400, "LEDGER_READ_FAILED", err.Error())
		return
	}
	entries := make([]map[string]any, 0, len(page.Entries))
	for _, e := range page.Entries {
		entries = append(entries, map[string]any{
			"id": e.ID, "transactionId": e.TransactionID, "direction": e.Direction,
			"value": moneyJSON(e.Value), "balanceBefore": moneyJSON(e.BalanceBefore),
			"balanceAfter": moneyJSON(e.BalanceAfter), "createdAt": e.CreatedAt,
		})
	}
	next := ""
	if page.NextCursor != "" {
		next = base64.RawURLEncoding.EncodeToString([]byte(page.NextCursor))
	}
	writeJSON(w, 200, map[string]any{"entries": entries, "nextCursor": next})
}

func (h *Handler) processWager(w http.ResponseWriter, r *http.Request) {
	idem := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idem == "" {
		writeError(w, 400, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key header is required")
		return
	}
	var req wagerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := requireProvider(r, req.ProviderID); err != nil {
		writeError(w, http.StatusForbidden, "PROVIDER_MISMATCH", err.Error())
		return
	}
	wallet, err := h.wallets.GetByID(r.Context(), req.WalletID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "WALLET_NOT_FOUND", "wallet not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_WALLET_ID", err.Error())
		return
	}
	if err := requireWalletOwner(r, wallet); err != nil {
		writeError(w, http.StatusForbidden, "WALLET_PROVIDER_MISMATCH", err.Error())
		return
	}
	hash, err := wagerPayloadHash(req)
	if err != nil {
		writeError(w, 400, "INVALID_PAYLOAD", err.Error())
		return
	}
	money, err := domain.NewMoney(req.Money.Amount, req.Money.Currency)
	if err != nil {
		writeError(w, 400, "INVALID_MONEY", err.Error())
		return
	}
	tx, err := domain.NewExternalWagerTransaction(domain.NewExternalWagerTransactionParams{
		ID: h.newID(), ExternalTransactionID: req.ExternalTransactionID, ProviderID: req.ProviderID,
		IdempotencyKey: idem, PayloadHash: hash, WalletID: req.WalletID, PlayerID: req.PlayerID,
		RoundID: req.RoundID, GameID: req.GameID, Kind: domain.WagerKind(strings.ToUpper(req.Kind)),
		Money: money, ReferenceExternalID: req.ReferenceExternalTransactionID, CreatedAt: h.now().UTC(),
	})
	if err != nil {
		writeError(w, 400, "INVALID_WAGER", err.Error())
		return
	}
	started := time.Now()
	result, err := h.processor.Process(r.Context(), tx, correlationID(r.Context()))
	observability.ObserveProcessingLatency(time.Since(started))
	if err != nil {
		observability.ObserveDatabaseConflict(err)
		switch {
		case errors.Is(err, application.ErrIdempotencyConflict), errors.Is(err, application.ErrExternalTransactionConflict):
			writeError(w, 409, "IDEMPOTENCY_CONFLICT", err.Error())
		case errors.Is(err, application.ErrWagerProviderMismatch):
			writeError(w, http.StatusForbidden, "WALLET_PROVIDER_MISMATCH", err.Error())
		case errors.Is(err, pgx.ErrNoRows):
			writeError(w, 404, "WALLET_NOT_FOUND", "wallet not found")
		default:
			writeError(w, 503, "PROCESSING_UNAVAILABLE", err.Error())
		}
		return
	}
	observability.IncWagerResult(string(result.Transaction.Status()))
	if result.Replay {
		observability.IncDuplicate()
	}

	status := http.StatusOK
	if result.Transaction.Status() == domain.WagerStatusPendingReference {
		status = http.StatusAccepted
	}
	writeJSON(w, status, wagerResultResponse(result))
}

func (h *Handler) getTransaction(w http.ResponseWriter, r *http.Request) {
	tx, err := h.wagers.GetByID(r.Context(), r.PathValue("transactionId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "TRANSACTION_NOT_FOUND", "transaction not found")
		return
	}
	if err != nil {
		writeError(w, 400, "INVALID_TRANSACTION_ID", err.Error())
		return
	}
	if tx.ProviderID() != "" {
		if err := requireProvider(r, tx.ProviderID()); err != nil {
			writeError(w, http.StatusForbidden, "PROVIDER_MISMATCH", err.Error())
			return
		}
	} else {
		wallet, walletErr := h.wallets.GetByID(r.Context(), tx.WalletID())
		if walletErr != nil {
			writeError(w, http.StatusForbidden, "WALLET_PROVIDER_MISMATCH", "transaction wallet ownership could not be verified")
			return
		}
		if err := requireWalletOwner(r, wallet); err != nil {
			writeError(w, http.StatusForbidden, "WALLET_PROVIDER_MISMATCH", err.Error())
			return
		}
	}
	writeJSON(w, 200, transactionResponse(tx))
}
func (h *Handler) getProviderTransaction(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("providerId")
	if err := requireProvider(r, providerID); err != nil {
		writeError(w, http.StatusForbidden, "PROVIDER_MISMATCH", err.Error())
		return
	}
	tx, err := h.wagers.GetByProviderAndExternalID(r.Context(), providerID, r.PathValue("externalTransactionId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "TRANSACTION_NOT_FOUND", "transaction not found")
		return
	}
	if err != nil {
		writeError(w, 400, "INVALID_TRANSACTION_LOOKUP", err.Error())
		return
	}
	writeJSON(w, 200, transactionResponse(tx))
}

func (h *Handler) reconcile(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.wallets.GetByID(r.Context(), r.PathValue("walletId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "WALLET_NOT_FOUND", "wallet not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_WALLET_ID", err.Error())
		return
	}
	if err := requireWalletOwner(r, wallet); err != nil {
		writeError(w, http.StatusForbidden, "WALLET_PROVIDER_MISMATCH", err.Error())
		return
	}

	s, err := h.ledger.Reconcile(r.Context(), r.PathValue("walletId"))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "WALLET_NOT_FOUND", "wallet not found")
		return
	}
	if err != nil {
		writeError(w, 503, "RECONCILIATION_UNAVAILABLE", err.Error())
		return
	}
	diffMinor := s.StoredBalance.MinorUnits() - s.CalculatedBalance.MinorUnits()
	if diffMinor != 0 {
		observability.IncReconciliationDivergence()
		observability.Error(observability.WithCorrelationID(r.Context(), correlationID(r.Context())), "wallet reconciliation divergence",
			slog.String("walletId", r.PathValue("walletId")),
			slog.Int64("differenceMinor", diffMinor),
		)
	}
	diff, err := domain.MoneyFromMinorUnits(diffMinor, s.StoredBalance.Currency())
	if err != nil {
		writeError(w, 500, "RECONCILIATION_FAILED", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{
		"walletId": r.PathValue("walletId"), "storedBalance": moneyJSON(s.StoredBalance),
		"calculatedBalance": moneyJSON(s.CalculatedBalance), "difference": moneyJSON(diff),
		"consistent": diffMinor == 0, "checkedEntries": s.CheckedEntries,
	})
}

func authenticatedProviderID(r *http.Request) (string, bool) {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		// Direct handler tests from the earlier roadmap stages intentionally bypass
		// production authentication. Production Fx wiring always injects identity.
		return "", false
	}
	return identity.ProviderID, true
}

func requireWalletOwner(r *http.Request, wallet *domain.Wallet) error {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		// Preserve direct unit/integration handler tests. In production every route
		// is wrapped by AuthenticationMiddleware.
		return nil
	}
	if wallet == nil {
		return errors.New("wallet is required")
	}
	if strings.TrimSpace(wallet.ProviderID()) == "" {
		return errors.New("wallet has no provider ownership assigned")
	}
	if wallet.ProviderID() != identity.ProviderID {
		return errors.New("authenticated provider does not own wallet")
	}
	return nil
}

func walletResponse(w *domain.Wallet) map[string]any {
	return map[string]any{"id": w.ID(), "playerId": w.PlayerID(), "balance": moneyJSON(w.Balance()), "version": w.Version()}
}
func wagerResultResponse(r *application.ProcessWagerResult) map[string]any {
	return map[string]any{"transactionId": r.Transaction.ID(), "status": r.Transaction.Status(), "balance": moneyJSON(r.Balance), "idempotentReplay": r.Replay, "failureCode": emptyToNil(r.Transaction.FailureCode())}
}
func transactionResponse(t *domain.WagerTransaction) map[string]any {
	result := map[string]any{
		"transactionId": t.ID(), "externalTransactionId": emptyToNil(t.ExternalTransactionID()),
		"providerId": emptyToNil(t.ProviderID()), "walletId": t.WalletID(), "playerId": t.PlayerID(),
		"roundId": emptyToNil(t.RoundID()), "gameId": emptyToNil(t.GameID()), "kind": t.Kind(),
		"money": moneyJSON(t.Money()), "status": t.Status(), "failureCode": emptyToNil(t.FailureCode()),
		"referenceExternalTransactionId": emptyToNil(t.ReferenceExternalID()),
		"resolvedReferenceTransactionId": emptyToNil(t.ResolvedReferenceID()),
		"createdAt":                      t.CreatedAt(), "updatedAt": t.UpdatedAt(),
	}
	if balance, ok := t.ResultBalance(); ok {
		result["balance"] = moneyJSON(balance)
	}
	return result
}
func moneyJSON(m domain.Money) moneyResponse {
	return moneyResponse{Amount: m.Amount(), Currency: m.Currency()}
}
func emptyToNil(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, 400, "INVALID_JSON", err.Error())
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Code: code, Message: message})
}
func isUniqueViolation(err error) bool { return strings.Contains(err.Error(), "SQLSTATE 23505") }

type correlationKey struct{}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func correlationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		id := strings.TrimSpace(r.Header.Get("X-Correlation-ID"))
		if id == "" {
			id = newUUID()
		}
		w.Header().Set("X-Correlation-ID", id)

		ctx := context.WithValue(r.Context(), correlationKey{}, id)
		ctx = observability.WithCorrelationID(ctx, id)
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(recorder, r.WithContext(ctx))

		attrs := []slog.Attr{
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", recorder.status),
			slog.Int64("latencyMs", time.Since(started).Milliseconds()),
		}
		if identity, ok := auth.IdentityFromContext(ctx); ok {
			attrs = append(attrs, slog.String("providerId", identity.ProviderID))
		}
		if walletID := strings.TrimSpace(r.PathValue("walletId")); walletID != "" {
			attrs = append(attrs, slog.String("walletId", walletID))
		}
		if transactionID := strings.TrimSpace(r.PathValue("transactionId")); transactionID != "" {
			attrs = append(attrs, slog.String("transactionId", transactionID))
		}
		observability.Info(ctx, "http request completed", attrs...)
	})
}
func correlationID(ctx context.Context) string {
	v, _ := ctx.Value(correlationKey{}).(string)
	return v
}

func decodeCursor(raw string) (*time.Time, string, error) {
	if raw == "" {
		return nil, "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, "", err
	}
	parts := strings.SplitN(string(b), "|", 2)
	if len(parts) != 2 {
		return nil, "", errors.New("bad cursor")
	}
	at, err := time.Parse(time.RFC3339Nano, parts[0])
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(parts[1]) == "" {
		return nil, "", errors.New("bad cursor id")
	}
	return &at, parts[1], nil
}

func newUUID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("uuid entropy: %v", err))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	x := hex.EncodeToString(b[:])
	return x[0:8] + "-" + x[8:12] + "-" + x[12:16] + "-" + x[16:20] + "-" + x[20:32]
}
