package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/config"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/logging"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/models"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/natsprop"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/telemetry"
	"github.com/nats-io/nats.go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

type worker struct {
	logger               *slog.Logger
	tracer               trace.Tracer
	notificationsTotal   metric.Int64Counter
}

func main() {
	cfg := config.LoadNotificationWorkerConfig()
	logger := logging.New(slog.LevelInfo)

	ctx := context.Background()
	shutdown, err := telemetry.Init(ctx, telemetry.Config{
		ServiceName:    "notification-worker",
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

	meter := otel.GetMeterProvider().Meter("notification-worker")
	notificationsTotal, _ := meter.Int64Counter("notifications_sent",
		metric.WithDescription("Total notifications sent"),
	)

	w := &worker{
		logger:             logger,
		tracer:             otel.Tracer("notification-worker"),
		notificationsTotal: notificationsTotal,
	}

	sub, err := nc.Subscribe("payments.completed", w.handleEvent)
	if err != nil {
		logger.ErrorContext(ctx, "nats subscribe failed", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer sub.Unsubscribe()

	logger.InfoContext(ctx, "notification-worker started, listening on payments.completed")

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = shutdown(shutdownCtx)
}

func (w *worker) handleEvent(msg *nats.Msg) {
	var event models.PaymentEvent
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		w.logger.Error("failed to unmarshal payment event", slog.String("error", err.Error()))
		return
	}

	parentCtx := natsprop.Extract(context.Background(), event.TraceCarrier)
	ctx, span := w.tracer.Start(parentCtx, "notification.send",
		trace.WithSpanKind(trace.SpanKindConsumer),
	)
	defer span.End()

	span.SetAttributes(
		attribute.String("payment.id", event.PaymentID),
		attribute.String("merchant.id", event.MerchantID),
		attribute.String("payment.status", event.Status),
	)

	time.Sleep(10 * time.Millisecond)

	if err := w.sendNotification(ctx, event); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "notification failed")
		w.notificationsTotal.Add(ctx, 1, metric.WithAttributes(
			attribute.String("status", "failed"),
		))
		return
	}

	w.notificationsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("status", "sent"),
	))

	logging.FromContext(ctx, w.logger).InfoContext(ctx, "notification sent",
		slog.String("payment_id", event.PaymentID),
		slog.String("merchant_id", event.MerchantID),
		slog.String("status", event.Status),
	)
}

func (w *worker) sendNotification(ctx context.Context, event models.PaymentEvent) error {
	logging.FromContext(ctx, w.logger).InfoContext(ctx, "dispatching notification",
		slog.String("payment_id", event.PaymentID),
		slog.String("type", "payment_completed"),
	)
	return nil
}

func connectNATS(url string, logger *slog.Logger, ctx context.Context) (*nats.Conn, error) {
	var nc *nats.Conn
	var err error
	for i := 0; i < 5; i++ {
		nc, err = nats.Connect(url)
		if err == nil {
			return nc, nil
		}
		logger.WarnContext(ctx, "nats connect attempt failed, retrying",
			slog.Int("attempt", i+1),
			slog.String("error", err.Error()),
		)
		time.Sleep(2 * time.Second)
	}
	return nil, err
}
