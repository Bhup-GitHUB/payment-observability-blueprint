package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand"
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
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type server struct {
	logger               *slog.Logger
	tracer               trace.Tracer
	authorizationsTotal  metric.Int64Counter
	authorizationSeconds metric.Float64Histogram
}

func main() {
	cfg := config.LoadBankSimulatorConfig()
	logger := logging.New(slog.LevelInfo)

	ctx := context.Background()
	shutdown, err := telemetry.Init(ctx, telemetry.Config{
		ServiceName:    "bank-simulator",
		ServiceVersion: cfg.ServiceVersion,
		OTLPEndpoint:   cfg.OTLPEndpoint,
	})
	if err != nil {
		logger.ErrorContext(ctx, "telemetry init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	meter := otel.GetMeterProvider().Meter("bank-simulator")
	authTotal, _ := meter.Int64Counter("bank_authorizations",
		metric.WithDescription("Total bank authorization attempts"),
	)
	authSeconds, _ := meter.Float64Histogram("bank_authorization_duration_seconds",
		metric.WithDescription("Bank authorization latency"),
	)

	srv := &server{
		logger:               logger,
		tracer:               otel.Tracer("bank-simulator"),
		authorizationsTotal:  authTotal,
		authorizationSeconds: authSeconds,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", health.LiveHandler())
	mux.HandleFunc("GET /health/ready", health.ReadyHandler())
	mux.HandleFunc("POST /authorize", srv.handleAuthorize)

	handler := middleware.Chain(mux,
		middleware.RequestID,
		middleware.OTelHTTP("bank-simulator"),
		middleware.Logging(logger),
	)

	httpSrv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      handler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.InfoContext(ctx, "bank-simulator started", slog.String("port", cfg.Port))
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

func (s *server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.tracer.Start(r.Context(), "bank.authorize")
	defer span.End()

	var req models.AuthorizeRequest
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
		attribute.String("bank.name", "demo-bank"),
	)

	start := time.Now()
	resp := s.authorize(ctx, req)
	elapsed := time.Since(start).Seconds()

	span.SetAttributes(
		attribute.String("bank.response_code", resp.Code),
		attribute.String("bank.ref", resp.BankRef),
	)

	if resp.Status == "failed" {
		span.SetStatus(codes.Error, "authorization declined")
		span.RecordError(errDeclined)
	}

	s.authorizationsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("status", resp.Status),
		attribute.String("code", resp.Code),
	))
	s.authorizationSeconds.Record(ctx, elapsed, metric.WithAttributes(
		attribute.String("status", resp.Status),
	))

	logging.FromContext(ctx, s.logger).InfoContext(ctx, "bank authorization",
		slog.String("payment_id", req.PaymentID),
		slog.String("status", resp.Status),
		slog.String("code", resp.Code),
		slog.Float64("elapsed_s", elapsed),
	)

	writeJSON(w, http.StatusOK, resp)
}

var errDeclined = errors.New("authorization declined by bank")

func (s *server) authorize(ctx context.Context, req models.AuthorizeRequest) models.AuthorizeResponse {
	bankRef := "bnk_" + uuid.New().String()[:8]

	switch req.Scenario {
	case "slow":
		delay := time.Duration(2000+rand.Intn(2000)) * time.Millisecond
		select {
		case <-time.After(delay):
		case <-ctx.Done():
		}
		return models.AuthorizeResponse{BankRef: bankRef, Status: "success", Code: "00"}

	case "failure":
		return models.AuthorizeResponse{BankRef: bankRef, Status: "failed", Code: "05"}

	default:
		time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
		return models.AuthorizeResponse{BankRef: bankRef, Status: "success", Code: "00"}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
