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
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/qa2a ./cmd/api

# -----------------------------------------------------------------------------
# Stage 2: Minimal runtime image
# -----------------------------------------------------------------------------
FROM alpine:3.20 AS runner

RUN apk add --no-cache ca-certificates tzdata curl

WORKDIR /app

# Copy compiled binary
COPY --from=builder /app/qa2a /app/qa2a

# Copy static assets and fonts
COPY --from=builder /app/web /app/web
COPY --from=builder /app/fonts /app/fonts

# Create directories for dynamic uploads
RUN mkdir -p /app/uploads/tickets

ENV GOFPDF_FONTPATH=/app/fonts
ENV PORT=8082

EXPOSE 8082

HEALTHCHECK --interval=10s --timeout=3s --retries=3 \
  CMD curl -f http://localhost:8082/health || exit 1

ENTRYPOINT ["/app/qa2a"]
