package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultConnectTimeout = 5 * time.Second
	defaultPingTimeout    = 5 * time.Second
)

var ErrDatabaseURLRequired = errors.New("database URL is required")

func NewPool(
	ctx context.Context,
	databaseURL string,
) (*pgxpool.Pool, error) {
	if databaseURL == "" {
		return nil, ErrDatabaseURLRequired
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf(
			"parse postgres configuration: %w",
			err,
		)
	}

	connectCtx, cancel := context.WithTimeout(
		ctx,
		defaultConnectTimeout,
	)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(
		connectCtx,
		config,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"create postgres pool: %w",
			err,
		)
	}

	pingCtx, pingCancel := context.WithTimeout(
		ctx,
		defaultPingTimeout,
	)
	defer pingCancel()

	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()

		return nil, fmt.Errorf(
			"ping postgres: %w",
			err,
		)
	}

	return pool, nil
}
