FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /api-gateway ./cmd/api-gateway

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /api-gateway /api-gateway
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/api-gateway"]
