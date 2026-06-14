FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -p 1 -ldflags="-s -w" -o /bank-simulator ./cmd/bank-simulator

FROM alpine:3.20
COPY --from=builder /bank-simulator /bank-simulator
RUN apk add --no-cache wget && adduser -D appuser
USER appuser
EXPOSE 8084
ENTRYPOINT ["/bank-simulator"]
