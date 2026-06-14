FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -p 1 -ldflags="-s -w" -o /notification-worker ./cmd/notification-worker

FROM alpine:3.20
COPY --from=builder /notification-worker /notification-worker
RUN apk add --no-cache wget && adduser -D appuser
USER appuser
ENTRYPOINT ["/notification-worker"]
