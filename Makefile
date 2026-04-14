IMG ?= ghcr.io/tfsync/tfsync:latest
NAMESPACE ?= tfsync-system

.PHONY: help
help:
	@grep -E '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

.PHONY: tidy
tidy: ## go mod tidy
	go mod tidy

.PHONY: generate
generate: ## regenerate deepcopy + CRDs (requires controller-gen)
	controller-gen object paths=./api/...
	controller-gen crd paths=./api/... output:crd:dir=config/crd/bases

.PHONY: build
build: ## build manager binary
	CGO_ENABLED=0 go build -trimpath -o bin/manager ./cmd/manager

.PHONY: build-cli
build-cli: ## build tfsync CLI
	CGO_ENABLED=0 go build -trimpath -o bin/tfsync ./cmd/tfsync

.PHONY: test
test: ## run unit tests
	go test ./...

.PHONY: docker-build
docker-build: ## build container image
	docker build -t $(IMG) .

.PHONY: docker-push
docker-push: ## push container image
	docker push $(IMG)

.PHONY: install
install: ## install CRD into the cluster
	kubectl apply -f config/crd/bases/

.PHONY: uninstall
uninstall: ## remove CRD from the cluster
	kubectl delete -f config/crd/bases/ --ignore-not-found

.PHONY: deploy
deploy: install ## deploy controller manifests to cluster
	kubectl apply -f config/manager/namespace.yaml
	kubectl apply -f config/rbac/
	kubectl apply -f config/manager/manager.yaml

.PHONY: undeploy
undeploy: ## tear down controller manifests
	kubectl delete -f config/manager/manager.yaml --ignore-not-found
	kubectl delete -f config/rbac/ --ignore-not-found
	kubectl delete -f config/manager/namespace.yaml --ignore-not-found

.PHONY: run
run: ## run manager locally against current kubecontext
	go run ./cmd/manager
