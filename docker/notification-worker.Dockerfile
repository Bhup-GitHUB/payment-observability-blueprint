FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /notification-worker ./cmd/notification-worker

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /notification-worker /notification-worker
USER nonroot:nonroot
ENTRYPOINT ["/notification-worker"]
