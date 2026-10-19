# Multi-stage build for ClaimBridge Go API Orchestrator
FROM golang:alpine AS builder

WORKDIR /app

RUN apk add --no-cache git ca-certificates

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/seed ./cmd/seed && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /bin/readiness ./cmd/readiness

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata wget && \
    addgroup -S claimbridge && adduser -S -G claimbridge claimbridge

WORKDIR /app
COPY --from=builder /bin/server /app/server
COPY --from=builder /bin/seed /app/seed
COPY --from=builder /bin/readiness /app/readiness

USER claimbridge

EXPOSE 8080

ENTRYPOINT ["/app/server"]
