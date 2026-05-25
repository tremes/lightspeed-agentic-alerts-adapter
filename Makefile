BINARY_NAME := alerts-adapter
BIN_DIR := bin
CMD_DIR := ./cmd

.PHONY: build test lint clean image-build deploy

build:
	CGO_ENABLED=0 go build -o $(BIN_DIR)/$(BINARY_NAME) $(CMD_DIR)/

test:
	go test ./... -v -count=1

lint:
	golangci-lint run ./...

clean:
	rm -rf $(BIN_DIR)

image-build:
	podman build -f Containerfile -t lightspeed-agentic-alerts-adapter:latest .

deploy:
	kubectl apply -f manifests/
