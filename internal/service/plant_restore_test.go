package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/wyw14/solo-039/internal/journal"
	"github.com/wyw14/solo-039/internal/model"
)

func routedSnapshot() (model.Campaign, model.Snapshot) {
	c := model.NewCampaign("baseline")
	c.Phase = model.OnSpec
	snapshot := model.Snapshot{Campaign: c, Column: model.Column{ID: "T-101", Phase: c.Phase, Pressure: 0.8, Level: 0.4, Fencing: c.Fencing}, Products: []string{"T-901"}}
	return c, snapshot
}

func TestRestoreReopensRoutedProductWithDurableProof(t *testing.T) {
	dir := t.TempDir()
	c, snapshot := routedSnapshot()

	first := NewPlant(dir)
	proof := model.Proof{ID: uuid.New(), Revision: c.Revision, Source: "analyzer", Digest: "durable-digest", Durable: true}
	if err := journal.PersistProof(first.Journal, proof, c); err != nil {
		t.Fatalf("persist proof: %v", err)
	}
	if err := first.Snapshots.Save(snapshot); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	restarted := NewPlant(dir)
	if err := restarted.Restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !restarted.Product.Route.Opened() || restarted.Product.Route.Target() != "T-901" {
		t.Fatalf("expected route restored to T-901, opened=%v target=%q", restarted.Product.Route.Opened(), restarted.Product.Route.Target())
	}
	if targets := restarted.ProductTargets(); len(targets) != 1 || targets[0] != "T-901" {
		t.Fatalf("expected product target T-901, got %v", targets)
	}
	if restarted.Campaign.Snapshot().Phase != model.OnSpec {
		t.Fatalf("batch phase not restored: %v", restarted.Campaign.Snapshot().Phase)
	}
	if restarted.Incidents.CountOpen() != 0 {
		t.Fatal("successful route restore must not raise an incident")
	}
}

func TestRestoreKeepsProductClosedWhenProofMissing(t *testing.T) {
	dir := t.TempDir()
	_, snapshot := routedSnapshot()

	first := NewPlant(dir)
	// Snapshot records a running route, but no proof.durable is persisted.
	if err := first.Snapshots.Save(snapshot); err != nil {
		t.Fatalf("save snapshot: %v", err)
	}

	restarted := NewPlant(dir)
	if err := restarted.Restore(); err != nil {
		t.Fatalf("restore should succeed even without release proof: %v", err)
	}
	if restarted.Product.Route.Opened() || restarted.Product.Route.Target() != "" {
		t.Fatalf("route must stay closed without release basis: opened=%v target=%q", restarted.Product.Route.Opened(), restarted.Product.Route.Target())
	}
	if targets := restarted.ProductTargets(); len(targets) != 0 {
		t.Fatalf("expected no product targets, got %v", targets)
	}
	if restarted.Incidents.CountOpen() != 1 {
		t.Fatalf("expected one open incident explaining the closure, got %d", restarted.Incidents.CountOpen())
	}
	incident := restarted.Incidents.List(true)[0]
	if incident.Kind != "product-recovery" || incident.Message == "" {
		t.Fatalf("incident must explain why the route stayed closed: %+v", incident)
	}
}

func TestRestoreOnMissingSnapshotLeavesRouteClosed(t *testing.T) {
	restarted := NewPlant(t.TempDir())
	// A missing snapshot surfaces an error the caller treats as "nothing to
	// restore" (see api server startup); the route must remain closed.
	_ = restarted.Restore()
	if restarted.Product.Route.Opened() {
		t.Fatal("no snapshot must yield a closed route")
	}
}

// Drives the real running flow (start -> samples -> route) so that the durable
// proof and route event are persisted, then restores into a brand new instance
// and confirms the target comes back without a spurious incident.
func TestRestoreRecoversRouteThroughLiveFlow(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()

	live := NewPlant(dir)
	if _, err := live.Start(ctx, "baseline", "tank-A"); err != nil {
		t.Fatalf("start: %v", err)
	}
	// The analyzer window requires three valid samples for a composition proof.
	for i := 0; i < 3; i++ {
		if _, err := live.AddSample(0.9, int64(10+i)); err != nil {
			t.Fatalf("sample %d: %v", i, err)
		}
	}
	if err := live.RouteProduct("T-901"); err != nil {
		t.Fatalf("route product: %v", err)
	}
	if !live.Product.Route.Opened() {
		t.Fatal("precondition: live route should be open")
	}

	restarted := NewPlant(dir)
	if err := restarted.Restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !restarted.Product.Route.Opened() || restarted.Product.Route.Target() != "T-901" {
		t.Fatalf("route not recovered: opened=%v target=%q", restarted.Product.Route.Opened(), restarted.Product.Route.Target())
	}
	if got := restarted.Campaign.Snapshot().Phase; got != model.OnSpec {
		t.Fatalf("phase = %v want on-spec", got)
	}
	if restarted.Incidents.CountOpen() != 0 {
		t.Fatal("a fully evidenced route must restore cleanly with no incident")
	}
}
