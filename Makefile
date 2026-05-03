# kubebuilder は make target を呼び出すため、target 名だけを維持します。
# 実処理は mise task に集約し、Makefile からは同名 task を起動します。

IMG ?= controller:latest
CONTAINER_TOOL ?= docker
KIND_CLUSTER ?= cluster-api-provider-tart-test-e2e
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
MISE ?= mise

ifndef ignore-not-found
  ignore-not-found = false
endif

MISE_ENV = IMG="$(IMG)" CONTAINER_TOOL="$(CONTAINER_TOOL)" KIND_CLUSTER="$(KIND_CLUSTER)" PLATFORMS="$(PLATFORMS)" IGNORE_NOT_FOUND="$(ignore-not-found)"

.DEFAULT_GOAL := build

.PHONY: all
all: build

##@ General

.PHONY: help
help: ## Display this help.
	$(MISE) run help

##@ Development

.PHONY: manifests
manifests: ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(MISE_ENV) $(MISE) run manifests

.PHONY: generate
generate: ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(MISE_ENV) $(MISE) run generate

.PHONY: fmt
fmt: ## Run go fmt against code.
	$(MISE_ENV) $(MISE) run fmt

.PHONY: vet
vet: ## Run go vet against code.
	$(MISE_ENV) $(MISE) run vet

.PHONY: test
test: ## Run tests.
	$(MISE_ENV) $(MISE) run test

.PHONY: setup-test-e2e
setup-test-e2e: ## Set up a Kind cluster for e2e tests if it does not exist.
	$(MISE_ENV) $(MISE) run setup-test-e2e

.PHONY: test-e2e
test-e2e: ## Run the e2e tests. Expected an isolated environment using Kind.
	$(MISE_ENV) $(MISE) run test-e2e

.PHONY: cleanup-test-e2e
cleanup-test-e2e: ## Tear down the Kind cluster used for e2e tests.
	$(MISE_ENV) $(MISE) run cleanup-test-e2e

.PHONY: lint
lint: ## Run golangci-lint linter.
	$(MISE_ENV) $(MISE) run lint

.PHONY: lint-fix
lint-fix: ## Run golangci-lint linter and perform fixes.
	$(MISE_ENV) $(MISE) run lint-fix

.PHONY: lint-config
lint-config: ## Verify golangci-lint linter configuration.
	$(MISE_ENV) $(MISE) run lint-config

##@ Build

.PHONY: build
build: ## Build manager binary.
	$(MISE_ENV) $(MISE) run build

.PHONY: run
run: ## Run a controller from your host.
	$(MISE_ENV) $(MISE) run run

.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(MISE_ENV) $(MISE) run docker-build

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(MISE_ENV) $(MISE) run docker-push

.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support.
	$(MISE_ENV) $(MISE) run docker-buildx

.PHONY: build-installer
build-installer: ## Generate a consolidated YAML with CRDs and deployment.
	$(MISE_ENV) $(MISE) run build-installer

##@ Deployment

.PHONY: install
install: ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(MISE_ENV) $(MISE) run install

.PHONY: uninstall
uninstall: ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(MISE_ENV) $(MISE) run uninstall

.PHONY: deploy
deploy: ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	$(MISE_ENV) $(MISE) run deploy

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(MISE_ENV) $(MISE) run undeploy

##@ Dependencies

.PHONY: kustomize
kustomize: ## Verify kustomize is available through mise.
	$(MISE_ENV) $(MISE) run kustomize

.PHONY: controller-gen
controller-gen: ## Verify controller-gen is available through mise.
	$(MISE_ENV) $(MISE) run controller-gen

.PHONY: setup-envtest
setup-envtest: ## Download the binaries required for ENVTEST in the local bin directory.
	$(MISE_ENV) $(MISE) run setup-envtest

.PHONY: envtest
envtest: ## Verify setup-envtest is available through mise.
	$(MISE_ENV) $(MISE) run envtest

.PHONY: golangci-lint
golangci-lint: ## Verify golangci-lint is available through mise.
	$(MISE_ENV) $(MISE) run golangci-lint
