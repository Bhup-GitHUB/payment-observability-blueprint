FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -p 1 -ldflags="-s -w" -o /payment-router ./cmd/payment-router

FROM alpine:3.20
COPY --from=builder /payment-router /payment-router
RUN apk add --no-cache wget && adduser -D appuser
USER appuser
EXPOSE 8083
ENTRYPOINT ["/payment-router"]
