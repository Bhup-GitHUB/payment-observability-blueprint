package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/logging"
	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/models"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric/noop"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"log/slog"
)

func init() {
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
}

func newTestServer() *server {
	mp := noop.NewMeterProvider()
	meter := mp.Meter("test")
	authTotal, _ := meter.Int64Counter("bank_authorizations")
	authSeconds, _ := meter.Float64Histogram("bank_authorization_duration_seconds")

	return &server{
		logger:               logging.New(slog.LevelError),
		tracer:               otel.Tracer("test"),
		authorizationsTotal:  authTotal,
		authorizationSeconds: authSeconds,
	}
}

func TestAuthorize_Success(t *testing.T) {
	srv := newTestServer()
	resp := srv.authorize(context.Background(), models.AuthorizeRequest{
		PaymentID: "pay_test",
		Scenario:  "success",
	})
	if resp.Status != "success" {
		t.Errorf("expected success, got %s", resp.Status)
	}
	if resp.Code != "00" {
		t.Errorf("expected code 00, got %s", resp.Code)
	}
}

func TestAuthorize_Failure(t *testing.T) {
	srv := newTestServer()
	resp := srv.authorize(context.Background(), models.AuthorizeRequest{
		PaymentID: "pay_test",
		Scenario:  "failure",
	})
	if resp.Status != "failed" {
		t.Errorf("expected failed, got %s", resp.Status)
	}
	if resp.Code != "05" {
		t.Errorf("expected code 05, got %s", resp.Code)
	}
}

func TestHandleAuthorize_HTTPLayer(t *testing.T) {
	srv := newTestServer()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /authorize", srv.handleAuthorize)

	body, _ := json.Marshal(models.AuthorizeRequest{
		PaymentID: "pay_http_test",
		Scenario:  "success",
	})
	req := httptest.NewRequest(http.MethodPost, "/authorize", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	var resp models.AuthorizeResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal("failed to decode response:", err)
	}
	if resp.Status != "success" {
		t.Errorf("expected success, got %s", resp.Status)
	}
}
