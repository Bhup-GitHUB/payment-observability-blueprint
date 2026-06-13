FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bank-simulator ./cmd/bank-simulator

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /bank-simulator /bank-simulator
USER nonroot:nonroot
EXPOSE 8084
ENTRYPOINT ["/bank-simulator"]
