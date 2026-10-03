#!/bin/sh
set -eu

REGION="${AWS_DEFAULT_REGION:-us-east-1}"

create_fifo_pair() {
  QUEUE_NAME="$1"
  DLQ_NAME="$2"

  echo "Creating SQS DLQ: ${DLQ_NAME}"
  awslocal sqs create-queue \
    --region "${REGION}" \
    --queue-name "${DLQ_NAME}" \
    --attributes '{"FifoQueue":"true","ContentBasedDeduplication":"false"}' >/dev/null

  DLQ_URL="$(awslocal sqs get-queue-url --region "${REGION}" --queue-name "${DLQ_NAME}" --query 'QueueUrl' --output text)"
  DLQ_ARN="$(awslocal sqs get-queue-attributes --region "${REGION}" --queue-url "${DLQ_URL}" --attribute-names QueueArn --query 'Attributes.QueueArn' --output text)"

  ATTRIBUTES_FILE="/tmp/${QUEUE_NAME}.json"
  cat > "${ATTRIBUTES_FILE}" <<JSON
{
  "FifoQueue": "true",
  "ContentBasedDeduplication": "false",
  "VisibilityTimeout": "30",
  "ReceiveMessageWaitTimeSeconds": "10",
  "RedrivePolicy": "{\"deadLetterTargetArn\":\"${DLQ_ARN}\",\"maxReceiveCount\":\"5\"}"
}
JSON

  echo "Creating SQS queue: ${QUEUE_NAME}"
  awslocal sqs create-queue \
    --region "${REGION}" \
    --queue-name "${QUEUE_NAME}" \
    --attributes "file://${ATTRIBUTES_FILE}" >/dev/null
}

create_fifo_pair "wager-transactions.fifo" "wager-transactions-dlq.fifo"
create_fifo_pair "wager-events.fifo" "wager-events-dlq.fifo"

echo "SQS queues ready:"
awslocal sqs list-queues --region "${REGION}"
