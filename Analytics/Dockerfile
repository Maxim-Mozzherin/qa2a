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
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/analytics_server ./cmd/analytics_server

# -----------------------------------------------------------------------------
# Stage 2: Minimal runtime image
# -----------------------------------------------------------------------------
FROM alpine:3.20 AS runner

RUN apk add --no-cache ca-certificates tzdata curl && \
    adduser -D -u 1000 appuser

WORKDIR /app

# Copy compiled binary
COPY --from=builder /app/analytics_server /app/analytics_server

# Copy static assets and fonts
COPY --from=builder /app/web /app/web
COPY --from=builder /app/fonts /app/fonts

RUN chown -R appuser:appuser /app

USER appuser

ENV FONT_PATH=/app/fonts/DejaVuSans.ttf
ENV PORT=8096

EXPOSE 8096

HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
  CMD curl -f http://localhost:8096/analytics/login || exit 1

ENTRYPOINT ["/app/analytics_server"]
