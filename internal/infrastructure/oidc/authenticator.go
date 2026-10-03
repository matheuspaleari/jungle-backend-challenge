package oidc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/auth"
	"github.com/matheuspaleari/jungle-backend-challenge/internal/platform/config"
)

type Authenticator struct {
	verifier *coreoidc.IDTokenVerifier
}

type tokenClaims struct {
	AuthorizedParty string `json:"azp"`
	ClientID        string `json:"client_id"`
	ProviderID      string `json:"provider_id"`
}

func NewAuthenticator(cfg config.Config) (*Authenticator, error) {
	issuer := strings.TrimSpace(cfg.OIDCIssuerURL)
	audience := strings.TrimSpace(cfg.OIDCAudience)
	jwksURL := strings.TrimSpace(cfg.OIDCJWKSURL)
	if issuer == "" || audience == "" || jwksURL == "" {
		return nil, errors.New("OIDC issuer, audience and JWKS URL are required")
	}

	keySet := coreoidc.NewRemoteKeySet(context.Background(), jwksURL)
	verifier := coreoidc.NewVerifier(issuer, keySet, &coreoidc.Config{
		ClientID:             audience,
		SupportedSigningAlgs: []string{coreoidc.RS256},
	})
	return &Authenticator{verifier: verifier}, nil
}

func (a *Authenticator) Verify(ctx context.Context, rawToken string) (auth.Identity, error) {
	if a == nil || a.verifier == nil {
		return auth.Identity{}, auth.ErrInvalidToken
	}
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return auth.Identity{}, auth.ErrMissingBearerToken
	}

	token, err := a.verifier.Verify(ctx, rawToken)
	if err != nil {
		return auth.Identity{}, fmt.Errorf("%w: %v", auth.ErrInvalidToken, err)
	}

	var claims tokenClaims
	if err := token.Claims(&claims); err != nil {
		return auth.Identity{}, fmt.Errorf("%w: decode claims: %v", auth.ErrInvalidToken, err)
	}

	providerID := strings.TrimSpace(claims.ProviderID)
	if providerID == "" {
		return auth.Identity{}, auth.ErrProviderRequired
	}
	clientID := strings.TrimSpace(claims.AuthorizedParty)
	if clientID == "" {
		clientID = strings.TrimSpace(claims.ClientID)
	}

	return auth.Identity{Subject: token.Subject, ProviderID: providerID, ClientID: clientID}, nil
}
