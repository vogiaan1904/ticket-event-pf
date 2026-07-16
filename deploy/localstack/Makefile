# Rung 1.5 — kind + host LocalStack (AWS services only). Run from deploy/localstack/.
ROOT  := $(shell cd ../.. && pwd)
NS    := ticketbottle
CTX   := kind-ticketbottle
CHART := $(ROOT)/deploy/helm/ticketbottle

.PHONY: up runtime-image lambdas overlay order-restart gate all down revert help

help:          ## list targets
	@grep -E '^[a-z-]+:.*##' $(MAKEFILE_LIST) | sed 's/:.*##/\t/'

up:            ## start host LocalStack on the kind net (needs kind cluster up + Gate-1 baseline green)
	export $$(grep LOCALSTACK_AUTH_TOKEN $(ROOT)/development/envs/.env.localstack) && \
	docker compose -f docker-compose.yml up -d

runtime-image: ## REQUIRED: pre-pull the AMD64 Lambda runtime (matches x86_64 fns + amd64 Prisma engine)
	docker pull --platform linux/amd64 public.ecr.aws/lambda/nodejs:20

lambdas:       ## deploy the 3 Lambdas + API Gateway to LocalStack
	./deploy-lambdas.sh

overlay:       ## apply the values-localstack overlay to the running kind cluster
	helm upgrade tb $(CHART) -n $(NS) -f $(CHART)/values-local.yaml -f $(CHART)/values-localstack.yaml --wait --timeout 6m

order-restart: ## restart order pods so they pick up DYNAMODB_ENDPOINT=LocalStack (ConfigMap change needs a restart)
	kubectl --context $(CTX) -n $(NS) rollout restart deploy/order-service deploy/order-consumer
	kubectl --context $(CTX) -n $(NS) rollout status deploy/order-service --timeout=120s

gate:          ## run Gate 1.5 (full purchase flow through the Lambda path)
	./gate.sh

all: up runtime-image lambdas overlay order-restart gate  ## full Rung-1.5 bring-up + gate, in order

down:          ## stop LocalStack (kind cluster untouched)
	docker compose -f docker-compose.yml down

revert:        ## back to plain kind mode (dynamodb-local + adapter), then restart order
	helm upgrade tb $(CHART) -n $(NS) -f $(CHART)/values-local.yaml --wait --timeout 6m
	kubectl --context $(CTX) -n $(NS) rollout restart deploy/order-service deploy/order-consumer
