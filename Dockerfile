# Build stage
FROM golang:1.25-alpine AS builder

ARG ALPINE_MIRROR=https://mirror.sgp1.digitalocean.com/alpine
RUN set -eux; \
    mirror="${ALPINE_MIRROR}"; \
    if [ -n "${mirror}" ]; then \
      tmp="$(mktemp)"; \
      sed "s|https://dl-cdn.alpinelinux.org/alpine|${mirror}|g" /etc/apk/repositories > "${tmp}"; \
      cat /etc/apk/repositories >> "${tmp}"; \
      mv "${tmp}" /etc/apk/repositories; \
    fi

WORKDIR /app

# Install git for go mod download
RUN apk add --no-cache git

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o olt-monitor ./cmd/main.go

# Runtime stage
FROM alpine:3.19

ARG ALPINE_MIRROR=https://mirror.sgp1.digitalocean.com/alpine
RUN set -eux; \
    mirror="${ALPINE_MIRROR}"; \
    if [ -n "${mirror}" ]; then \
      tmp="$(mktemp)"; \
      sed "s|https://dl-cdn.alpinelinux.org/alpine|${mirror}|g" /etc/apk/repositories > "${tmp}"; \
      cat /etc/apk/repositories >> "${tmp}"; \
      mv "${tmp}" /etc/apk/repositories; \
    fi

WORKDIR /app

# Install ca-certificates for HTTPS
RUN apk --no-cache add ca-certificates tzdata

# Copy binary from builder
COPY --from=builder /app/olt-monitor .

# Copy config
COPY --from=builder /app/config ./config

# Set timezone
ENV TZ=Asia/Jakarta

# Expose port
EXPOSE 8081

# Run
CMD ["./olt-monitor"]
