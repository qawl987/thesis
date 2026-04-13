# free5GC Makefile
# Scale helpers to reduce VM load when free5GC is not in active use.
# Modelled after srsran-operator/Makefile gnb-up/gnb-down targets.

F5G_KUBECONFIG ?= /home/free5gc/regional.kubeconfig
F5G_CLUSTER    ?= regional
F5G_CP_NS      ?= free5gc-cp
F5G_UPF_NS     ?= free5gc-upf
F5G_OP_NS      ?= free5gc  # namespace where free5gc-operator runs

# Gitea repo URL (single source of truth – changes here are picked up by ConfigSync)
GITEA_BASE     ?= http://nephio:secret@172.18.0.200:3000/nephio/regional.git

# Worker node docker container name (auto-detected from cluster nodes if not set)
WORKER_NODE    ?= $(shell kubectl --kubeconfig=$(F5G_KUBECONFIG) get node -o name 2>/dev/null | grep md | head -1 | cut -d/ -f2)
# Control-plane kind node container name (fixed for every new VM)
CP_NODE        ?= regional-control-plane
# Parent interface for n6-gw macvlan (the VLAN interface on the worker node)
N6_PARENT      ?= eth1.4
N6_GW_IP       ?= 10.0.1.254/8

# kube-rbac-proxy image fix (gcr.io/kubebuilder removed this image)
KRBP_SRC       ?= quay.io/brancz/kube-rbac-proxy:v0.8.0
KRBP_IMAGE     ?= gcr.io/kubebuilder/kube-rbac-proxy:v0.8.0

.PHONY: help op-down op-up cp-down cp-up upf-down upf-up all-down all-up fix-n6-gw reconcile-nfs fix-git-upf-n3 fix-rbac-image fix-upf-dnnlist verify-upf-dnnlist

help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", $$2 } ' $(MAKEFILE_LIST)

##@ Operator (free5gc: free5gc-operator)

op-down: ## Scale down free5gc-operator to 0 and wait until gone.
	kubectl --kubeconfig=$(F5G_KUBECONFIG) scale deployment --all --replicas=0 -n $(F5G_OP_NS)
	kubectl --kubeconfig=$(F5G_KUBECONFIG) wait --for=delete pod -l control-plane=controller-manager \
		-n $(F5G_OP_NS) --timeout=60s 2>/dev/null || \
	kubectl --kubeconfig=$(F5G_KUBECONFIG) wait --for=delete pod --all \
		-n $(F5G_OP_NS) --timeout=60s 2>/dev/null || true
	@echo "  free5gc-operator is down"

op-up: ## Scale up free5gc-operator to 1.
	kubectl --kubeconfig=$(F5G_KUBECONFIG) scale deployment --all --replicas=1 -n $(F5G_OP_NS)

##@ Control Plane (free5gc-cp: AMF, SMF, AUSF, NRF, NSSF, PCF, UDM, UDR, WebUI)

cp-down: op-down ## Stop operator (and wait), then scale down all free5gc-cp workloads to 0.
	kubectl --kubeconfig=$(F5G_KUBECONFIG) scale deployment --all --replicas=0 -n $(F5G_CP_NS)
	kubectl --kubeconfig=$(F5G_KUBECONFIG) scale statefulset --all --replicas=0 -n $(F5G_CP_NS)

cp-up: ## Scale up all free5gc-cp workloads to 1 then resume operator.
	kubectl --kubeconfig=$(F5G_KUBECONFIG) scale statefulset --all --replicas=1 -n $(F5G_CP_NS)
	kubectl --kubeconfig=$(F5G_KUBECONFIG) scale deployment --all --replicas=1 -n $(F5G_CP_NS)
	$(MAKE) op-up

##@ User Plane (free5gc-upf: UPF)

upf-down: ## Scale down ALL deployments in free5gc-upf to 0 (run after op-down).
	kubectl --kubeconfig=$(F5G_KUBECONFIG) scale deployment --all --replicas=0 -n $(F5G_UPF_NS)

upf-up: ## Scale up ALL deployments in free5gc-upf to 1, then restore n6-gw.
	kubectl --kubeconfig=$(F5G_KUBECONFIG) scale deployment --all --replicas=1 -n $(F5G_UPF_NS)
	$(MAKE) fix-n6-gw

##@ All

all-down: op-down ## Stop operator then scale down CP + UPF.
	kubectl --kubeconfig=$(F5G_KUBECONFIG) scale deployment --all --replicas=0 -n $(F5G_CP_NS)
	kubectl --kubeconfig=$(F5G_KUBECONFIG) scale statefulset --all --replicas=0 -n $(F5G_CP_NS)
	kubectl --kubeconfig=$(F5G_KUBECONFIG) scale deployment --all --replicas=0 -n $(F5G_UPF_NS)

all-up: ## Scale up CP + UPF then resume operator and restore n6-gw.
	kubectl --kubeconfig=$(F5G_KUBECONFIG) scale statefulset --all --replicas=1 -n $(F5G_CP_NS)
	kubectl --kubeconfig=$(F5G_KUBECONFIG) scale deployment --all --replicas=1 -n $(F5G_CP_NS)
	kubectl --kubeconfig=$(F5G_KUBECONFIG) scale deployment --all --replicas=1 -n $(F5G_UPF_NS)
	$(MAKE) op-up
	$(MAKE) fix-n6-gw

##@ N6 Gateway

fix-n6-gw: ## Restore n6-gw macvlan interface on worker node (ephemeral, lost on pod/node restart).
	@WORKER=$(WORKER_NODE); \
	if [ -z "$$WORKER" ]; then echo "ERROR: cannot detect worker node"; exit 1; fi; \
	echo "==> Worker node: $$WORKER"; \
	if sudo docker exec $$WORKER ip link show n6-gw >/dev/null 2>&1; then \
		echo "  n6-gw already exists, skipping."; \
	else \
		echo "  Creating n6-gw ($(N6_PARENT) -> macvlan -> $(N6_GW_IP))..."; \
		sudo docker exec $$WORKER sh -c \
			"ip link add n6-gw link $(N6_PARENT) type macvlan mode bridge && \
			 ip addr add $(N6_GW_IP) dev n6-gw && \
			 ip link set n6-gw up" && \
		echo "  n6-gw created: $$(sudo docker exec $$WORKER ip addr show n6-gw 2>/dev/null | grep inet | xargs)"; \
	fi

##@ Image Fixes

fix-rbac-image: ## Pull kube-rbac-proxy from quay.io, retag as gcr.io, and load into kind nodes (one-time per VM).
	@if sudo docker image inspect "$(KRBP_IMAGE)" >/dev/null 2>&1; then \
		echo "  kube-rbac-proxy already present in Docker cache – skipping pull"; \
	else \
		echo "  Pulling $(KRBP_SRC)..."; \
		sudo docker pull "$(KRBP_SRC)" \
			&& sudo docker tag "$(KRBP_SRC)" "$(KRBP_IMAGE)" \
			&& echo "  ✓ Pulled and tagged as $(KRBP_IMAGE)" \
			|| echo "  Warning: pull failed – kube-rbac-proxy sidecar may stay in ImagePullBackOff"; \
	fi
	@for node in "$(CP_NODE)" "$(WORKER_NODE)"; do \
		if sudo docker exec "$$node" ctr -n k8s.io images ls 2>/dev/null | grep -q "kube-rbac-proxy:v0.8.0"; then \
			echo "  $$node: image already present in containerd – skipping"; \
		else \
			echo "  Loading $(KRBP_IMAGE) into node $$node..."; \
			sudo docker save "$(KRBP_IMAGE)" \
				| sudo docker exec -i "$$node" ctr -n k8s.io images import - 2>/dev/null \
				&& echo "  ✓ $$node: loaded" \
				|| echo "  Warning: failed to load into $$node"; \
		fi; \
	done
	@echo "✓ kube-rbac-proxy image pre-loaded into all nodes"

##@ Git Source-of-Truth Fixes

fix-git-upf-n3: ## Patch regional.git: set UPF N3 endpoint to 172.3.0.254/24 in SMF Config resource.
	@# ConfigSync is the single source of truth – direct configmap edits get reverted.
	@# IPAM specializer renders 172.3.0.0/16 (network base) into Config resources;
	@# free5gc-operator reads these to build smfcfg.yaml userplaneInformation.
	@# Fix: overwrite with the actual UPF host address 172.3.0.254/24.
	@REPO_DIR=$$(mktemp -d); \
	git clone $(GITEA_BASE) $$REPO_DIR --depth=1 -q; \
	cd $$REPO_DIR; \
	git config user.email "nephio@nephio.org"; \
	git config user.name "Nephio"; \
	SMF_CFG="free5gc-smf/free5gc-cp/config_smf-regional-upf-regional.yaml.yaml"; \
	sed -i 's|address: 172\.3\.0\.0/[0-9]*|address: 172.3.0.254/24|g' $$SMF_CFG 2>/dev/null || true; \
	if git diff --quiet; then \
		echo "  regional.git already has correct UPF N3 endpoint – nothing to commit."; \
	else \
		git add $$SMF_CFG; \
		git commit -m "fix: UPF N3 endpoint → 172.3.0.254/24 in SMF Config (IPAM rendered /16 base addr)"; \
		git push -q && echo "  ✓ Pushed – ConfigSync applies in ~60s, then run: make reconcile-nfs"; \
	fi; \
	rm -rf $$REPO_DIR

##@ IP Conflict Fix
# Symptom: AMF or UPF pod starts but binds to wrong IP (e.g. 172.2.0.0 instead of 172.2.0.254).
# Root cause: free5gc-operator only sets the Multus network annotation and ConfigMap at *creation* time.
#   If the NFDeployment IP was later fixed in regional.git (via ConfigSync), the existing
#   Deployment/ConfigMap are stale and the operator will NOT update them.
# Fix: delete both, the operator recreates them from the (now-correct) NFDeployment spec.

reconcile-nfs: ## Force operator to recreate AMF+SMF+UPF Deployment and ConfigMap with correct IPs/pools.
	@echo "==> Deleting stale AMF ConfigMap + Deployment (free5gc-operator will recreate)..."
	kubectl --kubeconfig=$(F5G_KUBECONFIG) delete configmap  amf-regional -n $(F5G_CP_NS)  --ignore-not-found
	kubectl --kubeconfig=$(F5G_KUBECONFIG) delete deployment amf-regional -n $(F5G_CP_NS)  --ignore-not-found
	@echo "==> Deleting stale SMF ConfigMap + Deployment (free5gc-operator will recreate with updated pool order)..."
	kubectl --kubeconfig=$(F5G_KUBECONFIG) delete configmap  smf-regional -n $(F5G_CP_NS)  --ignore-not-found
	kubectl --kubeconfig=$(F5G_KUBECONFIG) delete deployment smf-regional -n $(F5G_CP_NS)  --ignore-not-found
	@echo "==> Deleting stale UPF ConfigMap + Deployment (free5gc-operator will recreate)..."
	kubectl --kubeconfig=$(F5G_KUBECONFIG) delete configmap  upf-regional  -n $(F5G_UPF_NS) --ignore-not-found
	kubectl --kubeconfig=$(F5G_KUBECONFIG) delete deployment upf-regional  -n $(F5G_UPF_NS) --ignore-not-found
	@echo "==> Nudging AMF/SMF/UPF NFDeployments to trigger reconcile..."
	kubectl --kubeconfig=$(F5G_KUBECONFIG) annotate nfdeployment amf-regional -n $(F5G_CP_NS) \
		fix-trigger="$$(date +%s)" --overwrite
	kubectl --kubeconfig=$(F5G_KUBECONFIG) annotate nfdeployment smf-regional -n $(F5G_CP_NS) \
		fix-trigger="$$(date +%s)" --overwrite
	kubectl --kubeconfig=$(F5G_KUBECONFIG) annotate nfdeployment upf-regional -n $(F5G_UPF_NS) \
		fix-trigger="$$(date +%s)" --overwrite
	@echo "==> Waiting 20s for operator to recreate resources..."
	@sleep 20
	@echo "==> Resulting Multus annotations (should show .254 addresses):"
	@kubectl --kubeconfig=$(F5G_KUBECONFIG) get deployment amf-regional -n $(F5G_CP_NS) \
		-o jsonpath='AMF n2: {.spec.template.metadata.annotations.k8s\.v1\.cni\.cncf\.io/networks}{"\n"}' 2>/dev/null || echo "  AMF deployment not found yet"
	@kubectl --kubeconfig=$(F5G_KUBECONFIG) get deployment upf-regional -n $(F5G_UPF_NS) \
		-o jsonpath='UPF n3: {.spec.template.metadata.annotations.k8s\.v1\.cni\.cncf\.io/networks}{"\n"}' 2>/dev/null || echo "  UPF deployment not found yet"
	@echo "==> SMF pool order (first entry should be 10.0.2.0/24):"
	@kubectl --kubeconfig=$(F5G_KUBECONFIG) get configmap smf-regional -n $(F5G_CP_NS) \
		-o jsonpath='{.data.smfcfg\.yaml}' 2>/dev/null | grep cidr | head -3 || echo "  SMF configmap not found yet"

##@ UPF DNN Pool Fix

fix-upf-dnnlist: ## Patch UPF ConfigMap to add missing dnnList entries (10.0.2.0/24, 10.0.3.0/24).
	@# Problem: free5gc-operator only reads first pool from NFDeployment, but SMF allocates
	@# UE IPs from all 3 pools. UPF needs routes for all pools to forward return traffic.
	@# This patches the ConfigMap directly. Run after operator recreates UPF resources.
	@echo "==> Checking current UPF dnnList..."
	@CURRENT=$$(kubectl --kubeconfig=$(F5G_KUBECONFIG) get configmap upf-regional -n $(F5G_UPF_NS) \
		-o jsonpath='{.data.upfcfg\.yaml}' 2>/dev/null | grep -c "cidr:" || echo "0"); \
	if [ "$$CURRENT" -ge 3 ]; then \
		echo "  UPF dnnList already has 3+ entries – nothing to patch."; \
	else \
		echo "  Patching UPF ConfigMap to add 10.0.2.0/24 and 10.0.3.0/24..."; \
		kubectl --kubeconfig=$(F5G_KUBECONFIG) get configmap upf-regional -n $(F5G_UPF_NS) -o yaml > /tmp/upf-cm.yaml; \
		sed -i '/dnnList:/,/pfcp:/{s/dnnList:/dnnList:\n      - cidr: 10.0.2.0\/24\n        dnn: internet\n        natifname: n6\n      - cidr: 10.0.3.0\/24\n        dnn: internet\n        natifname: n6/}' /tmp/upf-cm.yaml; \
		sed -i 's/MASQUERADE  # route traffic/MASQUERADE\n    iptables -t nat -A POSTROUTING -s 10.0.2.0\/24 -o n6 -j MASQUERADE\n    iptables -t nat -A POSTROUTING -s 10.0.3.0\/24 -o n6 -j MASQUERADE  # route traffic/' /tmp/upf-cm.yaml; \
		sed -i 's/ip rule add from 10.0.1.0\/24/ip rule add from 10.0.1.0\/24 table n6if\n    ip rule add from 10.0.2.0\/24 table n6if\n    ip rule add from 10.0.3.0\/24/' /tmp/upf-cm.yaml; \
		kubectl --kubeconfig=$(F5G_KUBECONFIG) apply -f /tmp/upf-cm.yaml; \
		rm -f /tmp/upf-cm.yaml; \
		echo "  ✓ Patched. Restarting UPF pod to apply changes..."; \
		kubectl --kubeconfig=$(F5G_KUBECONFIG) rollout restart deployment upf-regional -n $(F5G_UPF_NS); \
		echo "  ✓ UPF restart triggered. Wait ~30s then verify with: make verify-upf-dnnlist"; \
	fi

verify-upf-dnnlist: ## Verify UPF has all 3 dnnList entries and routes.
	@echo "==> UPF dnnList entries:"
	@kubectl --kubeconfig=$(F5G_KUBECONFIG) get configmap upf-regional -n $(F5G_UPF_NS) \
		-o jsonpath='{.data.upfcfg\.yaml}' 2>/dev/null | grep -A2 "dnnList:" | head -10 || echo "  ConfigMap not found"
	@echo ""
	@echo "==> UPF pod routes (should have 10.0.1/2/3.0/24 via upfgtp):"
	@UPF_POD=$$(kubectl --kubeconfig=$(F5G_KUBECONFIG) get pod -n $(F5G_UPF_NS) -l name=upf-regional -o name 2>/dev/null | head -1); \
	if [ -n "$$UPF_POD" ]; then \
		kubectl --kubeconfig=$(F5G_KUBECONFIG) exec -n $(F5G_UPF_NS) $$UPF_POD -- ip route 2>/dev/null | grep "10.0" || echo "  No 10.0.x routes found"; \
	else \
		echo "  UPF pod not running"; \
	fi

##@ Kind Redeploy (free5gc-operator)

# free5gc-operator image
F5G_OP_IMG     ?= docker.io/nephio/free5gc-operator:latest
# Kind worker node container name
KIND_WORKER    ?= $(shell kubectl --kubeconfig=$(F5G_KUBECONFIG) get node -o name 2>/dev/null | grep md | head -1 | cut -d/ -f2)
# free5gc source directory
F5G_SRC_DIR    ?= /home/free5gc/free5gc

.PHONY: build-linux
build-linux: ## Build the free5gc-operator binary for linux/amd64 (no CGO).
	cd $(F5G_SRC_DIR) && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -C free5gc-operator/ -a -o free5gc-operator

.PHONY: docker-build-sudo
docker-build-sudo: ## Build the container image using sudo docker.
	cd $(F5G_SRC_DIR) && sudo docker build -t $(F5G_OP_IMG) .

.PHONY: kind-load
kind-load: ## Save the image and import it into the kind worker node via ctr.
	@if [ -z "$(KIND_WORKER)" ]; then echo "ERROR: cannot detect worker node"; exit 1; fi
	sudo docker save $(F5G_OP_IMG) | \
		sudo docker exec -i $(KIND_WORKER) ctr -n k8s.io images import -

.PHONY: restart-operator
restart-operator: ## Rollout-restart the free5gc-operator deployment and wait for it.
	kubectl --kubeconfig=$(F5G_KUBECONFIG) rollout restart \
		deployment/free5gc-operator -n $(F5G_OP_NS)
	kubectl --kubeconfig=$(F5G_KUBECONFIG) rollout status \
		deployment/free5gc-operator -n $(F5G_OP_NS) --timeout=60s

.PHONY: restart-nfs
restart-nfs: ## Rollout-restart all free5gc NF deployments and wait for them.
	kubectl --kubeconfig=$(F5G_KUBECONFIG) rollout restart \
		deployment/amf-regional \
		deployment/smf-regional \
		-n $(F5G_CP_NS)
	kubectl --kubeconfig=$(F5G_KUBECONFIG) rollout restart \
		deployment/upf-regional \
		-n $(F5G_UPF_NS)
	kubectl --kubeconfig=$(F5G_KUBECONFIG) rollout status \
		deployment/amf-regional -n $(F5G_CP_NS) --timeout=120s
	kubectl --kubeconfig=$(F5G_KUBECONFIG) rollout status \
		deployment/smf-regional -n $(F5G_CP_NS) --timeout=120s
	kubectl --kubeconfig=$(F5G_KUBECONFIG) rollout status \
		deployment/upf-regional -n $(F5G_UPF_NS) --timeout=120s

.PHONY: restart-all
restart-all: build-linux docker-build-sudo kind-load restart-operator ## Full redeploy: build → docker → kind load → restart operator → restart NFs.
	@echo "Waiting 15s for operator to fully start and reconcile..."
	sleep 15
	$(MAKE) restart-nfs