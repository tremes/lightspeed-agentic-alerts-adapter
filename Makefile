BINARY     := alerts-adapter
IMAGE      := quay.io/openshift-lightspeed/lightspeed-agentic-alerts-adapter
IMAGE_TAG  ?= latest
GO         := go
GOFLAGS    ?=

.PHONY: build test lint image clean

build:
	$(GO) build $(GOFLAGS) -o bin/$(BINARY) ./cmd/alerts-adapter

test:
	$(GO) test ./... -race -count=1

lint:
	golangci-lint run ./...

image:
	podman build -t $(IMAGE):$(IMAGE_TAG) .

clean:
	rm -rf bin/
