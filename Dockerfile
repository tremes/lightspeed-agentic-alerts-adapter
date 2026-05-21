FROM registry.access.redhat.com/ubi9/go-toolset:1.22 AS builder
WORKDIR /opt/app-root/src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /opt/app-root/alerts-adapter ./cmd/alerts-adapter

FROM registry.access.redhat.com/ubi9-micro:latest
COPY --from=builder /opt/app-root/alerts-adapter /usr/local/bin/alerts-adapter
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/alerts-adapter"]
