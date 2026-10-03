package httpapi

import "github.com/matheuspaleari/jungle-backend-challenge/internal/application"

func wagerPayloadHash(request wagerRequest) (string, error) {
	return application.WagerPayloadHash(application.WagerPayload{
		ProviderID:            request.ProviderID,
		ExternalTransactionID: request.ExternalTransactionID,
		PlayerID:              request.PlayerID,
		WalletID:              request.WalletID,
		RoundID:               request.RoundID,
		GameID:                request.GameID,
		Kind:                  request.Kind,
		Money: application.WagerMoneyPayload{
			Amount:   request.Money.Amount,
			Currency: request.Money.Currency,
		},
		ReferenceExternalTransactionID: request.ReferenceExternalTransactionID,
	})
}
