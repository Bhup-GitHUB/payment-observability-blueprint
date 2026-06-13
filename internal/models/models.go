package models

import "time"

type PaymentRequest struct {
	MerchantID string `json:"merchant_id"`
	Amount     int64  `json:"amount"`
	Currency   string `json:"currency"`
	Scenario   string `json:"scenario"`
}

type PaymentResponse struct {
	PaymentID  string `json:"payment_id"`
	Status     string `json:"status"`
	TraceID    string `json:"trace_id,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Message    string `json:"message,omitempty"`
}

type RiskRequest struct {
	PaymentID  string `json:"payment_id"`
	MerchantID string `json:"merchant_id"`
	Amount     int64  `json:"amount"`
	Scenario   string `json:"scenario"`
}

type RiskResponse struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
}

type RouteRequest struct {
	PaymentID  string `json:"payment_id"`
	MerchantID string `json:"merchant_id"`
	Amount     int64  `json:"amount"`
	Currency   string `json:"currency"`
	Scenario   string `json:"scenario"`
}

type RouteResponse struct {
	BankRef string `json:"bank_ref"`
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

type AuthorizeRequest struct {
	PaymentID  string `json:"payment_id"`
	MerchantID string `json:"merchant_id"`
	Amount     int64  `json:"amount"`
	Currency   string `json:"currency"`
	Scenario   string `json:"scenario"`
}

type AuthorizeResponse struct {
	BankRef string `json:"bank_ref"`
	Status  string `json:"status"`
	Code    string `json:"code"`
}

type LedgerRequest struct {
	PaymentID  string `json:"payment_id"`
	MerchantID string `json:"merchant_id"`
	Amount     int64  `json:"amount"`
	Currency   string `json:"currency"`
	BankRef    string `json:"bank_ref"`
}

type LedgerResponse struct {
	Recorded bool `json:"recorded"`
}

type PaymentEvent struct {
	PaymentID    string            `json:"payment_id"`
	MerchantID   string            `json:"merchant_id"`
	Amount       int64             `json:"amount"`
	Currency     string            `json:"currency"`
	Status       string            `json:"status"`
	Timestamp    time.Time         `json:"timestamp"`
	TraceCarrier map[string]string `json:"trace_carrier"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}
