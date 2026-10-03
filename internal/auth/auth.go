package auth

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrMissingBearerToken = errors.New("bearer token is required")
	ErrInvalidToken       = errors.New("bearer token is invalid")
	ErrProviderRequired   = errors.New("provider identity is required")
)

type Identity struct {
	Subject    string
	ProviderID string
	ClientID   string
}

type TokenVerifier interface {
	Verify(ctx context.Context, rawToken string) (Identity, error)
}

type identityKey struct{}

func WithIdentity(ctx context.Context, identity Identity) context.Context {
	return context.WithValue(ctx, identityKey{}, identity)
}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityKey{}).(Identity)
	if !ok || strings.TrimSpace(identity.ProviderID) == "" {
		return Identity{}, false
	}
	return identity, true
}
