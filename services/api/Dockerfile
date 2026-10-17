# Multi-stage build for ClaimBridge Go API Orchestrator
FROM golang:1.22-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/server ./cmd/server

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S claimbridge && adduser -S claimbridge -G claimbridge

WORKDIR /app
COPY --from=builder /bin/server /app/server

USER claimbridge

EXPOSE 8080

ENTRYPOINT ["/app/server"]
