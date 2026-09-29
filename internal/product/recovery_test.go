package product

import (
	"errors"
	"testing"

	"github.com/wyw14/solo-039/internal/analyzer"
	"github.com/wyw14/solo-039/internal/campaign"
	"github.com/wyw14/solo-039/internal/journal"
	"github.com/wyw14/solo-039/internal/model"
)

func proofEvent(c model.Campaign, digest string) model.Event {
	event := model.NewEvent("proof.durable", c, digest)
	return event
}

func routeEvent(c model.Campaign, target string) model.Event {
	return model.NewEvent("product.routed", c, target)
}

func newRecoveryFixture() (*Coordinator, *journal.Store, model.Campaign, model.ProductPlan) {
	c := campaign.New("jet-fuel")
	a := analyzer.New()
	store := journal.New()
	coordinator := NewCoordinator(c, a, store)
	current := c.Snapshot()
	plan := model.ProductPlan{Campaign: current.ID, Revision: current.Revision, Grade: current.Grade, Target: "T-201"}
	return coordinator, store, current, plan
}

func TestBuildRecoveryReconstructsOpenRouteFromEvents(t *testing.T) {
	_, _, current, plan := newRecoveryFixture()
	events := []model.Event{
		proofEvent(current, "digest-1"),
		routeEvent(current, plan.Target),
	}
	recovery := BuildRecovery(events, nil)
	if !recovery.Durable(current.Revision.String()) {
		t.Fatal("expected durable proof for revision")
	}
	if !recovery.Routed(current.Revision.String()) {
		t.Fatal("expected routed decision reconstructed from journal")
	}
	target, ok := recovery.RouteTarget(current.Revision.String())
	if !ok || target != plan.Target {
		t.Fatalf("target = %q, ok=%v, want %q", target, ok, plan.Target)
	}
	if err := recovery.Validate(current.Revision.String()); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestBuildRecoveryTakesLatestRoutingForRevision(t *testing.T) {
	_, _, current, _ := newRecoveryFixture()
	events := []model.Event{
		routeEvent(current, "T-201"),
		routeEvent(current, "T-202"),
	}
	recovery := BuildRecovery(events, nil)
	target, _ := recovery.RouteTarget(current.Revision.String())
	if target != "T-202" {
		t.Fatalf("target = %q, want T-202 (latest routing)", target)
	}
}

func TestRestoreRouteOpensWhenEvidenceIsDurable(t *testing.T) {
	coordinator, store, current, plan := newRecoveryFixture()
	events := []model.Event{
		proofEvent(current, "digest-1"),
		routeEvent(current, plan.Target),
	}
	for _, event := range events {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	recovery := BuildRecovery(events, nil)
	if err := RestoreRoute(coordinator, plan, recovery); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !coordinator.Route.Opened() || coordinator.Route.Target() != plan.Target {
		t.Fatalf("route = open:%v target:%q", coordinator.Route.Opened(), coordinator.Route.Target())
	}
}

func TestRestoreRouteStaysClosedWithoutDurableProof(t *testing.T) {
	coordinator, _, current, plan := newRecoveryFixture()
	events := []model.Event{routeEvent(current, plan.Target)}
	recovery := BuildRecovery(events, nil)
	err := RestoreRoute(coordinator, plan, recovery)
	if !errors.Is(err, ErrRouteNotProven) {
		t.Fatalf("err = %v, want ErrRouteNotProven", err)
	}
	if coordinator.Route.Opened() {
		t.Fatal("route must stay closed when durable proof is missing")
	}
}

func TestRestoreRouteStaysClosedWhenProofBelongsToAnotherGeneration(t *testing.T) {
	coordinator, store, current, plan := newRecoveryFixture()
	other := model.NewCampaign("diesel")
	events := []model.Event{
		proofEvent(other, "digest-other"),
		routeEvent(current, plan.Target),
	}
	for _, event := range events {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	recovery := BuildRecovery(events, nil)
	err := RestoreRoute(coordinator, plan, recovery)
	if err == nil {
		t.Fatal("expected error when proof is for another revision")
	}
	if coordinator.Route.Opened() {
		t.Fatal("route must stay closed when proof is for another generation")
	}
}

func TestRestoreRouteStaysClosedWithoutDurableRouting(t *testing.T) {
	coordinator, _, _, plan := newRecoveryFixture()
	recovery := BuildRecovery(nil, nil)
	err := RestoreRoute(coordinator, plan, recovery)
	if err == nil {
		t.Fatal("expected error when target has no durable routing decision")
	}
	if coordinator.Route.Opened() {
		t.Fatal("route must stay closed without a durable routing decision")
	}
}

func TestRestoreRouteNoRouteIsClosedWithoutError(t *testing.T) {
	coordinator, _, _, plan := newRecoveryFixture()
	plan.Target = ""
	recovery := BuildRecovery(nil, nil)
	if err := RestoreRoute(coordinator, plan, recovery); err != nil {
		t.Fatalf("restore with no recorded route should succeed: %v", err)
	}
	if coordinator.Route.Opened() {
		t.Fatal("route must stay closed when none was recorded")
	}
}

func TestRestoreRouteRejectsSnapshotJournalTargetMismatch(t *testing.T) {
	coordinator, store, current, plan := newRecoveryFixture()
	events := []model.Event{
		proofEvent(current, "digest-1"),
		routeEvent(current, "T-209"),
	}
	for _, event := range events {
		if err := store.Append(event); err != nil {
			t.Fatal(err)
		}
	}
	recovery := BuildRecovery(events, nil)
	err := RestoreRoute(coordinator, plan, recovery)
	if !errors.Is(err, ErrRouteMismatch) {
		t.Fatalf("err = %v, want ErrRouteMismatch", err)
	}
	if coordinator.Route.Opened() {
		t.Fatal("route must stay closed when snapshot and journal disagree")
	}
}
