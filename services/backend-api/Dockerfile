# Build stage — repo root is the build context
FROM golang:1.25-alpine AS builder

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY api/ api/
COPY services/backend-api/ services/backend-api/

RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o backend-api \
    ./services/backend-api/

# Run stage — distroless/base for SQLite file access
FROM gcr.io/distroless/base:nonroot
WORKDIR /
COPY --from=builder /workspace/backend-api .
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/backend-api"]
