package store

import (
	"errors"
	"testing"
)

func TestAuditChainAndApprovalSeparation(t *testing.T) {
	database, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := database.Seed(); err != nil {
		t.Fatal(err)
	}

	valid, err := database.VerifyAuditChain()
	if err != nil || !valid {
		t.Fatalf("audit chain valid = %v, error = %v", valid, err)
	}
	if err := database.DecideChange("chg-1042", "release-bot", "approved"); !errors.Is(err, ErrSameActor) {
		t.Fatalf("self approval error = %v, want ErrSameActor", err)
	}
	if err := database.DecideChange("chg-1042", "platform-reviewer", "approved"); err != nil {
		t.Fatalf("approve change: %v", err)
	}
	if err := database.ApplyChange("chg-1042", "config-controller"); err != nil {
		t.Fatalf("apply change: %v", err)
	}

	dashboard, err := database.Dashboard(true)
	if err != nil {
		t.Fatal(err)
	}
	if !dashboard.AuditChainValid {
		t.Fatal("audit chain invalid after change")
	}
	if dashboard.Changes[0].Status != "applied" {
		t.Fatalf("status = %q, want applied", dashboard.Changes[0].Status)
	}
	for _, route := range dashboard.Routes {
		if route.ID == "route-deploy" && (route.RateLimitPerMinute != 45 || route.CanaryPercent != 35 || route.Revision != 8) {
			t.Fatalf("route not updated: %#v", route)
		}
	}
}
