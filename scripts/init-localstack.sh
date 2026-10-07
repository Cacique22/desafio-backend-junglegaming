#!/bin/bash
set -eo pipefail

echo "Initializing LocalStack AWS SQS FIFO queues..."

# Create DLQ
awslocal sqs create-queue \
    --queue-name wager-transactions-dlq.fifo \
    --attributes FifoQueue=true,ContentBasedDeduplication=false

DLQ_ARN=$(awslocal sqs get-queue-attributes \
    --queue-url http://localhost:4566/000000000000/wager-transactions-dlq.fifo \
    --attribute-names QueueArn \
    --query 'Attributes.QueueArn' \
    --output text)

echo "Created DLQ with ARN: $DLQ_ARN"

# Create main FIFO queue with RedrivePolicy
REDRIVE_POLICY="{\"deadLetterTargetArn\":\"$DLQ_ARN\",\"maxReceiveCount\":\"5\"}"

awslocal sqs create-queue \
    --queue-name wager-transactions.fifo \
    --attributes FifoQueue=true,ContentBasedDeduplication=false,RedrivePolicy="$REDRIVE_POLICY"

echo "Created wager-transactions.fifo with redrive to DLQ successfully!"
