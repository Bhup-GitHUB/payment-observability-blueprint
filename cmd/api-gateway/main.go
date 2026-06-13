package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/config"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/health"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/logging"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/middleware"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/models"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/telemetry"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

//go:embed web
var webFS embed.FS

type server struct {
	cfg        config.APIGatewayConfig
	httpClient *http.Client
	logger     *slog.Logger
	tracer     trace.Tracer
}

func main() {
	cfg := config.LoadAPIGatewayConfig()
	logger := logging.New(slog.LevelInfo)

	ctx := context.Background()
	shutdown, err := telemetry.Init(ctx, telemetry.Config{
		ServiceName:    "api-gateway",
		ServiceVersion: cfg.ServiceVersion,
		OTLPEndpoint:   cfg.OTLPEndpoint,
	})
	if err != nil {
		logger.ErrorContext(ctx, "telemetry init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	srv := &server{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout:   30 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		logger: logger,
		tracer: otel.Tracer("api-gateway"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", health.LiveHandler())
	mux.HandleFunc("GET /health/ready", health.ReadyHandler())
	mux.HandleFunc("POST /api/payments", srv.handlePayment)

	webContent, err := fs.Sub(webFS, "web")
	if err != nil {
		logger.ErrorContext(ctx, "web fs error", slog.String("error", err.Error()))
		os.Exit(1)
	}
	mux.Handle("/", http.FileServer(http.FS(webContent)))

	handler := middleware.Chain(mux,
		middleware.RequestID,
		middleware.OTelHTTP("api-gateway"),
		middleware.Logging(logger),
	)

	httpSrv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.InfoContext(ctx, "api-gateway started", slog.String("port", cfg.Port))
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.ErrorContext(ctx, "server error", slog.String("error", err.Error()))
			os.Exit(1)
		}
	}()

	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	_ = shutdown(shutdownCtx)
}

func (s *server) handlePayment(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.tracer.Start(r.Context(), "POST /api/payments")
	defer span.End()

	var req models.PaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid request body")
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "invalid request body"})
		return
	}

	if err := validatePaymentRequest(&req); err != nil {
		span.SetStatus(codes.Error, err.Error())
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: err.Error()})
		return
	}

	span.SetAttributes(
		attribute.String("merchant.id", req.MerchantID),
		attribute.Int64("payment.amount", req.Amount),
		attribute.String("payment.currency", req.Currency),
		attribute.String("payment.scenario", req.Scenario),
	)

	start := time.Now()

	body, _ := json.Marshal(req)
	proxyReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.cfg.PaymentServiceURL+"/process", bytes.NewReader(body))
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "request creation failed")
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "internal error"})
		return
	}
	proxyReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(proxyReq)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "payment service unreachable")
		logging.FromContext(ctx, s.logger).ErrorContext(ctx, "payment service call failed",
			slog.String("error", err.Error()),
		)
		writeJSON(w, http.StatusServiceUnavailable, models.ErrorResponse{Error: "payment service unavailable"})
		return
	}
	defer resp.Body.Close()

	var payResp models.PaymentResponse
	if err := json.NewDecoder(resp.Body).Decode(&payResp); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid response")
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "internal error"})
		return
	}

	payResp.TraceID = span.SpanContext().TraceID().String()
	payResp.DurationMS = time.Since(start).Milliseconds()

	span.SetAttributes(
		attribute.String("payment.id", payResp.PaymentID),
		attribute.String("payment.status", payResp.Status),
	)

	if payResp.Status == "failed" {
		span.SetStatus(codes.Error, "payment failed")
	}

	logging.FromContext(ctx, s.logger).InfoContext(ctx, "payment processed",
		slog.String("payment_id", payResp.PaymentID),
		slog.String("status", payResp.Status),
		slog.Int64("duration_ms", payResp.DurationMS),
	)

	writeJSON(w, resp.StatusCode, payResp)
}

func validatePaymentRequest(req *models.PaymentRequest) error {
	if req.MerchantID == "" {
		return fmt.Errorf("merchant_id is required")
	}
	if req.Amount <= 0 {
		return fmt.Errorf("amount must be greater than zero")
	}
	switch req.Currency {
	case "INR", "USD", "EUR", "GBP":
	default:
		return fmt.Errorf("unsupported currency: %s", req.Currency)
	}
	switch req.Scenario {
	case "success", "slow", "failure", "fraud":
	default:
		return fmt.Errorf("unsupported scenario: %s", req.Scenario)
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
