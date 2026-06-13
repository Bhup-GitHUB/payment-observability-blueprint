FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /payment-router ./cmd/payment-router

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /payment-router /payment-router
USER nonroot:nonroot
EXPOSE 8083
ENTRYPOINT ["/payment-router"]
