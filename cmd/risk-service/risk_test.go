package main

import (
	"testing"

	"github.com/Bhup-GitHUB/payment-observability-blueprint/internal/models"
)

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name             string
		req              models.RiskRequest
		wantDecision     string
	}{
		{
			name:         "fraud scenario is rejected",
			req:          models.RiskRequest{Scenario: "fraud", Amount: 1000},
			wantDecision: "rejected",
		},
		{
			name:         "success scenario is approved",
			req:          models.RiskRequest{Scenario: "success", Amount: 1000},
			wantDecision: "approved",
		},
		{
			name:         "slow scenario is approved",
			req:          models.RiskRequest{Scenario: "slow", Amount: 5000},
			wantDecision: "approved",
		},
		{
			name:         "failure scenario is approved by risk",
			req:          models.RiskRequest{Scenario: "failure", Amount: 3000},
			wantDecision: "approved",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := evaluate(tc.req)
			if resp.Decision != tc.wantDecision {
				t.Errorf("got decision %q, want %q", resp.Decision, tc.wantDecision)
			}
		})
	}
}
