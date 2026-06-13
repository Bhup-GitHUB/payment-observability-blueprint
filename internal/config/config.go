package config

import "os"

type APIGatewayConfig struct {
	Port              string
	PaymentServiceURL string
	OTLPEndpoint      string
	ServiceVersion    string
}

type PaymentServiceConfig struct {
	Port             string
	RiskServiceURL   string
	RouterServiceURL string
	LedgerServiceURL string
	NATSUrl          string
	OTLPEndpoint     string
	ServiceVersion   string
}

type RiskServiceConfig struct {
	Port           string
	OTLPEndpoint   string
	ServiceVersion string
}

type RouterServiceConfig struct {
	Port             string
	BankSimulatorURL string
	OTLPEndpoint     string
	ServiceVersion   string
}

type BankSimulatorConfig struct {
	Port           string
	OTLPEndpoint   string
	ServiceVersion string
}

type LedgerServiceConfig struct {
	Port           string
	DatabaseURL    string
	OTLPEndpoint   string
	ServiceVersion string
}

type NotificationWorkerConfig struct {
	NATSUrl        string
	OTLPEndpoint   string
	ServiceVersion string
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func LoadAPIGatewayConfig() APIGatewayConfig {
	return APIGatewayConfig{
		Port:              getenv("PORT", "8080"),
		PaymentServiceURL: getenv("PAYMENT_SERVICE_URL", "http://payment-service:8081"),
		OTLPEndpoint:      getenv("OTLP_ENDPOINT", "otel-collector:4317"),
		ServiceVersion:    getenv("SERVICE_VERSION", "0.1.0"),
	}
}

func LoadPaymentServiceConfig() PaymentServiceConfig {
	return PaymentServiceConfig{
		Port:             getenv("PORT", "8081"),
		RiskServiceURL:   getenv("RISK_SERVICE_URL", "http://risk-service:8082"),
		RouterServiceURL: getenv("ROUTER_SERVICE_URL", "http://payment-router:8083"),
		LedgerServiceURL: getenv("LEDGER_SERVICE_URL", "http://ledger-service:8085"),
		NATSUrl:          getenv("NATS_URL", "nats://nats:4222"),
		OTLPEndpoint:     getenv("OTLP_ENDPOINT", "otel-collector:4317"),
		ServiceVersion:   getenv("SERVICE_VERSION", "0.1.0"),
	}
}

func LoadRiskServiceConfig() RiskServiceConfig {
	return RiskServiceConfig{
		Port:           getenv("PORT", "8082"),
		OTLPEndpoint:   getenv("OTLP_ENDPOINT", "otel-collector:4317"),
		ServiceVersion: getenv("SERVICE_VERSION", "0.1.0"),
	}
}

func LoadRouterServiceConfig() RouterServiceConfig {
	return RouterServiceConfig{
		Port:             getenv("PORT", "8083"),
		BankSimulatorURL: getenv("BANK_SIMULATOR_URL", "http://bank-simulator:8084"),
		OTLPEndpoint:     getenv("OTLP_ENDPOINT", "otel-collector:4317"),
		ServiceVersion:   getenv("SERVICE_VERSION", "0.1.0"),
	}
}

func LoadBankSimulatorConfig() BankSimulatorConfig {
	return BankSimulatorConfig{
		Port:           getenv("PORT", "8084"),
		OTLPEndpoint:   getenv("OTLP_ENDPOINT", "otel-collector:4317"),
		ServiceVersion: getenv("SERVICE_VERSION", "0.1.0"),
	}
}

func LoadLedgerServiceConfig() LedgerServiceConfig {
	return LedgerServiceConfig{
		Port:           getenv("PORT", "8085"),
		DatabaseURL:    getenv("DATABASE_URL", "postgres://ledger:ledger@postgres:5432/ledger?sslmode=disable"),
		OTLPEndpoint:   getenv("OTLP_ENDPOINT", "otel-collector:4317"),
		ServiceVersion: getenv("SERVICE_VERSION", "0.1.0"),
	}
}

func LoadNotificationWorkerConfig() NotificationWorkerConfig {
	return NotificationWorkerConfig{
		NATSUrl:        getenv("NATS_URL", "nats://nats:4222"),
		OTLPEndpoint:   getenv("OTLP_ENDPOINT", "otel-collector:4317"),
		ServiceVersion: getenv("SERVICE_VERSION", "0.1.0"),
	}
}
