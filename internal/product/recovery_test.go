package product

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/wyw14/solo-039/internal/analyzer"
	"github.com/wyw14/solo-039/internal/campaign"
	"github.com/wyw14/solo-039/internal/journal"
	"github.com/wyw14/solo-039/internal/model"
)

func onSpecCoordinator(store *journal.Store) (*Coordinator, model.Campaign) {
	campaignState := campaign.New("baseline")
	current := campaignState.Snapshot()
	current.Phase = model.OnSpec
	campaign.Restore(campaignState, current)
	return NewCoordinator(campaignState, analyzer.New(), store), current
}

func planFor(c model.Campaign, target string) model.ProductPlan {
	return model.ProductPlan{Campaign: c.ID, Revision: c.Revision, Grade: c.Grade, Target: target}
}

func TestRestoreRouteFromSnapshotOpensWithDurableProof(t *testing.T) {
	store := journal.New()
	coordinator, current := onSpecCoordinator(store)
	plan := planFor(current, "T-901")
	proof := model.Proof{ID: uuid.New(), Revision: current.Revision, Source: "analyzer", Digest: "durable-digest", Durable: true}
	if err := journal.PersistProof(store, proof, current); err != nil {
		t.Fatalf("persist proof: %v", err)
	}
	audit := NewAudit()

	outcome := RestoreRouteFromSnapshot(coordinator, plan, store.Events(), audit)

	if !outcome.Opened {
		t.Fatalf("expected route opened, got reason %q", outcome.Reason)
	}
	if !coordinator.Route.Opened() || coordinator.Route.Target() != "T-901" {
		t.Fatalf("route not restored to T-901: opened=%v target=%q", coordinator.Route.Opened(), coordinator.Route.Target())
	}
	latest, err := audit.Latest()
	if err != nil || !latest.Opened {
		t.Fatalf("expected positive audit decision, got %+v err=%v", latest, err)
	}
}

func TestRestoreRouteFromSnapshotStaysClosedWithoutProof(t *testing.T) {
	store := journal.New()
	coordinator, current := onSpecCoordinator(store)
	plan := planFor(current, "T-901")
	audit := NewAudit()

	outcome := RestoreRouteFromSnapshot(coordinator, plan, store.Events(), audit)

	if outcome.Opened {
		t.Fatal("route must not open without durable proof")
	}
	if coordinator.Route.Opened() {
		t.Fatal("route flag must stay closed without release basis")
	}
	if !strings.Contains(outcome.Reason, "kept closed") || !strings.Contains(outcome.Reason, "T-901") {
		t.Fatalf("reason must explain the closed route and target, got %q", outcome.Reason)
	}
	latest, err := audit.Latest()
	if err != nil {
		t.Fatalf("expected a recorded decision: %v", err)
	}
	if latest.Opened || !strings.Contains(latest.Reason, "kept closed") {
		t.Fatalf("audit must record closure with reason, got %+v", latest)
	}
}

func TestRestoreRouteFromSnapshotStaysClosedForStaleProof(t *testing.T) {
	store := journal.New()
	coordinator, current := onSpecCoordinator(store)

	// Durable proof exists, but for an older generation.
	stale := current
	stale.Revision = uuid.New()
	staleProof := model.Proof{ID: uuid.New(), Revision: stale.Revision, Source: "analyzer", Digest: "stale-digest", Durable: true}
	if err := journal.PersistProof(store, staleProof, stale); err != nil {
		t.Fatalf("persist stale proof: %v", err)
	}

	plan := planFor(current, "T-901")
	outcome := RestoreRouteFromSnapshot(coordinator, plan, store.Events(), NewAudit())

	if outcome.Opened || coordinator.Route.Opened() {
		t.Fatal("stale generation proof must not re-open the current route")
	}
	if !strings.Contains(outcome.Reason, "kept closed") {
		t.Fatalf("expected closed-with-reason, got %q", outcome.Reason)
	}
}

func TestRestoreRouteFromSnapshotNoTarget(t *testing.T) {
	store := journal.New()
	coordinator, current := onSpecCoordinator(store)
	outcome := RestoreRouteFromSnapshot(coordinator, planFor(current, ""), store.Events(), NewAudit())
	if outcome.Opened || coordinator.Route.Opened() || outcome.Target != "" {
		t.Fatalf("empty snapshot target must be a no-op, got %+v", outcome)
	}
}
