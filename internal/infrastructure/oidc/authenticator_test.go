package oidc

import (
	"testing"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/platform/config"
)

func TestNewAuthenticatorRequiresOIDCConfiguration(t *testing.T) {
	_, err := NewAuthenticator(config.Config{})
	if err == nil {
		t.Fatal("expected missing OIDC configuration to fail")
	}
}
