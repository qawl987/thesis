# e2e-orchestrator Makefile
# Based on srsran-operator Makefile structure.

# Image and version settings
IMG ?= docker.io/nephio/e2e-orchestrator:latest
CONTROLLER_GEN ?= $(shell which controller-gen 2>/dev/null || echo go run sigs.k8s.io/controller-tools/cmd/controller-gen@latest)
ENVTEST ?= $(shell which setup-envtest 2>/dev/null || echo go run sigs.k8s.io/controller-runtime/tools/setup-envtest@latest)

# Configurable variables
OPERATOR_NS    ?= e2e-orchestrator
KUBECONFIG     ?= ~/.kube/config

# Regional cluster kubeconfig for free5GC webconsole access
REGIONAL_KUBECONFIG ?= /home/free5gc/regional.kubeconfig

# free5GC WebConsole credentials
FREE5GC_USERNAME ?= admin
FREE5GC_PASSWORD ?= free5gc

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
GOBIN=$(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN=$(shell go env GOPATH)/bin
endif

# Setting SHELL to bash allows bash commands to be used in recipes.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", $$2 } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: ## Generate CRD and ClusterRole RBAC manifests.
	$(CONTROLLER_GEN) crd paths="./api/..." output:crd:artifacts:config=config/crd/bases
	$(CONTROLLER_GEN) rbac:roleName=e2e-orchestrator-role paths="./..." output:rbac:artifacts:config=config/rbac

.PHONY: generate
generate: ## Generate DeepCopy methods.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./api/..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: fmt vet ## Run unit tests.
	go test ./internal/controller/... -v -count=1

.PHONY: lint
lint: ## Run golangci-lint (requires golangci-lint installed).
	golangci-lint run ./...

##@ Build

.PHONY: build
build: fmt vet ## Build manager binary.
	go build -o bin/manager ./cmd/main.go

.PHONY: run
run: fmt vet ## Run manager from your host (requires kubeconfig).
	go run ./cmd/main.go

.PHONY: run-webconsole
run-webconsole: fmt vet ## Run manager with free5GC webconsole integration.
	@echo "Detecting free5GC WebConsole URL from regional cluster..."
	$(eval NODE_IP := $(shell KUBECONFIG=$(REGIONAL_KUBECONFIG) kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}' 2>/dev/null))
	$(eval NODE_PORT := $(shell KUBECONFIG=$(REGIONAL_KUBECONFIG) kubectl get svc webui-service -n free5gc-cp -o jsonpath='{.spec.ports[0].nodePort}' 2>/dev/null))
	@if [ -z "$(NODE_IP)" ] || [ -z "$(NODE_PORT)" ]; then \
		echo "Error: Could not detect WebConsole URL. Check REGIONAL_KUBECONFIG=$(REGIONAL_KUBECONFIG)"; \
		exit 1; \
	fi
	@echo "Using WebConsole URL: http://$(NODE_IP):$(NODE_PORT)"
	go run ./cmd/main.go \
		--free5gc-url http://$(NODE_IP):$(NODE_PORT) \
		--free5gc-username $(FREE5GC_USERNAME) \
		--free5gc-password $(FREE5GC_PASSWORD)

.PHONY: docker-build
docker-build: ## Build container image.
	docker build -t $(IMG) .

.PHONY: docker-push
docker-push: ## Push container image.
	docker push $(IMG)

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests ## Install CRDs into the cluster.
	kubectl apply -f config/crd/bases/

.PHONY: uninstall
uninstall: manifests ## Uninstall CRDs from the cluster.
	kubectl delete --ignore-not-found=$(ignore-not-found) -f config/crd/bases/

.PHONY: deploy
deploy: manifests ## Deploy controller to the cluster.
	kubectl apply -f config/rbac/
	kubectl apply -f config/manager/

.PHONY: undeploy
undeploy: ## Undeploy controller from the cluster.
	kubectl delete --ignore-not-found=$(ignore-not-found) -f config/rbac/
	kubectl delete --ignore-not-found=$(ignore-not-found) -f config/manager/

##@ Build for deployment

.PHONY: build-linux
build-linux: ## Build the manager binary for linux/amd64 (no CGO).
	CGO_ENABLED=0 GOOS=linux go build -o manager ./cmd/main.go

##@ Testing

.PHONY: apply-sample
apply-sample: ## Apply sample E2EQoSIntent CR.
	kubectl apply -f config/samples/e2eqosintent_sample.yaml

.PHONY: delete-sample
delete-sample: ## Delete sample E2EQoSIntent CR.
	kubectl delete -f config/samples/e2eqosintent_sample.yaml --ignore-not-found
