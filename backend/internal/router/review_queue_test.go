package router

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/config"
	"github.com/blueship581/railway-bridge-defect-priority/backend/internal/database"
	"log/slog"
)

func newSmokeEngine(t *testing.T) http.Handler {
	t.Helper()
	cfg := config.Config{
		AppName: "test-app", Environment: "test", Port: "18099",
		DatabaseDriver: "sqlite", DatabaseDSN: fmt.Sprintf("file:router-smoke-%d?mode=memory&cache=shared", time.Now().UnixNano()),
		JWTSecret: "test-secret-at-least-16-chars", TokenTTL: time.Hour, RequestLimit: 1000,
		StartupTimeout: 30 * time.Second, ShutdownTimeout: 5 * time.Second,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second,
	}
	db, _, err := database.Open(context.Background(), cfg, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	// Registering the engine panics when route tree segments conflict.
	return New(cfg, db, nil, slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
}

func loginToken(t *testing.T, engine http.Handler, username string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": "Admin123!"})
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("login %s: status %d body %s", username, response.Code, response.Body.String())
	}
	var envelope struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || envelope.Data.Token == "" {
		t.Fatalf("decode login response: %v (%s)", err, response.Body.String())
	}
	return envelope.Data.Token
}

func TestReviewQueueRoutingAndAccessControl(t *testing.T) {
	engine := newSmokeEngine(t)
	operatorToken := loginToken(t, engine, "operator")
	reviewerToken := loginToken(t, engine, "reviewer")

	// Unauthenticated call is rejected.
	response := httptest.NewRecorder()
	engine.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/priority-review-queue", nil))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("queue without token: want 401, got %d", response.Code)
	}

	// Operators are below reviewer and must not access the queue.
	response = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/priority-review-queue", nil)
	request.Header.Set("Authorization", "Bearer "+operatorToken)
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("operator queue access: want 403, got %d body %s", response.Code, response.Body.String())
	}

	// Reviewers get the seeded draft prepared by operator.
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/priority-review-queue", nil)
	request.Header.Set("Authorization", "Bearer "+reviewerToken)
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("reviewer queue: want 200, got %d body %s", response.Code, response.Body.String())
	}
	var queue struct {
		Data struct {
			Items []struct {
				Code            string   `json:"code"`
				PreparedBy      string   `json:"preparedBy"`
				SuggestedLevel  string   `json:"suggestedLevel"`
				WaitingHours    float64  `json:"waitingHours"`
				OrderingReasons []string `json:"orderingReasons"`
			} `json:"items"`
			ExcludedOwn int64  `json:"excludedOwn"`
			EmptyReason string `json:"emptyReason"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &queue); err != nil {
		t.Fatalf("decode queue: %v (%s)", err, response.Body.String())
	}
	if len(queue.Data.Items) != 1 || queue.Data.Items[0].Code != "PD-001" || queue.Data.Items[0].PreparedBy != "operator" {
		t.Fatalf("reviewer should see only the operator draft: %s", response.Body.String())
	}
	if queue.Data.Items[0].SuggestedLevel != "observe" || queue.Data.Items[0].WaitingHours < 0 || len(queue.Data.Items[0].OrderingReasons) == 0 {
		t.Fatalf("queue item missing suggestion/waiting/reason: %s", response.Body.String())
	}

	// Invalid suggested level is a 400.
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/priority-review-queue?suggestedLevel=bogus", nil)
	request.Header.Set("Authorization", "Bearer "+reviewerToken)
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("bad filter: want 400, got %d", response.Code)
	}

	// /priorities/:id must still resolve to the get handler, not the queue.
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/api/priorities/1", nil)
	request.Header.Set("Authorization", "Bearer "+reviewerToken)
	engine.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("get by id must keep working: want 200, got %d body %s", response.Code, response.Body.String())
	}
}
