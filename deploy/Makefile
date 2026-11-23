# TicketBottle — cluster operations for every deploy target.
NS      := ticketbottle
CHART   := helm/ticketbottle
RELEASE := tb

# --- observability ----------------------------------------------------
# Cluster-agnostic: acts on whatever the current KUBECONFIG points at.
.PHONY: obs-up obs-grafana

obs-up:                ## Install kube-prometheus-stack and provision the dashboards
	./scripts/obs-install.sh

obs-grafana:           ## Port-forward Grafana on :3001 (admin password below)
	@kubectl -n monitoring get secret kps-grafana -o jsonpath='{.data.admin-password}' | base64 -d; echo
	kubectl -n monitoring port-forward svc/kps-grafana 3001:80

.PHONY: temporal-ui

temporal-ui:           ## Port-forward the Temporal web UI on :8233
	@echo "http://localhost:8233"
	kubectl -n $(NS) port-forward svc/temporal-ui 8233:8080

# --- k3s on EC2 -------------------------------------------------------
K3S_ENV := terraform/envs/k3s
K3S_VALUES := $(CHART)/values-k3s.yaml
KUBECONFIG_FILE := /tmp/k3s.yaml
# SSH rides an SSM session to the instance id, so no inbound port or IP allowlist (0038).
# Keepalives also hold off Session Manager's 20-minute idle cut.
SSH_OPTS = -o StrictHostKeyChecking=accept-new -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR \
  -o ServerAliveInterval=30 \
  -o ProxyCommand="aws ssm start-session --region $(REGION) --target %h --document-name AWS-StartSSHSession --parameters portNumber=%p"

# The instance id is stable, so Terraform state is a fine source for it. The
# public IP is not — EC2 reassigns it on every stop/start — so read it live.
K3S_ID := $$(cd $(K3S_ENV) && terraform output -raw instance_id)
K3S_IP := $$(aws ec2 describe-instances --instance-ids $(K3S_ID) --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)
K3S_TUNNEL = ssh $(SSH_OPTS) -N -L 6443:127.0.0.1:6443 -L 3000:127.0.0.1:30000 ec2-user@

.PHONY: k3s-ip k3s-instance k3s-refresh stop-ec2-k3s start-ec2-k3s tunnel-ec2-k3s k3s-kubeconfig k3s-deploy k3s-gate2 k3s-gate-sold-out k3s-gate-checkout-expiry k3s-gate-room k3s-gate-orders k3s-gate-token-revoked k3s-gate-sell-out-tail k3s-gate-read-back k3s-load

k3s-ip:            ## Print the current public IP of the k3s box (live from EC2)
	@echo "$(K3S_IP)"

k3s-instance:      ## Print the instance id
	@echo "$(K3S_ID)"

k3s-refresh:       ## Sync Terraform's stored outputs with reality (changes no infra)
	@cd $(K3S_ENV) && terraform apply -refresh-only -auto-approve

stop-ec2-k3s:              ## Stop the k3s EC2 (halts all compute; EBS + data survive)
	aws ec2 stop-instances --instance-ids $(K3S_ID)
	@echo "stopping — billing for compute halts once 'stopped'"

start-ec2-k3s:             ## Start the k3s EC2 and wait until Session Manager can reach it
	aws ec2 start-instances --instance-ids $(K3S_ID)
	aws ec2 wait instance-running --instance-ids $(K3S_ID)
	@id=$(K3S_ID); for i in $$(seq 1 40); do \
	   s=$$(aws ssm describe-instance-information --region $(REGION) --filters Key=InstanceIds,Values=$$id \
	        --query 'InstanceInformationList[0].PingStatus' --output text); \
	   [ "$$s" = Online ] && { echo "SSM agent online. Open the tunnel: make -C deploy tunnel-ec2-k3s"; exit 0; }; \
	   sleep 5; done; echo "SSM agent not online after 200s — check the instance role"; exit 1

tunnel-ec2-k3s:            ## Open the SSH tunnel to the box (blocks — run in its own terminal)
	$(K3S_TUNNEL)$(K3S_ID)

k3s-kubeconfig:    ## Fetch the k3s kubeconfig over SSH into $(KUBECONFIG_FILE)
	scp $(SSH_OPTS) ec2-user@$(K3S_ID):/etc/rancher/k3s/k3s.yaml $(KUBECONFIG_FILE)
	@echo "export KUBECONFIG=$(KUBECONFIG_FILE)"

# REF is the moving tag CI pushes per branch (slashes as dashes), e.g. REF=my-branch.
# k3s-deploy deploys the immutable sha- build it points at: the moving tag itself
# leaves the pod spec unchanged, so helm rolls nothing and old pods keep running.
# HELM_EXTRA passes any other helm flags through.
REF ?= dev
HELM_EXTRA ?=

k3s-deploy: secrets-check  ## Deploy the build ECR's :$(REF) points at (tunnel must be open)
	@tags=$$(./scripts/image-tag.sh $(REF)) || exit 1; \
	 echo "deploying $$tags (:$(REF))"; \
	 KUBECONFIG=$(KUBECONFIG_FILE) helm upgrade --install $(RELEASE) $(CHART) -n $(NS) --create-namespace \
	  -f $(K3S_VALUES) -f $(SECRETS_FILE) \
	  --set image.registry="$$(aws sts get-caller-identity --query Account --output text).dkr.ecr.us-east-1.amazonaws.com/" \
	  --set image.tag=$$tags \
	  $(HELM_EXTRA) \
	  --wait --timeout 10m

k3s-gate2:         ## Run the purchase flow against k3s (tunnel must be open)
	KUBECONFIG=$(KUBECONFIG_FILE) GW=http://localhost:3000/api ./scripts/gate1-purchase-flow.sh

k3s-gate-token-revoked: ## A completed or cancelled checkout's token is refused with 401 (tunnel must be open)
	KUBECONFIG=$(KUBECONFIG_FILE) GW=http://localhost:3000/api ./scripts/gate-token-revoked.sh

k3s-gate-sold-out: ## The door pauses while the last ticket is held, then refuses with 409 (tunnel must be open)
	KUBECONFIG=$(KUBECONFIG_FILE) GW=http://localhost:3000/api ./scripts/gate-sold-out.sh

k3s-gate-checkout-expiry: ## An unpaid order times out at its hold's expiry and ends everywhere (~9 min; tunnel must be open)
	KUBECONFIG=$(KUBECONFIG_FILE) GW=http://localhost:3000/api ./scripts/gate-checkout-expiry.sh

k3s-gate-room:     ## A room follows the tickets left: three of five admitted, then all five (tunnel must be open)
	KUBECONFIG=$(KUBECONFIG_FILE) GW=http://localhost:3000/api ./scripts/gate-room.sh

k3s-gate-orders:   ## Orders are read, listed and cancelled by their owner only (tunnel must be open)
	KUBECONFIG=$(KUBECONFIG_FILE) GW=http://localhost:3000/api ./scripts/gate-orders.sh

k3s-gate-sell-out-tail: ## Chairs that take no ticket end with the 5-minute window (~6 min; tunnel must be open)
	KUBECONFIG=$(KUBECONFIG_FILE) GW=http://localhost:3000/api ./scripts/gate-sell-out-tail.sh

k3s-gate-read-back: ## Every field sent through the gateway reads back as sent (tunnel must be open)
	KUBECONFIG=$(KUBECONFIG_FILE) GW=http://localhost:3000/api ./scripts/gate-read-back.sh

# Gate 4a's harness in generator mode: DURATION picks k6's sustained scenario,
# which makes the script exit after the run instead of asserting an HPA k3s
# has no room for. TOTAL is sized so the allotment never bounds the run.
LOAD_DURATION ?= 3m
LOAD_VUS      ?= 20
LOAD_TOTAL    ?= 5000

k3s-load:          ## Drive sustained purchase traffic on k3s (tunnel must be open)
	KUBECONFIG=$(KUBECONFIG_FILE) GW=http://localhost:3000/api \
	  DURATION=$(LOAD_DURATION) VUS=$(LOAD_VUS) TOTAL=$(LOAD_TOTAL) ./scripts/gate4a-load.sh


# --- Ephemeral EKS ----------------------------------------------------
# Brought up on demand and torn down after use. 
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

eks-bootstrap:     ## Cluster-side pieces Terraform can't own (gp3 SC, LB controller, metrics-server, kube-prometheus-stack)
	./scripts/eks-bootstrap.sh

eks-deploy:        ## Deploy the build ECR's :$(REF) points at, and wait for the ALB to serve
	REF=$(REF) HELM_EXTRA='$(HELM_EXTRA)' ./scripts/eks-deploy.sh

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

# --- Your public IP: the ALB's allowlist ---------------------------------------
# Only the ALB allowlists an IP; the EKS API is gated by IAM (0041), the k3s box
# by SSM (0038). WARP can move the IP mid-session: re-run eks-allow-ip.
IP_URL := https://checkip.amazonaws.com

.PHONY: my-ip eks-allow-ip

my-ip:             ## Compare your current public IP against what the live ALB allows
	@printf '%-8s %s/32\n' current "$$(curl -fsS --max-time 5 $(IP_URL) | tr -d '[:space:]')"; \
	 alb=$$(helm get values $(RELEASE) -n $(NS) -o json 2>/dev/null | jq -r '.ingress.inboundCidrs // empty'); \
	 printf '%-8s %s\n' ALB "$${alb:-(chart not installed, or the cluster unreachable)}"

eks-allow-ip:      ## Re-point the live ALB at your current IP (annotation only; no pod rolls)
	@ip=$$(curl -fsS --max-time 5 $(IP_URL) | tr -d '[:space:]'); \
	 echo "$$ip" | grep -Eq '^([0-9]{1,3}\.){3}[0-9]{1,3}$$' || \
	   { echo "REFUSING: '$$ip' is not an IPv4 — captive portal, or $(IP_URL) unreachable"; exit 1; }; \
	 helm status $(RELEASE) -n $(NS) >/dev/null 2>&1 || \
	   { echo "chart not installed — the ALB takes your IP on the next 'make eks-deploy'"; exit 0; }; \
	 helm upgrade $(RELEASE) $(CHART) -n $(NS) --reuse-values --set ingress.inboundCidrs="$$ip/32"

# --- Chart secrets ------------------------------------------------------------
# Signing keys and the admin password are `required` by the chart and have no
# committed defaults, so every deploy needs this file. It is gitignored:
# a key committed to a template is published to everyone who can read the repo.
SECRETS_FILE := secrets.values.yaml

.PHONY: secrets-init secrets-check

secrets-init:      ## Generate $(SECRETS_FILE) with fresh random keys (refuses to overwrite)
	@if [ -f $(SECRETS_FILE) ]; then \
	   echo "REFUSING: $(SECRETS_FILE) exists. Rotating means redeploying every service and invalidating"; \
	   echo "          every issued token — delete it deliberately if that is what you want."; exit 1; fi
	@rand() { LC_ALL=C tr -dc 'A-Za-z0-9' </dev/urandom | head -c "$$1"; }; \
	 { echo "# Generated by 'make -C deploy secrets-init'. Gitignored — never commit."; \
	   echo "# Rotating these invalidates every issued access and refresh token."; \
	   echo "secrets:"; \
	   echo "  jwtSecret: \"$$(rand 64)\""; \
	   echo "  jwtAccessSecret: \"$$(rand 48)\""; \
	   echo "  jwtRefreshSecret: \"$$(rand 48)\""; \
	   echo "  encryptKey: \"$$(rand 64)\""; \
	   echo "  internalKey: \"$$(rand 48)\""; \
	   echo "  adminPassword: \"$$(rand 24)\""; \
	   echo "  # Only applies at initdb: an existing PVC keeps the password it was created with."; \
	   echo "  postgresPassword: \"$$(rand 32)\""; \
	   echo "  # ZaloPay's published sandbox pair for app id 2554 — test credentials,"; \
	   echo "  # not merchant keys. Replace both when integrating a real merchant account."; \
	   echo "  zalopayKey1: \"sdngKKJmqEMzvh5QQcdD2A9XBSKUNaYn\""; \
	   echo "  zalopayKey2: \"trMrHtvjo6myautxDUiAcYsVtaeQ8nhf\""; \
	 } > $(SECRETS_FILE)
	@chmod 600 $(SECRETS_FILE)
	@echo "wrote $(SECRETS_FILE) (mode 600, gitignored)"
	@echo "the admin password is in it — read it once, then treat the file as the source of truth"

secrets-check:     ## Fail unless $(SECRETS_FILE) exists and is not tracked by git
	@[ -f $(SECRETS_FILE) ] || { echo "missing $(SECRETS_FILE) — run 'make -C deploy secrets-init'"; exit 1; }
	@! git ls-files --error-unmatch $(SECRETS_FILE) >/dev/null 2>&1 || \
	   { echo "REFUSING: $(SECRETS_FILE) is tracked by git. Untrack it before deploying."; exit 1; }
	@echo "$(SECRETS_FILE) present and untracked"
