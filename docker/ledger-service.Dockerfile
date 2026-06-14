FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -p 1 -ldflags="-s -w" -o /ledger-service ./cmd/ledger-service

FROM alpine:3.20
COPY --from=builder /ledger-service /ledger-service
RUN apk add --no-cache wget && adduser -D appuser
USER appuser
EXPOSE 8085
ENTRYPOINT ["/ledger-service"]
