package outboxpublisher

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	QueueURL      string
	WorkerID      string
	BatchSize     int
	PollInterval  time.Duration
	LeaseDuration time.Duration
	BaseBackoff   time.Duration
	MaxBackoff    time.Duration
}

func LoadConfig() Config {
	endpoint := envOr("SQS_ENDPOINT", "http://localhost:4566")
	return Config{
		QueueURL:      envOr("OUTBOX_QUEUE_URL", strings.TrimRight(endpoint, "/")+"/000000000000/wager-events.fifo"),
		WorkerID:      envOr("OUTBOX_WORKER_ID", defaultWorkerID()),
		BatchSize:     envInt("OUTBOX_BATCH_SIZE", 10),
		PollInterval:  time.Duration(envInt("OUTBOX_POLL_INTERVAL_MS", 250)) * time.Millisecond,
		LeaseDuration: time.Duration(envInt("OUTBOX_LEASE_SECONDS", 30)) * time.Second,
		BaseBackoff:   time.Duration(envInt("OUTBOX_BACKOFF_BASE_MS", 500)) * time.Millisecond,
		MaxBackoff:    time.Duration(envInt("OUTBOX_BACKOFF_MAX_SECONDS", 60)) * time.Second,
	}
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func defaultWorkerID() string {
	host, err := os.Hostname()
	if err != nil || strings.TrimSpace(host) == "" {
		return "outbox-publisher"
	}
	return "outbox-publisher-" + host + "-" + strconv.Itoa(os.Getpid())
}
