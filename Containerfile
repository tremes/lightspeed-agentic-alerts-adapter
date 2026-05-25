FROM registry.access.redhat.com/ubi9/go-toolset:latest AS builder

USER 0
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -buildvcs=false -o ./alerts-adapter ./cmd/

FROM registry.access.redhat.com/ubi9/ubi-micro:latest
COPY --from=builder /build/alerts-adapter /alerts-adapter
ENTRYPOINT ["/alerts-adapter"]
