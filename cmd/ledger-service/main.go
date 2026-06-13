package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/config"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/db"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/health"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/logging"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/middleware"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/models"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/telemetry"
	_ "github.com/lib/pq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type dbChecker struct{ db *sql.DB }

func (d *dbChecker) Ready(ctx context.Context) error {
	return d.db.PingContext(ctx)
}

type server struct {
	db              *sql.DB
	logger          *slog.Logger
	tracer          trace.Tracer
	ledgerWrites    metric.Int64Counter
	ledgerDupes     metric.Int64Counter
	querySeconds    metric.Float64Histogram
}

func main() {
	cfg := config.LoadLedgerServiceConfig()
	logger := logging.New(slog.LevelInfo)

	ctx := context.Background()
	shutdown, err := telemetry.Init(ctx, telemetry.Config{
		ServiceName:    "ledger-service",
		ServiceVersion: cfg.ServiceVersion,
		OTLPEndpoint:   cfg.OTLPEndpoint,
	})
	if err != nil {
		logger.ErrorContext(ctx, "telemetry init failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	database, err := openDB(cfg.DatabaseURL)
	if err != nil {
		logger.ErrorContext(ctx, "db connect failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer database.Close()

	if err := migrateWithRetry(database, logger, ctx); err != nil {
		logger.ErrorContext(ctx, "migration failed", slog.String("error", err.Error()))
		os.Exit(1)
	}

	meter := otel.GetMeterProvider().Meter("ledger-service")
	ledgerWrites, _ := meter.Int64Counter("ledger_writes",
		metric.WithDescription("Total ledger write attempts"),
	)
	ledgerDupes, _ := meter.Int64Counter("ledger_duplicates",
		metric.WithDescription("Duplicate payment IDs rejected"),
	)
	querySeconds, _ := meter.Float64Histogram("db_query_duration_seconds",
		metric.WithDescription("Database query latency"),
	)

	srv := &server{
		db:           database,
		logger:       logger,
		tracer:       otel.Tracer("ledger-service"),
		ledgerWrites: ledgerWrites,
		ledgerDupes:  ledgerDupes,
		querySeconds: querySeconds,
	}

	checker := &dbChecker{db: database}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", health.LiveHandler())
	mux.HandleFunc("GET /health/ready", health.ReadyHandler(checker))
	mux.HandleFunc("POST /record", srv.handleRecord)

	handler := middleware.Chain(mux,
		middleware.RequestID,
		middleware.OTelHTTP("ledger-service"),
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
		logger.InfoContext(ctx, "ledger-service started", slog.String("port", cfg.Port))
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

func (s *server) handleRecord(w http.ResponseWriter, r *http.Request) {
	ctx, span := s.tracer.Start(r.Context(), "ledger.record")
	defer span.End()

	var req models.LedgerRequest
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
		attribute.String("payment.currency", req.Currency),
	)

	start := time.Now()
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO ledger_entries (payment_id, merchant_id, amount, currency, bank_ref)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (payment_id) DO NOTHING`,
		req.PaymentID, req.MerchantID, req.Amount, req.Currency, req.BankRef,
	)
	elapsed := time.Since(start).Seconds()
	s.querySeconds.Record(ctx, elapsed)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "db write failed")
		s.ledgerWrites.Add(ctx, 1, metric.WithAttributes(attribute.Bool("recorded", false)))
		logging.FromContext(ctx, s.logger).ErrorContext(ctx, "ledger write failed",
			slog.String("payment_id", req.PaymentID),
			slog.String("error", err.Error()),
		)
		writeJSON(w, http.StatusInternalServerError, models.ErrorResponse{Error: "write failed"})
		return
	}

	rows, _ := result.RowsAffected()
	recorded := rows > 0

	if !recorded {
		s.ledgerDupes.Add(ctx, 1)
	}
	s.ledgerWrites.Add(ctx, 1, metric.WithAttributes(attribute.Bool("recorded", recorded)))

	logging.FromContext(ctx, s.logger).InfoContext(ctx, "ledger entry",
		slog.String("payment_id", req.PaymentID),
		slog.Bool("recorded", recorded),
	)

	writeJSON(w, http.StatusOK, models.LedgerResponse{Recorded: recorded})
}

func openDB(dsn string) (*sql.DB, error) {
	database, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	database.SetMaxOpenConns(25)
	database.SetMaxIdleConns(5)
	database.SetConnMaxLifetime(5 * time.Minute)
	return database, nil
}

func migrateWithRetry(database *sql.DB, logger *slog.Logger, ctx context.Context) error {
	var err error
	for i := 0; i < 3; i++ {
		if err = db.RunMigrations(database); err == nil {
			return nil
		}
		logger.WarnContext(ctx, "migration attempt failed, retrying",
			slog.Int("attempt", i+1),
			slog.String("error", err.Error()),
		)
		time.Sleep(2 * time.Second)
	}
	return err
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
