#!/usr/bin/env bash
set -euo pipefail
NS=ticketbottle
K="kubectl -n $NS"
echo "== Postgres: databases =="
$K exec statefulset/postgres -- psql -U root -tAc \
  "SELECT count(*) FROM pg_database WHERE datname LIKE 'ticketbottle_%';" | grep -qx 4 \
  && echo "  OK 4 app databases"
echo "== Redis: ping =="
$K exec statefulset/redis -- redis-cli ping | grep -qx PONG && echo "  OK PONG"
echo "== Redpanda: broker =="
$K exec statefulset/redpanda -- rpk cluster info --brokers localhost:9093 | grep -qi redpanda \
  && echo "  OK broker up"
echo "== DynamoDB: table ACTIVE =="
$K run ddb-smoke --rm -i --restart=Never --image=amazon/aws-cli:2.17.0 \
  --env AWS_ACCESS_KEY_ID=local --env AWS_SECRET_ACCESS_KEY=local --env AWS_REGION=us-east-1 -- \
  dynamodb describe-table --endpoint-url http://dynamodb:8000 --table-name ticketbottle-orders \
  --query 'Table.TableStatus' --output text | grep -qx ACTIVE && echo "  OK ACTIVE"
echo "== Temporal: default namespace =="
$K exec deployment/temporal -- tctl --address 127.0.0.1:7233 namespace list | grep -q "Name: default" \
  && echo "  OK default namespace"
echo "ALL INFRA SMOKE CHECKS PASSED"
