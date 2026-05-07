# Build stage — repo root is the build context
FROM golang:1.26-alpine AS builder

WORKDIR /workspace

# Cache module downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy only what this service needs
COPY api/ api/
COPY services/cluster-watcher/ services/cluster-watcher/

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o cluster-watcher \
    ./services/cluster-watcher/

# Run stage
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/cluster-watcher .
USER 65532:65532
ENTRYPOINT ["/cluster-watcher"]
