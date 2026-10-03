package sqsconsumer

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/application"
)

const MessageTypeWagerTransactionRequested = "WagerTransactionRequested"

type Envelope struct {
	MessageID  string      `json:"messageId"`
	Type       string      `json:"type"`
	OccurredAt time.Time   `json:"occurredAt"`
	Data       MessageData `json:"data"`
}

type MessageData struct {
	ProviderID                     string     `json:"providerId"`
	ExternalTransactionID          string     `json:"externalTransactionId"`
	IdempotencyKey                 string     `json:"idempotencyKey"`
	PlayerID                       string     `json:"playerId"`
	WalletID                       string     `json:"walletId"`
	RoundID                        string     `json:"roundId"`
	GameID                         string     `json:"gameId"`
	Kind                           string     `json:"kind"`
	Money                          WagerMoney `json:"money"`
	ReferenceExternalTransactionID string     `json:"referenceExternalTransactionId,omitempty"`
}

type WagerMoney struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

func DecodeEnvelope(body string) (Envelope, error) {
	var e Envelope
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return Envelope{}, err
	}
	if strings.TrimSpace(e.MessageID) == "" {
		return Envelope{}, errors.New("messageId is required")
	}
	if e.Type != MessageTypeWagerTransactionRequested {
		return Envelope{}, errors.New("unsupported message type")
	}
	if e.OccurredAt.IsZero() {
		return Envelope{}, errors.New("occurredAt is required")
	}
	if strings.TrimSpace(e.Data.IdempotencyKey) == "" {
		return Envelope{}, errors.New("data.idempotencyKey is required")
	}
	return e, nil
}

func (e Envelope) BusinessPayload() application.WagerPayload {
	return application.WagerPayload{
		ProviderID:                     e.Data.ProviderID,
		ExternalTransactionID:          e.Data.ExternalTransactionID,
		PlayerID:                       e.Data.PlayerID,
		WalletID:                       e.Data.WalletID,
		RoundID:                        e.Data.RoundID,
		GameID:                         e.Data.GameID,
		Kind:                           e.Data.Kind,
		Money:                          application.WagerMoneyPayload{Amount: e.Data.Money.Amount, Currency: e.Data.Money.Currency},
		ReferenceExternalTransactionID: e.Data.ReferenceExternalTransactionID,
	}
}
