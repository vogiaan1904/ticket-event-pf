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
