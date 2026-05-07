# Build stage — repo root is the build context
FROM golang:1.26-alpine AS builder

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY api/ api/
COPY services/diagnostics-worker/ services/diagnostics-worker/

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o diagnostics-worker \
    ./services/diagnostics-worker/

# Run stage
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/diagnostics-worker .
USER 65532:65532
ENTRYPOINT ["/diagnostics-worker"]
