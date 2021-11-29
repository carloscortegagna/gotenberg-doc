.PHONY: help
help: ## Show the help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-30s\033[0m %s\n", $$1, $$2}'

.PHONY: it
it: build ## Initialize the development environment

GOLANG_VERSION=1.17
APP_NAME=gotenberg-doc
APP_VERSION=0.1
APP_AUTHOR=carlo
APP_REPOSITORY=https://github.com:carloscortegagna/gotenberg-doc.git
DOCKER_REPOSITORY=carloscortegagna
GOLANGCI_LINT_VERSION=v1.42.0 # See https://github.com/golangci/golangci-lint/releases.

.PHONY: build
build: ## Build the Gotenberg's Docker image
	docker build \
	--build-arg GOLANG_VERSION=$(GOLANG_VERSION) \
	--build-arg APP_NAME=$(APP_NAME) \
	--build-arg APP_VERSION=$(APP_VERSION) \
	--build-arg APP_AUTHOR=$(APP_AUTHOR) \
	--build-arg APP_REPOSITORY=$(APP_REPOSITORY) \
	-t $(DOCKER_REPOSITORY)/gotenberg:7-$(APP_NAME)-$(APP_VERSION) \
	-f build/Dockerfile .

.PHONY: build-tests
build-tests: ## Build the tests' Docker image
	docker build \
	--build-arg GOLANG_VERSION=$(GOLANG_VERSION) \
	--build-arg DOCKER_REPOSITORY=$(DOCKER_REPOSITORY) \
	--build-arg APP_NAME=$(APP_NAME) \
	--build-arg APP_VERSION=$(APP_VERSION) \
	--build-arg GOLANGCI_LINT_VERSION=$(GOLANGCI_LINT_VERSION) \
	-t $(DOCKER_REPOSITORY)/gotenberg:7-$(APP_NAME)-$(APP_VERSION)-tests \
	-f test/Dockerfile .

.PHONY: tests
tests: ## Start the testing environment
	docker run --rm -it \
	-v $(PWD):/tests \
	$(DOCKER_REPOSITORY)/gotenberg:7-$(APP_NAME)-$(APP_VERSION)-tests \
	bash

.PHONY: tests-once
tests-once: ## Run the tests once (prefer the "tests" command while developing)
	docker run --rm  \
	-v $(PWD):/tests \
	$(DOCKER_REPOSITORY)/gotenberg:7-$(APP_NAME)-$(APP_VERSION)-tests \
	gotest

.PHONY: fmt
fmt: ## Format the code and "optimize" the dependencies
	go fmt ./...
	go mod tidy
