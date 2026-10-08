#!/bin/bash
set -eo pipefail

echo "Initializing LocalStack AWS SQS FIFO queues..."

# Create DLQ for transactions
awslocal sqs create-queue \
    --queue-name wager-transactions-dlq.fifo \
    --attributes FifoQueue=true,ContentBasedDeduplication=false

# Create main FIFO queue for transactions
awslocal sqs create-queue \
    --queue-name wager-transactions.fifo \
    --attributes FifoQueue=true,ContentBasedDeduplication=false

# Configure RedrivePolicy to DLQ for transactions
python3 -c "import boto3, json; sqs = boto3.client('sqs', endpoint_url='http://localhost:4566', region_name='us-east-1', aws_access_key_id='test', aws_secret_access_key='test'); dlq_arn = sqs.get_queue_attributes(QueueUrl='http://localhost:4566/000000000000/wager-transactions-dlq.fifo', AttributeNames=['QueueArn'])['Attributes']['QueueArn']; sqs.set_queue_attributes(QueueUrl='http://localhost:4566/000000000000/wager-transactions.fifo', Attributes={'RedrivePolicy': json.dumps({'deadLetterTargetArn': dlq_arn, 'maxReceiveCount': '5'})}); print('Configured RedrivePolicy for wager-transactions successfully')"

# Create DLQ for domain events
awslocal sqs create-queue \
    --queue-name wager-events-dlq.fifo \
    --attributes FifoQueue=true,ContentBasedDeduplication=false

# Create main FIFO queue for domain events
awslocal sqs create-queue \
    --queue-name wager-events.fifo \
    --attributes FifoQueue=true,ContentBasedDeduplication=false

# Configure RedrivePolicy to DLQ for domain events
python3 -c "import boto3, json; sqs = boto3.client('sqs', endpoint_url='http://localhost:4566', region_name='us-east-1', aws_access_key_id='test', aws_secret_access_key='test'); dlq_arn = sqs.get_queue_attributes(QueueUrl='http://localhost:4566/000000000000/wager-events-dlq.fifo', AttributeNames=['QueueArn'])['Attributes']['QueueArn']; sqs.set_queue_attributes(QueueUrl='http://localhost:4566/000000000000/wager-events.fifo', Attributes={'RedrivePolicy': json.dumps({'deadLetterTargetArn': dlq_arn, 'maxReceiveCount': '5'})}); print('Configured RedrivePolicy for wager-events successfully')"

echo "All FIFO queues created with DLQ redrive successfully!"
