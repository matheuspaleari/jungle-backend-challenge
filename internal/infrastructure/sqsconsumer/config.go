package sqsconsumer

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Endpoint          string
	Region            string
	QueueURL          string
	ConsumerName      string
	WaitTimeSeconds   int32
	VisibilityTimeout int32
}

func LoadConfig() Config {
	wait := int32(10)
	visibility := int32(30)
	if n, err := strconv.Atoi(os.Getenv("SQS_WAIT_TIME_SECONDS")); err == nil && n >= 0 && n <= 20 {
		wait = int32(n)
	}
	if n, err := strconv.Atoi(os.Getenv("SQS_VISIBILITY_TIMEOUT")); err == nil && n > 0 {
		visibility = int32(n)
	}

	endpoint := strings.TrimSpace(os.Getenv("SQS_ENDPOINT"))
	if endpoint == "" {
		endpoint = "http://localhost:4566"
	}
	region := strings.TrimSpace(os.Getenv("AWS_REGION"))
	if region == "" {
		region = "us-east-1"
	}
	queueURL := strings.TrimSpace(os.Getenv("SQS_QUEUE_URL"))
	if queueURL == "" {
		queueURL = endpoint + "/000000000000/wager-transactions.fifo"
	}
	name := strings.TrimSpace(os.Getenv("SQS_CONSUMER_NAME"))
	if name == "" {
		name = "wager-transactions-v1"
	}

	return Config{Endpoint: endpoint, Region: region, QueueURL: queueURL, ConsumerName: name, WaitTimeSeconds: wait, VisibilityTimeout: visibility}
}
