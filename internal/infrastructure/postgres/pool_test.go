package postgres

import (
	"context"
	"os"
	"testing"
)

func TestNewPoolIntegration(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")

	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not configured")
	}

	ctx := context.Background()

	pool, err := NewPool(ctx, databaseURL)
	if err != nil {
		t.Fatalf("NewPool() error = %v", err)
	}
	defer pool.Close()

	var databaseName string
	var currentUser string

	err = pool.QueryRow(
		ctx,
		`
			SELECT
				current_database(),
				current_user
		`,
	).Scan(
		&databaseName,
		&currentUser,
	)
	if err != nil {
		t.Fatalf("query PostgreSQL: %v", err)
	}

	if databaseName != "jungle" {
		t.Fatalf(
			"database = %q, want %q",
			databaseName,
			"jungle",
		)
	}

	if currentUser != "jungle" {
		t.Fatalf(
			"user = %q, want %q",
			currentUser,
			"jungle",
		)
	}
}
