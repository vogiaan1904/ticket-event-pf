# TicketBottle — cluster operations for every deploy target.
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

.PHONY: apps-up apps-build apps-deploy smoke-apps

apps-build:            ## Build the app images and load them into kind
	./scripts/build-images.sh

apps-deploy:           ## Deploy/upgrade the chart only (no image build)
	helm upgrade --install $(RELEASE) $(CHART) -n $(NS) --create-namespace -f $(LOCAL_VALUES) --wait --timeout 8m

apps-up: apps-build apps-deploy   ## Build+load images and deploy the app tier

smoke-apps:            ## Verify the app tier (pods Ready + signup)
	./scripts/smoke-apps.sh

.PHONY: gate1

gate1:                 ## Run the full purchase-flow acceptance test
	./scripts/gate1-purchase-flow.sh



# --- k3s on EC2 -------------------------------------------------------
K3S_ENV := terraform/envs/k3s
K3S_VALUES := $(CHART)/values-k3s.yaml
KUBECONFIG_FILE := /tmp/k3s.yaml
SSH_OPTS := -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR

# The instance id is stable, so Terraform state is a fine source for it. The
# public IP is not — EC2 reassigns it on every stop/start — so read it live from
# the API. `terraform refresh` refreshes resource attributes but leaves the
# persisted root outputs stale, which is what made `ssh_command` wrong; use the
# `k3s-refresh` target below when the Terraform outputs themselves need syncing.
K3S_ID := $$(cd $(K3S_ENV) && terraform output -raw instance_id)
K3S_IP := $$(aws ec2 describe-instances --instance-ids $(K3S_ID) --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)
K3S_TUNNEL := ssh $(SSH_OPTS) -L 6443:127.0.0.1:6443 -L 3000:127.0.0.1:30000 ec2-user@

.PHONY: k3s-ip k3s-instance k3s-refresh stop-ec2-k3s start-ec2-k3s tunnel-ec2-k3s k3s-kubeconfig k3s-deploy k3s-gate2

k3s-ip:            ## Print the current public IP of the k3s box (live from EC2)
	@echo "$(K3S_IP)"

k3s-instance:      ## Print the instance id
	@echo "$(K3S_ID)"

k3s-refresh:       ## Sync Terraform's stored outputs with reality (changes no infra)
	@cd $(K3S_ENV) && terraform apply -refresh-only -auto-approve

stop-ec2-k3s:              ## Stop the k3s EC2 (halts all compute; EBS + data survive)
	aws ec2 stop-instances --instance-ids $(K3S_ID)
	@echo "stopping — billing for compute halts once 'stopped'"

start-ec2-k3s:             ## Start the k3s EC2 and print the new SSH tunnel command
	aws ec2 start-instances --instance-ids $(K3S_ID)
	aws ec2 wait instance-running --instance-ids $(K3S_ID)
	@echo "started. Open the tunnel with:"
	@echo "  $(K3S_TUNNEL)$(K3S_IP)"

tunnel-ec2-k3s:            ## Open the SSH tunnel to the box (blocks — run in its own terminal)
	$(K3S_TUNNEL)$(K3S_IP)

k3s-kubeconfig:    ## Fetch the k3s kubeconfig over SSH into $(KUBECONFIG_FILE)
	scp $(SSH_OPTS) ec2-user@$(K3S_IP):/etc/rancher/k3s/k3s.yaml $(KUBECONFIG_FILE)
	@echo "export KUBECONFIG=$(KUBECONFIG_FILE)"

k3s-deploy:        ## Deploy the chart to k3s from ECR (tunnel must be open)
	KUBECONFIG=$(KUBECONFIG_FILE) helm upgrade --install $(RELEASE) $(CHART) -n $(NS) --create-namespace \
	  -f $(K3S_VALUES) \
	  --set image.registry="$$(aws sts get-caller-identity --query Account --output text).dkr.ecr.us-east-1.amazonaws.com/" \
	  --wait --timeout 10m

k3s-gate2:         ## Run the purchase flow against k3s (tunnel must be open)
	KUBECONFIG=$(KUBECONFIG_FILE) GW=http://localhost:3000/api ./scripts/gate1-purchase-flow.sh


# --- Ephemeral EKS ----------------------------------------------------
# Weekend sprint, never an everyday environment. There is NO stop switch: the
# control plane bills $0.10/hr from `eks-up` until `eks-down`.
EKS_ENV     := terraform/envs/eks
EKS_VALUES  := $(CHART)/values-eks.yaml
EKS_CLUSTER := ticketbottle-eks
REGION      := us-east-1

.PHONY: eks-guard eks-status eks-up eks-kubeconfig eks-bootstrap eks-deploy eks-url eks-gate3 eks-teardown eks-leak-check eks-sweep-orphans eks-down eks-metrics eks-gate4a eks-gate4b

eks-guard:         ## Refuse to proceed unless the k3s box is stopped (never both)
	@state=$$(aws ec2 describe-instances --instance-ids $(K3S_ID) \
	   --query 'Reservations[0].Instances[0].State.Name' --output text 2>/dev/null || echo unknown); \
	 if [ "$$state" != "stopped" ] && [ "$$state" != "terminated" ]; then \
	   echo "REFUSING: the k3s box is '$$state'. Run 'make stop-ec2-k3s' first — never run both."; \
	   exit 1; \
	 fi; \
	 echo "k3s box is $$state — safe to proceed"

eks-status:        ## Is anything billing? (the only honest answer is from AWS)
	@echo "clusters: $$(aws eks list-clusters --region $(REGION) --query 'clusters' --output text)"
	@echo "load balancers: $$(aws elbv2 describe-load-balancers --region $(REGION) --query 'LoadBalancers[].LoadBalancerName' --output text)"
	@echo "k3s box: $$(aws ec2 describe-instances --instance-ids $(K3S_ID) --query 'Reservations[0].Instances[0].State.Name' --output text 2>/dev/null || echo none)"

eks-up: eks-guard  ## terraform apply — STARTS the $0.10/hr meter
	@date
	cd $(EKS_ENV) && terraform apply
	@echo "meter running. End the session with: make -C deploy eks-down"

eks-kubeconfig:    ## Point kubectl at the cluster
	aws eks update-kubeconfig --region $(REGION) --name $(EKS_CLUSTER)

eks-bootstrap:     ## Cluster-side pieces Terraform can't own (gp3 SC + LB controller)
	./scripts/eks-bootstrap.sh

eks-deploy:        ## Deploy the chart from ECR and wait for the ALB to serve
	./scripts/eks-deploy.sh

eks-url:           ## Print the ALB hostname
	@kubectl -n $(NS) get ingress app-gateway -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'; echo

eks-gate3:         ## Gate 3a — purchase flow through the ALB
	GW=http://$$(kubectl -n $(NS) get ingress app-gateway -o jsonpath='{.status.loadBalancer.ingress[0].hostname}')/api \
	  ./scripts/gate1-purchase-flow.sh

eks-teardown:      ## Ordered cleanup then terraform destroy (prompts)
	./scripts/eks-teardown.sh

eks-leak-check:    ## Gate 3b — assert nothing billable survived
	./scripts/eks-leak-check.sh

eks-sweep-orphans: ## Recovery: delete an ALB/volumes stranded by a destroy that skipped cleanup
	./scripts/eks-sweep-orphans.sh

eks-down: eks-teardown eks-leak-check  ## THE off switch: teardown + leak check. End every session with this.

eks-metrics:       ## Show live CPU/memory per pod (needs metrics-server)
	@kubectl top nodes
	@echo
	@kubectl top pods -n $(NS) --sort-by=cpu

eks-gate4a:        ## Gate 4a — concurrency: no oversell, HPA scales out and back in
	GW=http://$$(kubectl -n $(NS) get ingress app-gateway -o jsonpath='{.status.loadBalancer.ingress[0].hostname}')/api \
	  ./scripts/gate4a-load.sh

eks-gate4b:        ## Gate 4b — terminate a node under load, assert nothing is lost
	GW=http://$$(kubectl -n $(NS) get ingress app-gateway -o jsonpath='{.status.loadBalancer.ingress[0].hostname}')/api \
	  ./scripts/gate4b-chaos.sh

# --- Your public IP: the allowlist that follows you around --------------------
# `my_ip_cidr` in each env's terraform.tfvars gates three things:
#   1. port 22 on the k3s box       — SG rule, k3s env
#   2. the EKS public API endpoint  — public_access_cidrs, eks env
#   3. the ALB's inbound-cidrs      — a helm annotation, read from the eks output
#      by eks-deploy.sh, so `terraform apply` alone leaves the ALB on the old IP.
IP_URL := https://checkip.amazonaws.com
TFVARS := $(K3S_ENV)/terraform.tfvars $(EKS_ENV)/terraform.tfvars
READ_IP = sed -n 's/^my_ip_cidr[[:space:]]*=[[:space:]]*"\(.*\)".*/\1/p'

.PHONY: my-ip update-my-ip k3s-allow-ip eks-allow-ip

my-ip:             ## Compare your current public IP against what each tfvars allows
	@ip=$$(curl -fsS --max-time 5 $(IP_URL) | tr -d '[:space:]'); \
	 printf '%-34s %s\n' "current" "$$ip"; \
	 for f in $(TFVARS); do \
	   if [ -f $$f ]; then printf '%-34s %s\n' "$$f" "$$($(READ_IP) $$f)"; \
	   else printf '%-34s %s\n' "$$f" "(absent)"; fi; \
	 done

update-my-ip:      ## Rewrite my_ip_cidr in every tfvars to your current IP
	@ip=$$(curl -fsS --max-time 5 $(IP_URL) | tr -d '[:space:]'); \
	 echo "$$ip" | grep -Eq '^([0-9]{1,3}\.){3}[0-9]{1,3}$$' || \
	   { echo "REFUSING: '$$ip' is not an IPv4 — captive portal, or $(IP_URL) unreachable"; exit 1; }; \
	 for f in $(TFVARS); do \
	   if [ ! -f $$f ]; then echo "skip  $$f (absent — copy its terraform.tfvars.example)"; continue; fi; \
	   old=$$($(READ_IP) $$f); \
	   if [ "$$old" = "$$ip/32" ]; then echo "ok    $$f already $$ip/32"; continue; fi; \
	   sed -E 's|^(my_ip_cidr[[:space:]]*=[[:space:]]*).*|\1"'"$$ip"'/32"|' $$f > $$f.new && mv $$f.new $$f; \
	   echo "edit  $$f  $$old -> $$ip/32"; \
	 done; \
	 echo; echo "apply it: make -C deploy k3s-allow-ip   (or eks-allow-ip)"

k3s-allow-ip: update-my-ip   ## ...then apply it to the k3s SSH security group
	@cd $(K3S_ENV) && terraform state list 2>/dev/null | grep -q aws_instance || \
	  { echo "REFUSING: no k3s instance in state. 'make k3s-allow-ip' re-opens a firewall, it does not build a box."; exit 1; }
	cd $(K3S_ENV) && terraform apply

eks-allow-ip: update-my-ip   ## ...then apply it to the EKS API endpoint AND the live ALB
	@aws eks describe-cluster --region $(REGION) --name $(EKS_CLUSTER) >/dev/null 2>&1 || \
	  { echo "REFUSING: $(EKS_CLUSTER) does not exist. Applying here would BUILD it and start the \$$0.10/hr meter — use 'make eks-up' if that is what you want."; exit 1; }
	cd $(EKS_ENV) && terraform apply
	@ip=$$(cd $(EKS_ENV) && terraform output -raw my_ip_cidr); \
	 if helm status $(RELEASE) -n $(NS) >/dev/null 2>&1; then \
	   echo "== re-pointing the ALB at $$ip (annotation only; no pod rolls) =="; \
	   helm upgrade $(RELEASE) $(CHART) -n $(NS) --reuse-values --set ingress.inboundCidrs="$$ip"; \
	 else \
	   echo "chart not installed — the ALB picks up $$ip on the next 'make eks-deploy'"; \
	 fi
