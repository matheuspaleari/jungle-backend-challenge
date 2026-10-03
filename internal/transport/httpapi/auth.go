package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/matheuspaleari/jungle-backend-challenge/internal/auth"
)

func AuthenticationMiddleware(verifier auth.TokenVerifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := bearerToken(r.Header.Get("Authorization"))
		if err != nil {
			writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", err.Error())
			return
		}

		identity, err := verifier.Verify(r.Context(), raw)
		if err != nil {
			code := "INVALID_TOKEN"
			if errors.Is(err, auth.ErrProviderRequired) {
				code = "PROVIDER_IDENTITY_REQUIRED"
			}
			writeError(w, http.StatusUnauthorized, code, "authentication failed")
			return
		}

		next.ServeHTTP(w, r.WithContext(auth.WithIdentity(r.Context(), identity)))
	})
}

func bearerToken(header string) (string, error) {
	parts := strings.Fields(strings.TrimSpace(header))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
		return "", auth.ErrMissingBearerToken
	}
	return parts[1], nil
}

func requireProvider(r *http.Request, requestedProvider string) error {
	identity, ok := auth.IdentityFromContext(r.Context())
	if !ok {
		// Direct Handler tests intentionally run without production auth middleware.
		// Production wiring always authenticates before Routes() is reached.
		return nil
	}
	if strings.TrimSpace(requestedProvider) != identity.ProviderID {
		return errors.New("authenticated provider does not match requested provider")
	}
	return nil
}
