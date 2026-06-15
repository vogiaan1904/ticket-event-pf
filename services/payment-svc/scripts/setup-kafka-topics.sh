#!/bin/bash

# Kafka Topics Setup Script
# Run this script to create all required Kafka topics

KAFKA_BROKER="localhost:9092"

echo "Creating Kafka topics for Payment Service..."

# Payment Events Topic
kafka-topics.sh --create \
  --bootstrap-server $KAFKA_BROKER \
  --topic payment-events \
  --partitions 3 \
  --replication-factor 1 \
  --config retention.ms=604800000 \
  --config cleanup.policy=delete \
  --config compression.type=gzip \
  --if-not-exists

echo "✅ Topics created successfully!"

# List all topics
echo ""
echo "Current topics:"
kafka-topics.sh --list --bootstrap-server $KAFKA_BROKER
