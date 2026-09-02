package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/m3yyyyy/harborgate-api-gateway-control-plane/internal/store"
)

func testServer(t *testing.T, token string) http.Handler {
	t.Helper()
	database, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := database.Seed(); err != nil {
		t.Fatal(err)
	}
	return New(Config{Store: database, AdminToken: token, Version: "test", Commit: "test", Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
}

func TestHealthAndSimulation(t *testing.T) {
	handler := testServer(t, "")

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d, body = %s", health.Code, health.Body.String())
	}

	body, _ := json.Marshal(map[string]any{
		"method": "GET", "path": "/identity/users", "identity": "authenticated",
		"requestsInLastMinute": 12, "requestId": "test-request",
	})
	simulation := httptest.NewRecorder()
	handler.ServeHTTP(simulation, httptest.NewRequest(http.MethodPost, "/api/v1/simulate", bytes.NewReader(body)))
	if simulation.Code != http.StatusOK {
		t.Fatalf("simulation status = %d, body = %s", simulation.Code, simulation.Body.String())
	}
	var result map[string]any
	if err := json.Unmarshal(simulation.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["decision"] != "allow" {
		t.Fatalf("decision = %v, want allow", result["decision"])
	}
}

func TestMutationRequiresToken(t *testing.T) {
	handler := testServer(t, "known-token")
	body := bytes.NewBufferString(`{"actor":"reviewer","decision":"approved"}`)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/changes/chg-1042/decision", body))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
}
