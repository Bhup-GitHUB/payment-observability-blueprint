FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -p 1 -ldflags="-s -w" -o /api-gateway ./cmd/api-gateway

FROM alpine:3.20
COPY --from=builder /api-gateway /api-gateway
RUN apk add --no-cache wget && adduser -D appuser
USER appuser
EXPOSE 8080
ENTRYPOINT ["/api-gateway"]
