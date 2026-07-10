# TicketBottle — local Kubernetes (Rung 1) operations.
CLUSTER := ticketbottle
CTX     := kind-$(CLUSTER)
NS      := ticketbottle

.PHONY: cluster-up cluster-down cluster-status

cluster-up:            ## Create the local kind cluster
	kind create cluster --config kind/cluster.yaml
	kubectl --context $(CTX) create namespace $(NS) --dry-run=client -o yaml | kubectl --context $(CTX) apply -f -

cluster-down:          ## Delete the local kind cluster
	kind delete cluster --name $(CLUSTER)

cluster-status:        ## Show nodes and namespaces
	kubectl --context $(CTX) get nodes
	kubectl --context $(CTX) get ns $(NS)

CHART := helm/ticketbottle
RELEASE := tb
LOCAL_VALUES := $(CHART)/values-local.yaml

.PHONY: infra-up infra-down smoke

infra-up:              ## Install/upgrade the infra tier on the local cluster
	helm upgrade --install $(RELEASE) $(CHART) -n $(NS) --create-namespace -f $(LOCAL_VALUES) --wait --timeout 6m

infra-down:            ## Uninstall the chart (keeps the cluster)
	helm uninstall $(RELEASE) -n $(NS) || true

smoke:                 ## Verify every infra component end-to-end
	./scripts/smoke-infra.sh

.PHONY: apps-up smoke-apps

apps-up:               ## Build+load images and deploy the app tier
	./scripts/build-images.sh
	helm upgrade --install $(RELEASE) $(CHART) -n $(NS) --create-namespace -f $(LOCAL_VALUES) --wait --timeout 8m

smoke-apps:            ## Verify the app tier (pods Ready + signup)
	./scripts/smoke-apps.sh
