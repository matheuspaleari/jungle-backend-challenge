package httpapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/auth"
)

type fakeVerifier struct {
	identity auth.Identity
	err      error
}

func (f fakeVerifier) Verify(context.Context, string) (auth.Identity, error) {
	return f.identity, f.err
}

func TestAuthenticationMiddlewareRejectsMissingBearerToken(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next must not run") })
	h := AuthenticationMiddleware(fakeVerifier{}, next)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/wallets/x", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rr.Code)
	}
}

func TestAuthenticationMiddlewareRejectsInvalidToken(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next must not run") })
	h := AuthenticationMiddleware(fakeVerifier{err: auth.ErrInvalidToken}, next)
	req := httptest.NewRequest(http.MethodGet, "/wallets/x", nil)
	req.Header.Set("Authorization", "Bearer bad")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rr.Code)
	}
}

func TestAuthenticationMiddlewareInjectsIdentity(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, ok := auth.IdentityFromContext(r.Context())
		if !ok || identity.ProviderID != "provider-a" {
			t.Fatal("provider identity missing")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	h := AuthenticationMiddleware(fakeVerifier{identity: auth.Identity{Subject: "service-account-provider-a", ProviderID: "provider-a", ClientID: "provider-a"}}, next)
	req := httptest.NewRequest(http.MethodGet, "/wallets/x", nil)
	req.Header.Set("Authorization", "Bearer valid")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("got %d, want 204", rr.Code)
	}
}

func TestAuthenticationMiddlewareRejectsMissingProviderClaim(t *testing.T) {
	next := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("next must not run") })
	h := AuthenticationMiddleware(fakeVerifier{err: auth.ErrProviderRequired}, next)
	req := httptest.NewRequest(http.MethodGet, "/wallets/x", nil)
	req.Header.Set("Authorization", "Bearer valid")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rr.Code)
	}
	if !errors.Is(auth.ErrProviderRequired, auth.ErrProviderRequired) {
		t.Fatal("sanity")
	}
}
