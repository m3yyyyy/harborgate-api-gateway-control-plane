package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/m3yyyyy/harborgate-api-gateway-control-plane/internal/gateway"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound          = errors.New("resource not found")
	ErrInvalidTransition = errors.New("invalid status transition")
	ErrSameActor         = errors.New("requester cannot approve their own change")
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("database path is required")
	}
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			return nil, fmt.Errorf("create data directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &Store{db: db}
	if err := store.configure(); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func (s *Store) configure() error {
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
	} {
		if _, err := s.db.Exec(statement); err != nil {
			return fmt.Errorf("configure sqlite: %w", err)
		}
	}
	return nil
}

func (s *Store) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS services (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  base_url TEXT NOT NULL,
  owner TEXT NOT NULL,
  health TEXT NOT NULL CHECK (health IN ('healthy', 'degraded', 'offline')),
  last_checked TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS routes (
  id TEXT PRIMARY KEY,
  service_id TEXT NOT NULL REFERENCES services(id),
  name TEXT NOT NULL,
  path_prefix TEXT NOT NULL,
  methods_json TEXT NOT NULL,
  auth_mode TEXT NOT NULL CHECK (auth_mode IN ('public', 'authenticated', 'service-token')),
  rate_limit_per_minute INTEGER NOT NULL CHECK (rate_limit_per_minute > 0),
  canary_percent INTEGER NOT NULL CHECK (canary_percent BETWEEN 0 AND 100),
  enabled INTEGER NOT NULL CHECK (enabled IN (0, 1)),
  revision INTEGER NOT NULL CHECK (revision > 0),
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS change_requests (
  id TEXT PRIMARY KEY,
  route_id TEXT NOT NULL REFERENCES routes(id),
  summary TEXT NOT NULL,
  risk TEXT NOT NULL CHECK (risk IN ('low', 'medium', 'high')),
  requested_by TEXT NOT NULL,
  status TEXT NOT NULL CHECK (status IN ('pending', 'approved', 'rejected', 'applied')),
  proposal_json TEXT NOT NULL,
  approved_by TEXT,
  created_at TEXT NOT NULL,
  decided_at TEXT,
  applied_at TEXT
);

CREATE TABLE IF NOT EXISTS audit_events (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE,
  action TEXT NOT NULL,
  actor TEXT NOT NULL,
  subject TEXT NOT NULL,
  details_json TEXT NOT NULL,
  previous_hash TEXT NOT NULL,
  hash TEXT NOT NULL,
  created_at TEXT NOT NULL
);
`
	if _, err := s.db.Exec(schema); err != nil {
		return fmt.Errorf("migrate sqlite: %w", err)
	}
	return nil
}

func (s *Store) Seed() error {
	var count int
	if err := s.db.QueryRow("SELECT COUNT(*) FROM services").Scan(&count); err != nil {
		return fmt.Errorf("count services: %w", err)
	}
	if count > 0 {
		return nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	now := time.Now().UTC().Truncate(time.Second)
	services := []gateway.Service{
		{ID: "svc-identity", Name: "Identity API", BaseURL: "https://identity.internal", Owner: "IAM Platform", Health: "healthy", LastChecked: now.Add(-18 * time.Second)},
		{ID: "svc-deploy", Name: "Deployment API", BaseURL: "https://deployments.internal", Owner: "Platform Engineering", Health: "degraded", LastChecked: now.Add(-42 * time.Second)},
		{ID: "svc-audit", Name: "Audit API", BaseURL: "https://audit.internal", Owner: "Security Engineering", Health: "healthy", LastChecked: now.Add(-26 * time.Second)},
	}
	for _, service := range services {
		if _, err := tx.Exec(`INSERT INTO services (id, name, base_url, owner, health, last_checked) VALUES (?, ?, ?, ?, ?, ?)`,
			service.ID, service.Name, service.BaseURL, service.Owner, service.Health, formatTime(service.LastChecked)); err != nil {
			return fmt.Errorf("seed service: %w", err)
		}
	}

	routes := []gateway.Route{
		{ID: "route-identity", ServiceID: "svc-identity", Name: "Identity lookups", PathPrefix: "/identity", Methods: []string{"GET"}, AuthMode: "authenticated", RateLimitPerMinute: 120, CanaryPercent: 10, Enabled: true, Revision: 4, UpdatedAt: now.Add(-55 * time.Minute)},
		{ID: "route-deploy", ServiceID: "svc-deploy", Name: "Deployment operations", PathPrefix: "/deployments", Methods: []string{"GET", "POST"}, AuthMode: "service-token", RateLimitPerMinute: 60, CanaryPercent: 20, Enabled: true, Revision: 7, UpdatedAt: now.Add(-32 * time.Minute)},
		{ID: "route-audit", ServiceID: "svc-audit", Name: "Audit search", PathPrefix: "/audit", Methods: []string{"GET"}, AuthMode: "authenticated", RateLimitPerMinute: 90, CanaryPercent: 0, Enabled: true, Revision: 3, UpdatedAt: now.Add(-21 * time.Minute)},
	}
	for _, route := range routes {
		methods, _ := json.Marshal(route.Methods)
		if _, err := tx.Exec(`INSERT INTO routes (id, service_id, name, path_prefix, methods_json, auth_mode, rate_limit_per_minute, canary_percent, enabled, revision, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			route.ID, route.ServiceID, route.Name, route.PathPrefix, string(methods), route.AuthMode, route.RateLimitPerMinute, route.CanaryPercent, boolInt(route.Enabled), route.Revision, formatTime(route.UpdatedAt)); err != nil {
			return fmt.Errorf("seed route: %w", err)
		}
	}

	rate := 45
	canary := 35
	proposal, _ := json.Marshal(gateway.RouteProposal{RateLimitPerMinute: &rate, CanaryPercent: &canary})
	if _, err := tx.Exec(`INSERT INTO change_requests (id, route_id, summary, risk, requested_by, status, proposal_json, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		"chg-1042", "route-deploy", "Reduce deployment burst traffic and expand the canary cohort", "medium", "release-bot", "pending", string(proposal), formatTime(now.Add(-9*time.Minute))); err != nil {
		return fmt.Errorf("seed change request: %w", err)
	}

	if err := appendAuditTx(tx, now.Add(-58*time.Minute), "service.health_observed", "health-controller", "svc-identity", map[string]any{"health": "healthy"}); err != nil {
		return err
	}
	if err := appendAuditTx(tx, now.Add(-32*time.Minute), "route.revision_loaded", "config-controller", "route-deploy", map[string]any{"revision": 7}); err != nil {
		return err
	}
	if err := appendAuditTx(tx, now.Add(-9*time.Minute), "change.requested", "release-bot", "chg-1042", map[string]any{"risk": "medium", "routeId": "route-deploy"}); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit seed data: %w", err)
	}
	return nil
}

func (s *Store) Dashboard(mutating bool) (gateway.Dashboard, error) {
	services, err := s.ListServices()
	if err != nil {
		return gateway.Dashboard{}, err
	}
	routes, err := s.ListRoutes()
	if err != nil {
		return gateway.Dashboard{}, err
	}
	changes, err := s.ListChanges()
	if err != nil {
		return gateway.Dashboard{}, err
	}
	audit, err := s.ListAudit(25)
	if err != nil {
		return gateway.Dashboard{}, err
	}
	valid, err := s.VerifyAuditChain()
	if err != nil {
		return gateway.Dashboard{}, err
	}
	return gateway.Dashboard{
		Services: services, Routes: routes, Changes: changes, Audit: audit,
		AuditChainValid: valid, MutationsEnabled: mutating, Mode: "simulation",
	}, nil
}

func (s *Store) ListServices() ([]gateway.Service, error) {
	rows, err := s.db.Query(`SELECT id, name, base_url, owner, health, last_checked FROM services ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	services := []gateway.Service{}
	for rows.Next() {
		var service gateway.Service
		var checked string
		if err := rows.Scan(&service.ID, &service.Name, &service.BaseURL, &service.Owner, &service.Health, &checked); err != nil {
			return nil, err
		}
		service.LastChecked = parseTime(checked)
		services = append(services, service)
	}
	return services, rows.Err()
}

func (s *Store) ListRoutes() ([]gateway.Route, error) {
	rows, err := s.db.Query(`SELECT id, service_id, name, path_prefix, methods_json, auth_mode, rate_limit_per_minute, canary_percent, enabled, revision, updated_at FROM routes ORDER BY path_prefix`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := []gateway.Route{}
	for rows.Next() {
		var route gateway.Route
		var methods, updated string
		var enabled int
		if err := rows.Scan(&route.ID, &route.ServiceID, &route.Name, &route.PathPrefix, &methods, &route.AuthMode, &route.RateLimitPerMinute, &route.CanaryPercent, &enabled, &route.Revision, &updated); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(methods), &route.Methods); err != nil {
			return nil, fmt.Errorf("decode methods for %s: %w", route.ID, err)
		}
		route.Enabled = enabled == 1
		route.UpdatedAt = parseTime(updated)
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

func (s *Store) ListChanges() ([]gateway.ChangeRequest, error) {
	rows, err := s.db.Query(`SELECT id, route_id, summary, risk, requested_by, status, proposal_json, approved_by, created_at, decided_at, applied_at FROM change_requests ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	changes := []gateway.ChangeRequest{}
	for rows.Next() {
		var change gateway.ChangeRequest
		var proposal, created string
		var approved, decided, applied sql.NullString
		if err := rows.Scan(&change.ID, &change.RouteID, &change.Summary, &change.Risk, &change.RequestedBy, &change.Status, &proposal, &approved, &created, &decided, &applied); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(proposal), &change.Proposal); err != nil {
			return nil, fmt.Errorf("decode proposal for %s: %w", change.ID, err)
		}
		change.ApprovedBy = approved.String
		change.CreatedAt = parseTime(created)
		change.DecidedAt = optionalTime(decided)
		change.AppliedAt = optionalTime(applied)
		changes = append(changes, change)
	}
	return changes, rows.Err()
}

func (s *Store) ListAudit(limit int) ([]gateway.AuditEvent, error) {
	if limit < 1 || limit > 100 {
		limit = 25
	}
	rows, err := s.db.Query(`SELECT sequence, id, action, actor, subject, details_json, previous_hash, hash, created_at FROM audit_events ORDER BY sequence DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := []gateway.AuditEvent{}
	for rows.Next() {
		event, err := scanAudit(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Store) VerifyAuditChain() (bool, error) {
	rows, err := s.db.Query(`SELECT sequence, id, action, actor, subject, details_json, previous_hash, hash, created_at FROM audit_events ORDER BY sequence`)
	if err != nil {
		return false, err
	}
	defer rows.Close()

	previous := ""
	for rows.Next() {
		event, err := scanAudit(rows)
		if err != nil {
			return false, err
		}
		if event.PreviousHash != previous || event.Hash != auditHash(previous, event.ID, event.Action, event.Actor, event.Subject, event.Details, formatTime(event.CreatedAt)) {
			return false, nil
		}
		previous = event.Hash
	}
	return true, rows.Err()
}

func (s *Store) DecideChange(id, actor, decision string) error {
	actor = strings.TrimSpace(actor)
	decision = strings.ToLower(strings.TrimSpace(decision))
	if actor == "" || len(actor) > 80 {
		return errors.New("actor is required and must be at most 80 characters")
	}
	if decision != "approved" && decision != "rejected" {
		return errors.New("decision must be approved or rejected")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var requester, status string
	if err := tx.QueryRow(`SELECT requested_by, status FROM change_requests WHERE id = ?`, id).Scan(&requester, &status); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if status != "pending" {
		return ErrInvalidTransition
	}
	if strings.EqualFold(requester, actor) {
		return ErrSameActor
	}

	now := time.Now().UTC()
	result, err := tx.Exec(`UPDATE change_requests SET status = ?, approved_by = ?, decided_at = ? WHERE id = ? AND status = 'pending'`, decision, actor, formatTime(now), id)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return ErrInvalidTransition
	}
	if err := appendAuditTx(tx, now, "change."+decision, actor, id, map[string]any{"previousStatus": "pending"}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) ApplyChange(id, actor string) error {
	actor = strings.TrimSpace(actor)
	if actor == "" || len(actor) > 80 {
		return errors.New("actor is required and must be at most 80 characters")
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var routeID, status, proposalJSON string
	if err := tx.QueryRow(`SELECT route_id, status, proposal_json FROM change_requests WHERE id = ?`, id).Scan(&routeID, &status, &proposalJSON); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	if status != "approved" {
		return ErrInvalidTransition
	}

	var proposal gateway.RouteProposal
	if err := json.Unmarshal([]byte(proposalJSON), &proposal); err != nil {
		return err
	}
	if err := gateway.ValidateProposal(proposal); err != nil {
		return err
	}

	var rate, canary, revision int
	if err := tx.QueryRow(`SELECT rate_limit_per_minute, canary_percent, revision FROM routes WHERE id = ?`, routeID).Scan(&rate, &canary, &revision); err != nil {
		return err
	}
	if proposal.RateLimitPerMinute != nil {
		rate = *proposal.RateLimitPerMinute
	}
	if proposal.CanaryPercent != nil {
		canary = *proposal.CanaryPercent
	}

	now := time.Now().UTC()
	if _, err := tx.Exec(`UPDATE routes SET rate_limit_per_minute = ?, canary_percent = ?, revision = ?, updated_at = ? WHERE id = ?`, rate, canary, revision+1, formatTime(now), routeID); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE change_requests SET status = 'applied', applied_at = ? WHERE id = ? AND status = 'approved'`, formatTime(now), id); err != nil {
		return err
	}
	if err := appendAuditTx(tx, now, "change.applied", actor, id, map[string]any{"routeId": routeID, "revision": revision + 1}); err != nil {
		return err
	}
	return tx.Commit()
}

func appendAuditTx(tx *sql.Tx, when time.Time, action, actor, subject string, details any) error {
	var previous string
	err := tx.QueryRow(`SELECT hash FROM audit_events ORDER BY sequence DESC LIMIT 1`).Scan(&previous)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	detailBytes, err := json.Marshal(details)
	if err != nil {
		return err
	}
	id, err := randomID()
	if err != nil {
		return err
	}
	created := formatTime(when)
	hash := auditHash(previous, id, action, actor, subject, string(detailBytes), created)
	_, err = tx.Exec(`INSERT INTO audit_events (id, action, actor, subject, details_json, previous_hash, hash, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, action, actor, subject, string(detailBytes), previous, hash, created)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanAudit(row scanner) (gateway.AuditEvent, error) {
	var event gateway.AuditEvent
	var created string
	err := row.Scan(&event.Sequence, &event.ID, &event.Action, &event.Actor, &event.Subject, &event.Details, &event.PreviousHash, &event.Hash, &created)
	if err != nil {
		return gateway.AuditEvent{}, err
	}
	event.CreatedAt = parseTime(created)
	return event, nil
}

func auditHash(previous, id, action, actor, subject, details, created string) string {
	value := strings.Join([]string{previous, id, action, actor, subject, details, created}, "\n")
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func randomID() (string, error) {
	value := make([]byte, 8)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return "evt-" + hex.EncodeToString(value), nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	parsed, _ := time.Parse(time.RFC3339Nano, value)
	return parsed
}

func optionalTime(value sql.NullString) *time.Time {
	if !value.Valid {
		return nil
	}
	parsed := parseTime(value.String)
	return &parsed
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
