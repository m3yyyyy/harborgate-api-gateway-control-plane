package gateway

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

type Service struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	BaseURL     string    `json:"baseUrl"`
	Owner       string    `json:"owner"`
	Health      string    `json:"health"`
	LastChecked time.Time `json:"lastChecked"`
}

type Route struct {
	ID                 string    `json:"id"`
	ServiceID          string    `json:"serviceId"`
	Name               string    `json:"name"`
	PathPrefix         string    `json:"pathPrefix"`
	Methods            []string  `json:"methods"`
	AuthMode           string    `json:"authMode"`
	RateLimitPerMinute int       `json:"rateLimitPerMinute"`
	CanaryPercent      int       `json:"canaryPercent"`
	Enabled            bool      `json:"enabled"`
	Revision           int       `json:"revision"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type RouteProposal struct {
	RateLimitPerMinute *int `json:"rateLimitPerMinute,omitempty"`
	CanaryPercent      *int `json:"canaryPercent,omitempty"`
}

type ChangeRequest struct {
	ID          string        `json:"id"`
	RouteID     string        `json:"routeId"`
	Summary     string        `json:"summary"`
	Risk        string        `json:"risk"`
	RequestedBy string        `json:"requestedBy"`
	Status      string        `json:"status"`
	Proposal    RouteProposal `json:"proposal"`
	ApprovedBy  string        `json:"approvedBy,omitempty"`
	CreatedAt   time.Time     `json:"createdAt"`
	DecidedAt   *time.Time    `json:"decidedAt,omitempty"`
	AppliedAt   *time.Time    `json:"appliedAt,omitempty"`
}

type AuditEvent struct {
	Sequence     int64     `json:"sequence"`
	ID           string    `json:"id"`
	Action       string    `json:"action"`
	Actor        string    `json:"actor"`
	Subject      string    `json:"subject"`
	Details      string    `json:"details"`
	PreviousHash string    `json:"previousHash"`
	Hash         string    `json:"hash"`
	CreatedAt    time.Time `json:"createdAt"`
}

type Dashboard struct {
	Services         []Service       `json:"services"`
	Routes           []Route         `json:"routes"`
	Changes          []ChangeRequest `json:"changes"`
	Audit            []AuditEvent    `json:"audit"`
	AuditChainValid  bool            `json:"auditChainValid"`
	MutationsEnabled bool            `json:"mutationsEnabled"`
	Mode             string          `json:"mode"`
}

type SimulationRequest struct {
	Method               string `json:"method"`
	Path                 string `json:"path"`
	Identity             string `json:"identity"`
	RequestsInLastMinute int    `json:"requestsInLastMinute"`
	RequestID            string `json:"requestId"`
}

type SimulationDecision struct {
	Decision       string   `json:"decision"`
	RouteID        string   `json:"routeId,omitempty"`
	RouteName      string   `json:"routeName,omitempty"`
	ServiceID      string   `json:"serviceId,omitempty"`
	Upstream       string   `json:"upstream,omitempty"`
	TrafficVariant string   `json:"trafficVariant,omitempty"`
	PolicyRevision int      `json:"policyRevision,omitempty"`
	Reasons        []string `json:"reasons"`
}

func Evaluate(services []Service, routes []Route, input SimulationRequest) (SimulationDecision, error) {
	input.Method = strings.ToUpper(strings.TrimSpace(input.Method))
	input.Path = strings.TrimSpace(input.Path)
	input.Identity = strings.ToLower(strings.TrimSpace(input.Identity))
	input.RequestID = strings.TrimSpace(input.RequestID)

	if !slices.Contains([]string{"GET", "POST", "PUT", "PATCH", "DELETE"}, input.Method) {
		return SimulationDecision{}, errors.New("method must be GET, POST, PUT, PATCH or DELETE")
	}
	parsed, err := url.ParseRequestURI(input.Path)
	if err != nil || !strings.HasPrefix(input.Path, "/") || parsed.IsAbs() {
		return SimulationDecision{}, errors.New("path must be an absolute request path")
	}
	if !slices.Contains([]string{"anonymous", "authenticated", "service-token"}, input.Identity) {
		return SimulationDecision{}, errors.New("identity must be anonymous, authenticated or service-token")
	}
	if input.RequestsInLastMinute < 0 || input.RequestsInLastMinute > 1_000_000 {
		return SimulationDecision{}, errors.New("requestsInLastMinute must be between 0 and 1000000")
	}
	if input.RequestID == "" || len(input.RequestID) > 128 {
		return SimulationDecision{}, errors.New("requestId is required and must be at most 128 characters")
	}

	var matched *Route
	for i := range routes {
		route := &routes[i]
		if !route.Enabled || !pathMatches(route.PathPrefix, parsed.Path) {
			continue
		}
		if matched == nil || len(route.PathPrefix) > len(matched.PathPrefix) {
			matched = route
		}
	}

	if matched == nil {
		return SimulationDecision{Decision: "deny", Reasons: []string{"No enabled route matches this path."}}, nil
	}

	decision := SimulationDecision{
		Decision:       "allow",
		RouteID:        matched.ID,
		RouteName:      matched.Name,
		ServiceID:      matched.ServiceID,
		TrafficVariant: "stable",
		PolicyRevision: matched.Revision,
		Reasons:        []string{},
	}

	if !slices.Contains(matched.Methods, input.Method) {
		decision.Decision = "deny"
		decision.Reasons = append(decision.Reasons, fmt.Sprintf("Method %s is not allowed by this route.", input.Method))
	}
	if !identityAllowed(matched.AuthMode, input.Identity) {
		decision.Decision = "deny"
		decision.Reasons = append(decision.Reasons, fmt.Sprintf("Identity %s does not satisfy %s authentication.", input.Identity, matched.AuthMode))
	}
	if input.RequestsInLastMinute >= matched.RateLimitPerMinute {
		decision.Decision = "deny"
		decision.Reasons = append(decision.Reasons, fmt.Sprintf("Rate limit of %d requests per minute would be exceeded.", matched.RateLimitPerMinute))
	}

	service, ok := findService(services, matched.ServiceID)
	if !ok {
		decision.Decision = "deny"
		decision.Reasons = append(decision.Reasons, "The route references an unknown service.")
	} else if service.Health == "offline" {
		decision.Decision = "deny"
		decision.Reasons = append(decision.Reasons, "The selected upstream is offline.")
	} else {
		decision.Upstream = service.BaseURL
		if service.Health == "degraded" {
			decision.Reasons = append(decision.Reasons, "The selected upstream is degraded; the request remains eligible.")
		}
	}

	if decision.Decision == "allow" {
		if matched.CanaryPercent > 0 && bucket(input.RequestID+":"+matched.ID) < matched.CanaryPercent {
			decision.TrafficVariant = "canary"
		}
		decision.Reasons = append(decision.Reasons, "Method, identity, traffic and upstream checks passed.")
	}

	return decision, nil
}

func ValidateProposal(proposal RouteProposal) error {
	if proposal.RateLimitPerMinute == nil && proposal.CanaryPercent == nil {
		return errors.New("proposal must change at least one supported field")
	}
	if proposal.RateLimitPerMinute != nil && (*proposal.RateLimitPerMinute < 1 || *proposal.RateLimitPerMinute > 100_000) {
		return errors.New("rateLimitPerMinute must be between 1 and 100000")
	}
	if proposal.CanaryPercent != nil && (*proposal.CanaryPercent < 0 || *proposal.CanaryPercent > 100) {
		return errors.New("canaryPercent must be between 0 and 100")
	}
	return nil
}

func pathMatches(prefix, path string) bool {
	if prefix == "/" {
		return true
	}
	return path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/")
}

func identityAllowed(mode, identity string) bool {
	switch mode {
	case "public":
		return true
	case "authenticated":
		return identity == "authenticated" || identity == "service-token"
	case "service-token":
		return identity == "service-token"
	default:
		return false
	}
}

func findService(services []Service, id string) (Service, bool) {
	for _, service := range services {
		if service.ID == id {
			return service, true
		}
	}
	return Service{}, false
}

func bucket(value string) int {
	sum := sha256.Sum256([]byte(value))
	return int(binary.BigEndian.Uint32(sum[:4]) % 100)
}
