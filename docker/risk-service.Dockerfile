FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /risk-service ./cmd/risk-service

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /risk-service /risk-service
USER nonroot:nonroot
EXPOSE 8082
ENTRYPOINT ["/risk-service"]
