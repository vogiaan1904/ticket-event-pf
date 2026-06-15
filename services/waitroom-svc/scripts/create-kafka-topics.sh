#!/bin/bash

# Kafka broker address
KAFKA_BROKER="localhost:9092"

# Topics to create
TOPICS=(
  "queue.ready:10:1"
  "queue.joined:5:1"
  "queue.left:5:1"
  "checkout.completed:10:1"
  "checkout.failed:10:1"
  "checkout.expired:10:1"
  "payment.completed:10:1"
  "payment.failed:10:1"
)

echo "Creating Kafka topics..."

for topic_config in "${TOPICS[@]}"; do
  IFS=':' read -r topic partitions replication <<< "$topic_config"
  
  echo "Creating topic: $topic (partitions: $partitions, replication: $replication)"
  
  docker exec ticketbottle-kafka kafka-topics \
    --create \
    --if-not-exists \
    --bootstrap-server localhost:9092 \
    --topic "$topic" \
    --partitions "$partitions" \
    --replication-factor "$replication"
done

echo ""
echo "Listing all topics:"
docker exec ticketbottle-kafka kafka-topics \
  --list \
  --bootstrap-server localhost:9092

echo ""
echo "Topic details:"
for topic_config in "${TOPICS[@]}"; do
  IFS=':' read -r topic _ _ <<< "$topic_config"
  echo ""
  echo "Topic: $topic"
  docker exec ticketbottle-kafka kafka-topics \
    --describe \
    --bootstrap-server localhost:9092 \
    --topic "$topic"
done

echo ""
echo "Kafka topics created successfully!"