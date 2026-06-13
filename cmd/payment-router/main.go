package main

import (
	"bytes"
	"context"
	"encoding/json"
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
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type server struct {
	cfg          config.RouterServiceConfig
	httpClient   *http.Client
	logger       *slog.Logger
	tracer       trace.Tracer
	bankRequests metric.Int64Counter
	bankSeconds  metric.Float64Histogram
}

func main() {
	cfg := config.LoadRouterServiceConfig()
	logger := logging.New(slog.LevelInfo)

	ctx := context.Background()
	shutdown, err := telemetry.Init(ctx, telemetry.Config{
		ServiceName:    "payment-router",
		ServiceVersion: cfg.ServiceVersion,
		OTLPEndpoint:   cfg.OTLPEndpoint,
	})
	if err != nil {
		logger.ErrorContext(ctx, "telemetry init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	meter := otel.GetMeterProvider().Meter("payment-router")
	bankRequests, _ := meter.Int64Counter("bank_requests",
		metric.WithDescription("Total requests forwarded to bank"),
	)
	bankSeconds, _ := meter.Float64Histogram("bank_request_duration_seconds",
		metric.WithDescription("Bank request round-trip latency"),
	)

	srv := &server{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout:   8 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		logger:       logger,
		tracer:       otel.Tracer("payment-router"),
		bankRequests: bankRequests,
		bankSeconds:  bankSeconds,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", health.LiveHandler())
	mux.HandleFunc("GET /health/ready", health.ReadyHandler())
	mux.HandleFunc("POST /route", srv.handleRoute)

	handler := middleware.Chain(mux,
		middleware.RequestID,
		middleware.OTelHTTP("payment-router"),
		middleware.Logging(logger),
	)

	httpSrv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.InfoContext(ctx, "payment-router started", slog.String("port", cfg.Port))
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

func (s *server) handleRoute(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.tracer.Start(r.Context(), "payment.route")
	defer span.End()

	var req models.RouteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid request")
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "invalid request"})
		return
	}

	span.SetAttributes(
		attribute.String("payment.id", req.PaymentID),
		attribute.String("merchant.id", req.MerchantID),
		attribute.String("payment.scenario", req.Scenario),
		attribute.String("bank.name", "demo-bank"),
	)

	authReq := models.AuthorizeRequest{
		PaymentID:  req.PaymentID,
		MerchantID: req.MerchantID,
		Amount:     req.Amount,
		Currency:   req.Currency,
		Scenario:   req.Scenario,
	}

	start := time.Now()
	authResp, err := s.callBank(ctx, authReq)
	elapsed := time.Since(start).Seconds()

	s.bankSeconds.Record(ctx, elapsed, metric.WithAttributes(
		attribute.String("bank", "demo-bank"),
	))

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "bank call failed")
		s.bankRequests.Add(ctx, 1, metric.WithAttributes(
			attribute.String("bank", "demo-bank"),
			attribute.String("status", "error"),
		))
		logging.FromContext(ctx, s.logger).ErrorContext(ctx, "bank call failed",
			slog.String("payment_id", req.PaymentID),
			slog.String("error", err.Error()),
		)
		writeJSON(w, http.StatusServiceUnavailable, models.ErrorResponse{Error: "bank unreachable"})
		return
	}

	s.bankRequests.Add(ctx, 1, metric.WithAttributes(
		attribute.String("bank", "demo-bank"),
		attribute.String("status", authResp.Status),
	))

	if authResp.Status == "failed" {
		span.SetStatus(codes.Error, "bank declined")
	}

	logging.FromContext(ctx, s.logger).InfoContext(ctx, "bank authorization complete",
		slog.String("payment_id", req.PaymentID),
		slog.String("status", authResp.Status),
		slog.Float64("elapsed_s", elapsed),
	)

	writeJSON(w, http.StatusOK, models.RouteResponse{
		BankRef: authResp.BankRef,
		Status:  authResp.Status,
	})
}

func (s *server) callBank(ctx context.Context, req models.AuthorizeRequest) (*models.AuthorizeResponse, error) {
	body, _ := json.Marshal(req)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.cfg.BankSimulatorURL+"/authorize", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var authResp models.AuthorizeResponse
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return nil, err
	}
	return &authResp, nil
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
