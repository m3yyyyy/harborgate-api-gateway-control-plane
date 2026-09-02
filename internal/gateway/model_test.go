package gateway

import (
	"testing"
	"time"
)

func TestEvaluatePolicy(t *testing.T) {
	services := []Service{{ID: "svc-deploy", BaseURL: "https://deploy.internal", Health: "healthy", LastChecked: time.Now()}}
	routes := []Route{{
		ID: "route-deploy", ServiceID: "svc-deploy", Name: "Deployments", PathPrefix: "/deployments",
		Methods: []string{"GET", "POST"}, AuthMode: "service-token", RateLimitPerMinute: 60,
		CanaryPercent: 20, Enabled: true, Revision: 3,
	}}

	tests := []struct {
		name     string
		input    SimulationRequest
		decision string
	}{
		{"allows valid service request", SimulationRequest{"POST", "/deployments/release", "service-token", 12, "req-100"}, "allow"},
		{"denies missing service identity", SimulationRequest{"POST", "/deployments", "authenticated", 12, "req-101"}, "deny"},
		{"denies method", SimulationRequest{"DELETE", "/deployments", "service-token", 12, "req-102"}, "deny"},
		{"denies rate limit", SimulationRequest{"GET", "/deployments", "service-token", 60, "req-103"}, "deny"},
		{"denies unknown route", SimulationRequest{"GET", "/unknown", "authenticated", 0, "req-104"}, "deny"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Evaluate(services, routes, tt.input)
			if err != nil {
				t.Fatalf("Evaluate returned error: %v", err)
			}
			if got.Decision != tt.decision {
				t.Fatalf("decision = %q, want %q", got.Decision, tt.decision)
			}
		})
	}
}

func TestPathBoundaryDoesNotOvermatch(t *testing.T) {
	services := []Service{{ID: "svc", BaseURL: "https://example.internal", Health: "healthy"}}
	routes := []Route{{ID: "route", ServiceID: "svc", PathPrefix: "/api", Methods: []string{"GET"}, AuthMode: "public", RateLimitPerMinute: 10, Enabled: true}}
	decision, err := Evaluate(services, routes, SimulationRequest{"GET", "/apiv2", "anonymous", 0, "req-boundary"})
	if err != nil {
		t.Fatal(err)
	}
	if decision.Decision != "deny" || decision.RouteID != "" {
		t.Fatalf("unexpected match: %#v", decision)
	}
}
