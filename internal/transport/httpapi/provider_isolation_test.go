package httpapi

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/auth"
	"github.com/matheuspaleari/jungle-backend-challenge/internal/domain"
)

func TestRequireWalletOwnerAllowsMatchingProvider(t *testing.T) {
	wallet := testProviderWallet(t, "provider-a")
	req := httptest.NewRequest("GET", "/wallets/wallet-1", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{ProviderID: "provider-a"}))
	if err := requireWalletOwner(req, wallet); err != nil {
		t.Fatalf("requireWalletOwner: %v", err)
	}
}

func TestRequireWalletOwnerRejectsDifferentProvider(t *testing.T) {
	wallet := testProviderWallet(t, "provider-a")
	req := httptest.NewRequest("GET", "/wallets/wallet-1", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{ProviderID: "provider-b"}))
	if err := requireWalletOwner(req, wallet); err == nil {
		t.Fatal("expected provider mismatch")
	}
}

func TestRequireWalletOwnerRejectsUnassignedLegacyWalletWhenAuthenticated(t *testing.T) {
	money, err := domain.NewMoney("100.00", "BRL")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	wallet, err := domain.NewWallet("wallet-legacy", "player-legacy", money, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewWallet: %v", err)
	}
	req := httptest.NewRequest("GET", "/wallets/wallet-legacy", nil)
	req = req.WithContext(auth.WithIdentity(req.Context(), auth.Identity{ProviderID: "provider-a"}))
	if err := requireWalletOwner(req, wallet); err == nil {
		t.Fatal("expected unassigned legacy wallet to be denied")
	}
}

func testProviderWallet(t *testing.T, providerID string) *domain.Wallet {
	t.Helper()
	money, err := domain.NewMoney("100.00", "BRL")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	wallet, err := domain.NewWalletForProvider("wallet-1", providerID, "player-1", money, time.Now().UTC())
	if err != nil {
		t.Fatalf("NewWalletForProvider: %v", err)
	}
	return wallet
}
