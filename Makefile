BINARY_NAME := lightspeed-agentic-alerts-adapter
IMAGE_REPO ?= quay.io/openshift-lightspeed/$(BINARY_NAME)
IMAGE_TAG ?= latest

.PHONY: build test lint image-build

build:
	go build -o $(BINARY_NAME) ./cmd/alerts-adapter/

test:
	go test ./...

lint:
	go vet ./...

image-build:
	podman build -t $(IMAGE_REPO):$(IMAGE_TAG) .
