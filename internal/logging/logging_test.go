package logging_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/logging"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func init() {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
}

func TestFromContextWithActiveSpan(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))

	_, span := otel.Tracer("test").Start(context.Background(), "op")
	ctx := context.Background()
	traceCtx, _ := otel.Tracer("test").Start(ctx, "op2")
	_ = span
	_ = traceCtx

	logger := logging.FromContext(traceCtx, base)
	logger.InfoContext(traceCtx, "test message")

	var record map[string]any
	if err := json.NewDecoder(&buf).Decode(&record); err != nil {
		t.Fatal("failed to decode log line:", err)
	}
	if _, ok := record["trace_id"]; !ok {
		t.Error("expected trace_id in log record")
	}
	if _, ok := record["span_id"]; !ok {
		t.Error("expected span_id in log record")
	}
}

func TestFromContextWithoutSpan(t *testing.T) {
	var buf bytes.Buffer
	base := slog.New(slog.NewJSONHandler(&buf, nil))

	logger := logging.FromContext(context.Background(), base)
	logger.Info("no span")

	var record map[string]any
	if err := json.NewDecoder(&buf).Decode(&record); err != nil {
		t.Fatal("failed to decode log line:", err)
	}
	if _, ok := record["trace_id"]; ok {
		t.Error("expected no trace_id without an active span")
	}
}
