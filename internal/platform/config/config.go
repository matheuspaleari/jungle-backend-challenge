package config

import (
	"errors"
	"os"
	"strings"
)

var ErrDatabaseURLRequired = errors.New("DATABASE_URL is required")

type Config struct {
	DatabaseURL   string
	OIDCIssuerURL string
	OIDCJWKSURL   string
	OIDCAudience  string
}

func Load() (Config, error) {
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if databaseURL == "" {
		return Config{}, ErrDatabaseURLRequired
	}

	return Config{
		DatabaseURL:   databaseURL,
		OIDCIssuerURL: envOrDefault("OIDC_ISSUER_URL", "http://localhost:8081/realms/jungle"),
		OIDCJWKSURL:   envOrDefault("OIDC_JWKS_URL", "http://localhost:8081/realms/jungle/protocol/openid-connect/certs"),
		OIDCAudience:  envOrDefault("OIDC_AUDIENCE", "jungle-api"),
	}, nil
}

func envOrDefault(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}
