FROM golang:1.23-alpine AS builder

WORKDIR /workspace

# Copy go mod files
COPY go.mod go.mod
COPY go.sum go.sum

# Download dependencies
RUN go mod download

# Copy source code
COPY cmd/ cmd/
COPY api/ api/
COPY internal/ internal/

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -a -o manager ./cmd/main.go

# Runtime image
FROM alpine:3.19

WORKDIR /
COPY --from=builder /workspace/manager .

# Install porchctl for Nephio Porch workflow
# Note: In production, consider using Porch Go SDK instead of CLI
RUN apk add --no-cache kubectl

ENTRYPOINT ["/manager"]
