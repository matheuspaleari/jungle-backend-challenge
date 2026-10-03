package httpapi

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestWagerPayloadHashIsDeterministicAndIgnoresTransportMetadata(t *testing.T) {
	req := wagerRequest{
		ProviderID: "provider-a", ExternalTransactionID: "tx-1", PlayerID: "player-1",
		WalletID: "wallet-1", RoundID: "round-1", GameID: "game-1", Kind: "BET",
		Money: moneyRequest{Amount: "25.00", Currency: "BRL"},
	}
	first, err := wagerPayloadHash(req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := wagerPayloadHash(req)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("hash changed: %s != %s", first, second)
	}
	if len(first) != 64 {
		t.Fatalf("sha256 hex length=%d, want 64", len(first))
	}
}

func TestLedgerCursorRoundTrip(t *testing.T) {
	at := time.Date(2026, 10, 2, 12, 30, 0, 123, time.UTC)
	id := "0192f291-27dd-7d3f-8071-5f8685deef37"
	raw := base64.RawURLEncoding.EncodeToString([]byte(at.Format(time.RFC3339Nano) + "|" + id))
	gotAt, gotID, err := decodeCursor(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !gotAt.Equal(at) || gotID != id {
		t.Fatalf("cursor roundtrip got %v %s", gotAt, gotID)
	}
}
