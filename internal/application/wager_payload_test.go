package application

import "testing"

func TestWagerPayloadHashNormalizesTransportEquivalentPayloads(t *testing.T) {
	a := WagerPayload{
		ProviderID: "provider-a", ExternalTransactionID: "tx-1", PlayerID: "player-1",
		WalletID: "wallet-1", RoundID: "round-1", GameID: "game-1", Kind: "bet",
		Money: WagerMoneyPayload{Amount: "25.00", Currency: "brl"},
	}
	b := a
	b.Kind = " BET "
	b.Money.Currency = "BRL"

	ha, err := WagerPayloadHash(a)
	if err != nil {
		t.Fatal(err)
	}
	hb, err := WagerPayloadHash(b)
	if err != nil {
		t.Fatal(err)
	}
	if ha != hb {
		t.Fatalf("hashes differ: %s != %s", ha, hb)
	}
}
