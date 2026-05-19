FROM registry.access.redhat.com/ubi9/go-toolset:1.25 AS builder

WORKDIR /opt/app-root/src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o lightspeed-agentic-alerts-adapter ./cmd/alerts-adapter/

FROM registry.access.redhat.com/ubi9/ubi-minimal:latest

COPY --from=builder /opt/app-root/src/lightspeed-agentic-alerts-adapter /usr/local/bin/lightspeed-agentic-alerts-adapter

USER 1001

ENTRYPOINT ["lightspeed-agentic-alerts-adapter"]
