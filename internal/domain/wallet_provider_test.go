package domain

import (
	"errors"
	"testing"
	"time"
)

func TestNewWalletForProviderRequiresProvider(t *testing.T) {
	money, err := NewMoney("100.00", "BRL")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	_, err = NewWalletForProvider("wallet-1", "", "player-1", money, time.Now().UTC())
	if !errors.Is(err, ErrInvalidProviderID) {
		t.Fatalf("got %v, want ErrInvalidProviderID", err)
	}
}

func TestNewWalletForProviderPersistsProviderInDomain(t *testing.T) {
	money, err := NewMoney("100.00", "BRL")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	wallet, err := NewWalletForProvider("wallet-1", "provider-a", "player-1", money, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewWalletForProvider: %v", err)
	}
	if wallet.ProviderID() != "provider-a" {
		t.Fatalf("ProviderID = %q, want provider-a", wallet.ProviderID())
	}
}
