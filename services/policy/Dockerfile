# Multi-stage build for ClaimBridge Rust Policy Engine
FROM rust:1.77-alpine AS builder

WORKDIR /usr/src/claimbridge-policy

RUN apk add --no-cache musl-dev

COPY Cargo.toml Cargo.lock ./
COPY src ./src

RUN cargo build --release

FROM alpine:3.19

RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S claimbridge && adduser -S claimbridge -G claimbridge

WORKDIR /app
COPY --from=builder /usr/src/claimbridge-policy/target/release/claimbridge-policy /app/claimbridge-policy

USER claimbridge

ENTRYPOINT ["/app/claimbridge-policy"]
