package main

import (
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
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type server struct {
	logger           *slog.Logger
	tracer           trace.Tracer
	evaluationsTotal metric.Int64Counter
}

func main() {
	cfg := config.LoadRiskServiceConfig()
	logger := logging.New(slog.LevelInfo)

	ctx := context.Background()
	shutdown, err := telemetry.Init(ctx, telemetry.Config{
		ServiceName:    "risk-service",
		ServiceVersion: cfg.ServiceVersion,
		OTLPEndpoint:   cfg.OTLPEndpoint,
	})
	if err != nil {
		logger.ErrorContext(ctx, "telemetry init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	meter := otel.GetMeterProvider().Meter("risk-service")
	evaluationsTotal, _ := meter.Int64Counter("risk_evaluations",
		metric.WithDescription("Total risk evaluations"),
	)

	srv := &server{
		logger:           logger,
		tracer:           otel.Tracer("risk-service"),
		evaluationsTotal: evaluationsTotal,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", health.LiveHandler())
	mux.HandleFunc("GET /health/ready", health.ReadyHandler())
	mux.HandleFunc("POST /evaluate", srv.handleEvaluate)

	handler := middleware.Chain(mux,
		middleware.RequestID,
		middleware.OTelHTTP("risk-service"),
		middleware.Logging(logger),
	)

	httpSrv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.InfoContext(ctx, "risk-service started", slog.String("port", cfg.Port))
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

func (s *server) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.tracer.Start(r.Context(), "risk.evaluate")
	defer span.End()

	var req models.RiskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid request")
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "invalid request"})
		return
	}

	span.SetAttributes(
		attribute.String("payment.id", req.PaymentID),
		attribute.String("merchant.id", req.MerchantID),
		attribute.Int64("payment.amount", req.Amount),
		attribute.String("payment.scenario", req.Scenario),
	)

	resp := evaluate(req)

	span.SetAttributes(
		attribute.String("risk.decision", resp.Decision),
	)
	if resp.Reason != "" {
		span.SetAttributes(attribute.String("risk.reason", resp.Reason))
	}

	s.evaluationsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("decision", resp.Decision),
	))

	logging.FromContext(ctx, s.logger).InfoContext(ctx, "risk evaluation",
		slog.String("payment_id", req.PaymentID),
		slog.String("decision", resp.Decision),
		slog.String("scenario", req.Scenario),
	)

	writeJSON(w, http.StatusOK, resp)
}

func evaluate(req models.RiskRequest) models.RiskResponse {
	if req.Scenario == "fraud" {
		return models.RiskResponse{Decision: "rejected", Reason: "fraud_detected"}
	}
	return models.RiskResponse{Decision: "approved"}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
