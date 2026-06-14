FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -p 1 -ldflags="-s -w" -o /payment-service ./cmd/payment-service

FROM alpine:3.20
COPY --from=builder /payment-service /payment-service
RUN apk add --no-cache wget && adduser -D appuser
USER appuser
EXPOSE 8081
ENTRYPOINT ["/payment-service"]
