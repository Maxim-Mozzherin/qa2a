# -----------------------------------------------------------------------------
# Stage 1: Build stage
# -----------------------------------------------------------------------------
FROM golang:alpine AS builder

RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /app

# Cache Go dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Compile static binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/iiko_parser .

# -----------------------------------------------------------------------------
# Stage 2: Minimal runtime image with poppler-utils
# -----------------------------------------------------------------------------
FROM alpine:3.20 AS runner

RUN apk add --no-cache ca-certificates tzdata poppler-utils curl

WORKDIR /app

# Copy compiled binary
COPY --from=builder /app/iiko_parser /app/iiko_parser

# Copy web static UI
COPY --from=builder /app/static /app/static

# Ensure temp directory exists for PDF OCR
RUN mkdir -p /app/temp

ENV PORT=8099

EXPOSE 8099

HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
  CMD curl -f http://localhost:8099/health || exit 1

ENTRYPOINT ["/app/iiko_parser"]
