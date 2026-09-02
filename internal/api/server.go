package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/m3yyyyy/harborgate-api-gateway-control-plane/internal/gateway"
	"github.com/m3yyyyy/harborgate-api-gateway-control-plane/internal/store"
)

type Config struct {
	Store      *store.Store
	AdminToken string
	WebDir     string
	Version    string
	Commit     string
	Logger     *slog.Logger
}

type server struct {
	store      *store.Store
	adminToken string
	webDir     string
	version    string
	commit     string
	logger     *slog.Logger
}

func New(config Config) http.Handler {
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	s := &server{
		store: config.Store, adminToken: config.AdminToken, webDir: config.WebDir,
		version: config.Version, commit: config.Commit, logger: config.Logger,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.HandleFunc("GET /api/v1/dashboard", s.dashboard)
	mux.HandleFunc("POST /api/v1/simulate", s.simulate)
	mux.HandleFunc("POST /api/v1/changes/{id}/decision", s.decide)
	mux.HandleFunc("POST /api/v1/changes/{id}/apply", s.apply)
	mux.HandleFunc("/", s.static)
	return s.middleware(mux)
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	database := "connected"
	if err := s.store.Ping(ctx); err != nil {
		database = "unavailable"
	}
	auditValid, err := s.store.VerifyAuditChain()
	if err != nil {
		auditValid = false
	}
	status := http.StatusOK
	state := "ok"
	if database != "connected" || !auditValid {
		status = http.StatusServiceUnavailable
		state = "degraded"
	}
	writeJSON(w, status, map[string]any{
		"status": state, "database": database, "auditChainValid": auditValid,
		"mode": "simulation", "version": s.version, "commit": s.commit,
	})
}

func (s *server) dashboard(w http.ResponseWriter, _ *http.Request) {
	dashboard, err := s.store.Dashboard(s.adminToken != "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "dashboard_unavailable", "Dashboard data is temporarily unavailable.")
		return
	}
	writeJSON(w, http.StatusOK, dashboard)
}

func (s *server) simulate(w http.ResponseWriter, r *http.Request) {
	var input gateway.SimulationRequest
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	services, err := s.store.ListServices()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "simulation_unavailable", "Service state is unavailable.")
		return
	}
	routes, err := s.store.ListRoutes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "simulation_unavailable", "Route state is unavailable.")
		return
	}
	decision, err := gateway.Evaluate(services, routes, input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_simulation", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, decision)
}

func (s *server) decide(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "A valid X-Admin-Token header is required.")
		return
	}
	var input struct {
		Actor    string `json:"actor"`
		Decision string `json:"decision"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := s.store.DecideChange(r.PathValue("id"), input.Actor, input.Decision); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": strings.ToLower(input.Decision)})
}

func (s *server) apply(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		writeError(w, http.StatusUnauthorized, "unauthorized", "A valid X-Admin-Token header is required.")
		return
	}
	var input struct {
		Actor string `json:"actor"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err := s.store.ApplyChange(r.PathValue("id"), input.Actor); err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "applied"})
}

func (s *server) writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "The change request was not found.")
	case errors.Is(err, store.ErrSameActor):
		writeError(w, http.StatusConflict, "separation_required", err.Error())
	case errors.Is(err, store.ErrInvalidTransition):
		writeError(w, http.StatusConflict, "invalid_transition", "The change is not in the required state.")
	default:
		writeError(w, http.StatusUnprocessableEntity, "change_rejected", err.Error())
	}
}

func (s *server) authorized(r *http.Request) bool {
	if s.adminToken == "" {
		return false
	}
	want := sha256.Sum256([]byte(s.adminToken))
	got := sha256.Sum256([]byte(r.Header.Get("X-Admin-Token")))
	return subtle.ConstantTimeCompare(want[:], got[:]) == 1
}

func (s *server) static(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "not_found", "API endpoint not found.")
		return
	}
	if s.webDir == "" {
		writeError(w, http.StatusNotFound, "dashboard_unavailable", "The dashboard build is not available.")
		return
	}
	root, err := filepath.Abs(s.webDir)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	relative := strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), string(filepath.Separator))
	target := filepath.Join(root, relative)
	if !strings.HasPrefix(target, root+string(filepath.Separator)) && target != root {
		http.NotFound(w, r)
		return
	}
	info, statErr := os.Stat(target)
	if statErr == nil && !info.IsDir() {
		if contentType := mime.TypeByExtension(filepath.Ext(target)); contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		http.ServeFile(w, r, target)
		return
	}
	http.ServeFile(w, r, filepath.Join(root, "index.html"))
}

func (s *server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				s.logger.Error("request panic", "error", recovered, "method", r.Method, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal_error", "The request could not be completed.")
			}
			s.logger.Info("request", "method", r.Method, "path", r.URL.Path, "durationMs", time.Since(started).Milliseconds())
		}()
		next.ServeHTTP(w, r)
	})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return errors.New("request body must be valid JSON with known fields")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}
