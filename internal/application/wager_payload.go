package application

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

type WagerMoneyPayload struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type WagerPayload struct {
	ProviderID                     string            `json:"providerId"`
	ExternalTransactionID          string            `json:"externalTransactionId"`
	PlayerID                       string            `json:"playerId"`
	WalletID                       string            `json:"walletId"`
	RoundID                        string            `json:"roundId"`
	GameID                         string            `json:"gameId"`
	Kind                           string            `json:"kind"`
	Money                          WagerMoneyPayload `json:"money"`
	ReferenceExternalTransactionID string            `json:"referenceExternalTransactionId,omitempty"`
}

func (p WagerPayload) Canonical() WagerPayload {
	p.ProviderID = strings.TrimSpace(p.ProviderID)
	p.ExternalTransactionID = strings.TrimSpace(p.ExternalTransactionID)
	p.PlayerID = strings.TrimSpace(p.PlayerID)
	p.WalletID = strings.TrimSpace(p.WalletID)
	p.RoundID = strings.TrimSpace(p.RoundID)
	p.GameID = strings.TrimSpace(p.GameID)
	p.Kind = strings.ToUpper(strings.TrimSpace(p.Kind))
	p.Money.Amount = strings.TrimSpace(p.Money.Amount)
	p.Money.Currency = strings.ToUpper(strings.TrimSpace(p.Money.Currency))
	p.ReferenceExternalTransactionID = strings.TrimSpace(p.ReferenceExternalTransactionID)
	return p
}

func WagerPayloadHash(payload WagerPayload) (string, error) {
	encoded, err := json.Marshal(payload.Canonical())
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}
