FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /ledger-service ./cmd/ledger-service

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /ledger-service /ledger-service
USER nonroot:nonroot
EXPOSE 8085
ENTRYPOINT ["/ledger-service"]
