package natsprop_test

import (
	"context"
	"testing"

	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/natsprop"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func init() {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
}

func TestInjectExtractRoundTrip(t *testing.T) {
	tracer := otel.Tracer("test")
	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	originalTraceID := span.SpanContext().TraceID().String()

	carrier := natsprop.Inject(ctx)
	if len(carrier) == 0 {
		t.Fatal("expected non-empty carrier after inject")
	}

	recovered := natsprop.Extract(context.Background(), carrier)
	recoveredSpan := otel.Tracer("test2")
	_, child := recoveredSpan.Start(recovered, "child")
	defer child.End()

	if child.SpanContext().TraceID().String() != originalTraceID {
		t.Errorf("trace ID mismatch: got %s, want %s",
			child.SpanContext().TraceID().String(), originalTraceID)
	}
}

func TestExtractEmptyCarrier(t *testing.T) {
	ctx := natsprop.Extract(context.Background(), map[string]string{})
	if ctx == nil {
		t.Fatal("expected non-nil context from empty carrier")
	}
}
