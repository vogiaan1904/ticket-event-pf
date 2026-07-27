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

infra-up:              ## Install/upgrade the infra tier on the local cluster (no app pods)
	./scripts/build-migrate-images.sh
	helm upgrade --install $(RELEASE) $(CHART) -n $(NS) --create-namespace -f $(LOCAL_VALUES) --set apps.enabled=false --wait --timeout 6m

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

.PHONY: gate1

gate1:                 ## Run the full purchase-flow acceptance test (Phase 0B-2)
	./scripts/gate1-purchase-flow.sh



# --- Rung 2: k3s on EC2 -------------------------------------------------------
K3S_ENV := terraform/envs/k3s
KUBECONFIG_FILE := /tmp/k3s.yaml

.PHONY: k3s-ip k3s-instance stop start tunnel k3s-kubeconfig k3s-deploy k3s-gate2

k3s-ip:            ## Print the current public IP of the k3s box
	@cd $(K3S_ENV) && terraform output -raw public_ip

k3s-instance:      ## Print the instance id
	@cd $(K3S_ENV) && terraform output -raw instance_id

stop:              ## Stop the k3s EC2 (halts all compute; EBS + data survive)
	aws ec2 stop-instances --instance-ids $$(cd $(K3S_ENV) && terraform output -raw instance_id)
	@echo "stopping — billing for compute halts once 'stopped'"

start:             ## Start the k3s EC2 and print the new SSH tunnel command
	aws ec2 start-instances --instance-ids $$(cd $(K3S_ENV) && terraform output -raw instance_id)
	aws ec2 wait instance-running --instance-ids $$(cd $(K3S_ENV) && terraform output -raw instance_id)
	@echo "started. Refresh outputs + open the tunnel:"
	@cd $(K3S_ENV) && terraform refresh >/dev/null && terraform output -raw ssh_command; echo

k3s-kubeconfig:    ## Fetch the k3s kubeconfig over SSH into $(KUBECONFIG_FILE)
	scp -o StrictHostKeyChecking=accept-new \
	  ec2-user@$$(cd $(K3S_ENV) && terraform output -raw public_ip):/etc/rancher/k3s/k3s.yaml $(KUBECONFIG_FILE)
	@echo "export KUBECONFIG=$(KUBECONFIG_FILE)"

k3s-gate2:         ## Run the purchase flow against k3s (tunnel must be open)
	KUBECONFIG=$(KUBECONFIG_FILE) GW=http://localhost:3000/api ./scripts/gate1-purchase-flow.sh
