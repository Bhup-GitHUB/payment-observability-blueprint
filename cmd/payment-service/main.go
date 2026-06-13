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
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/natsprop"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/telemetry"
	"github.com/google/uuid"
	natsgo "github.com/nats-io/nats.go"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type server struct {
	cfg              config.PaymentServiceConfig
	httpClient       *http.Client
	nc               *natsgo.Conn
	logger           *slog.Logger
	tracer           trace.Tracer
	paymentsTotal    metric.Int64Counter
	paymentSeconds   metric.Float64Histogram
}

func main() {
	cfg := config.LoadPaymentServiceConfig()
	logger := logging.New(slog.LevelInfo)

	ctx := context.Background()
	shutdown, err := telemetry.Init(ctx, telemetry.Config{
		ServiceName:    "payment-service",
		ServiceVersion: cfg.ServiceVersion,
		OTLPEndpoint:   cfg.OTLPEndpoint,
	})
	if err != nil {
		logger.ErrorContext(ctx, "telemetry init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	nc, err := connectNATS(cfg.NATSUrl, logger, ctx)
	if err != nil {
		logger.ErrorContext(ctx, "nats connect failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer nc.Drain()

	meter := otel.GetMeterProvider().Meter("payment-service")
	paymentsTotal, _ := meter.Int64Counter("payment_requests",
		metric.WithDescription("Total payment requests processed"),
	)
	paymentSeconds, _ := meter.Float64Histogram("payment_duration_seconds",
		metric.WithDescription("Payment processing end-to-end latency"),
	)

	srv := &server{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout:   15 * time.Second,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
		nc:             nc,
		logger:         logger,
		tracer:         otel.Tracer("payment-service"),
		paymentsTotal:  paymentsTotal,
		paymentSeconds: paymentSeconds,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", health.LiveHandler())
	mux.HandleFunc("GET /health/ready", health.ReadyHandler())
	mux.HandleFunc("POST /process", srv.handleProcess)

	handler := middleware.Chain(mux,
		middleware.RequestID,
		middleware.OTelHTTP("payment-service"),
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
		logger.InfoContext(ctx, "payment-service started", slog.String("port", cfg.Port))
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

func (s *server) handleProcess(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.tracer.Start(r.Context(), "payment.process")
	defer span.End()

	var req models.PaymentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "invalid request")
		writeJSON(w, http.StatusBadRequest, models.ErrorResponse{Error: "invalid request"})
		return
	}

	paymentID := "pay_" + uuid.New().String()[:12]
	start := time.Now()

	span.SetAttributes(
		attribute.String("payment.id", paymentID),
		attribute.String("merchant.id", req.MerchantID),
		attribute.Int64("payment.amount", req.Amount),
		attribute.String("payment.currency", req.Currency),
		attribute.String("payment.scenario", req.Scenario),
	)

	log := logging.FromContext(ctx, s.logger).With(
		slog.String("payment_id", paymentID),
		slog.String("merchant_id", req.MerchantID),
		slog.String("scenario", req.Scenario),
	)

	riskResp, err := s.callRisk(ctx, models.RiskRequest{
		PaymentID:  paymentID,
		MerchantID: req.MerchantID,
		Amount:     req.Amount,
		Scenario:   req.Scenario,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "risk service error")
		log.ErrorContext(ctx, "risk service call failed", slog.String("error", err.Error()))
		s.paymentsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "error")))
		writeJSON(w, http.StatusServiceUnavailable, models.ErrorResponse{Error: "risk service unavailable"})
		return
	}

	span.SetAttributes(attribute.String("risk.decision", riskResp.Decision))

	if riskResp.Decision == "rejected" {
		log.InfoContext(ctx, "payment rejected by risk",
			slog.String("reason", riskResp.Reason),
		)
		s.paymentsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "rejected")))
		s.paymentSeconds.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
			attribute.String("status", "rejected"),
		))
		writeJSON(w, http.StatusOK, models.PaymentResponse{
			PaymentID: paymentID,
			Status:    "rejected",
			Message:   riskResp.Reason,
		})
		return
	}

	routeResp, err := s.callRouter(ctx, models.RouteRequest{
		PaymentID:  paymentID,
		MerchantID: req.MerchantID,
		Amount:     req.Amount,
		Currency:   req.Currency,
		Scenario:   req.Scenario,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "router error")
		log.ErrorContext(ctx, "router call failed", slog.String("error", err.Error()))
		s.paymentsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "error")))
		writeJSON(w, http.StatusServiceUnavailable, models.ErrorResponse{Error: "router unavailable"})
		return
	}

	span.SetAttributes(attribute.String("bank.ref", routeResp.BankRef))

	if routeResp.Status == "failed" {
		span.SetStatus(codes.Error, "bank declined")
		log.WarnContext(ctx, "payment declined by bank")
		s.paymentsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "failed")))
		s.paymentSeconds.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(
			attribute.String("status", "failed"),
		))
		writeJSON(w, http.StatusOK, models.PaymentResponse{
			PaymentID: paymentID,
			Status:    "failed",
			Message:   "payment declined by bank",
		})
		return
	}

	if _, err := s.callLedger(ctx, models.LedgerRequest{
		PaymentID:  paymentID,
		MerchantID: req.MerchantID,
		Amount:     req.Amount,
		Currency:   req.Currency,
		BankRef:    routeResp.BankRef,
	}); err != nil {
		log.ErrorContext(ctx, "ledger write failed", slog.String("error", err.Error()))
	}

	if err := s.publishEvent(ctx, models.PaymentEvent{
		PaymentID:    paymentID,
		MerchantID:   req.MerchantID,
		Amount:       req.Amount,
		Currency:     req.Currency,
		Status:       "completed",
		Timestamp:    time.Now(),
		TraceCarrier: natsprop.Inject(ctx),
	}); err != nil {
		log.WarnContext(ctx, "event publish failed", slog.String("error", err.Error()))
	}

	elapsed := time.Since(start).Seconds()
	s.paymentsTotal.Add(ctx, 1, metric.WithAttributes(attribute.String("status", "completed")))
	s.paymentSeconds.Record(ctx, elapsed, metric.WithAttributes(attribute.String("status", "completed")))

	log.InfoContext(ctx, "payment completed",
		slog.String("bank_ref", routeResp.BankRef),
		slog.Float64("elapsed_s", elapsed),
	)

	writeJSON(w, http.StatusOK, models.PaymentResponse{
		PaymentID: paymentID,
		Status:    "completed",
	})
}

func (s *server) callRisk(ctx context.Context, req models.RiskRequest) (*models.RiskResponse, error) {
	return post[models.RiskResponse](ctx, s.httpClient, s.cfg.RiskServiceURL+"/evaluate", req)
}

func (s *server) callRouter(ctx context.Context, req models.RouteRequest) (*models.RouteResponse, error) {
	return post[models.RouteResponse](ctx, s.httpClient, s.cfg.RouterServiceURL+"/route", req)
}

func (s *server) callLedger(ctx context.Context, req models.LedgerRequest) (*models.LedgerResponse, error) {
	return post[models.LedgerResponse](ctx, s.httpClient, s.cfg.LedgerServiceURL+"/record", req)
}

func (s *server) publishEvent(ctx context.Context, event models.PaymentEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return s.nc.Publish("payments.completed", data)
}

func post[T any](ctx context.Context, client *http.Client, url string, body any) (*T, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var result T
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

func connectNATS(url string, logger *slog.Logger, ctx context.Context) (*natsgo.Conn, error) {
	var nc *natsgo.Conn
	var err error
	for i := 0; i < 5; i++ {
		nc, err = natsgo.Connect(url)
		if err == nil {
			return nc, nil
		}
		logger.WarnContext(ctx, "nats connect attempt failed",
			slog.Int("attempt", i+1),
			slog.String("error", err.Error()),
		)
		time.Sleep(2 * time.Second)
	}
	return nil, err
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
