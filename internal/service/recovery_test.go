package service

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wyw14/solo-039/internal/model"
)

// routeProductOnDisk drives a plant through start, evidence collection and
// product routing, leaving snapshot.json and events.jsonl on disk.
func routeProductOnDisk(t *testing.T, dir, target string) *Plant {
	t.Helper()
	plant := NewPlant(dir)
	ctx := context.Background()
	if _, err := plant.Start(ctx, "jet-fuel", "tank-A"); err != nil {
		t.Fatalf("start: %v", err)
	}
	for _, at := range []int64{1, 2, 3} {
		if _, err := plant.AddSample(0.95, at); err != nil {
			t.Fatalf("add sample: %v", err)
		}
	}
	if err := plant.RouteProduct(target); err != nil {
		t.Fatalf("route product: %v", err)
	}
	if err := plant.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	return plant
}

func TestRestoreReopensProductRouteWithDurableProof(t *testing.T) {
	dir := t.TempDir()
	routeProductOnDisk(t, dir, "T-201")

	restored := NewPlant(dir)
	if err := restored.Restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !restored.Product.Route.Opened() {
		t.Fatal("product route should be open after recovery with durable proof")
	}
	if restored.Product.Route.Target() != "T-201" {
		t.Fatalf("target = %q, want T-201", restored.Product.Route.Target())
	}
	if targets := restored.ProductTargets(); len(targets) != 1 || targets[0] != "T-201" {
		t.Fatalf("product targets = %v, want [T-201]", targets)
	}
	if phase := restored.Campaign.Snapshot().Phase; phase != model.OnSpec {
		t.Fatalf("phase = %q, want on-spec", phase)
	}
	latest, err := restored.ProductAudit.Latest()
	if err != nil || !latest.Opened {
		t.Fatalf("audit should record a successful restoration, got %+v err=%v", latest, err)
	}
	if restored.Incidents.CountOpen() != 0 {
		t.Fatalf("expected no open incidents, got %d", restored.Incidents.CountOpen())
	}
}

func TestRestoreKeepsRouteClosedWhenProofIsMissing(t *testing.T) {
	dir := t.TempDir()
	routeProductOnDisk(t, dir, "T-201")

	// Tamper with the durable log: remove the release proof while keeping the
	// routing decision and the snapshot (which still claims the route is open).
	logPath := filepath.Join(dir, "events.jsonl")
	file, err := os.Open(logPath)
	if err != nil {
		t.Fatal(err)
	}
	var kept []model.Event
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event model.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		if event.Kind != "proof.durable" {
			kept = append(kept, event)
		}
	}
	file.Close()
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	out, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	encoder := json.NewEncoder(out)
	for _, event := range kept {
		if err := encoder.Encode(event); err != nil {
			t.Fatal(err)
		}
	}
	out.Close()

	restored := NewPlant(dir)
	if err := restored.Restore(); err != nil {
		t.Fatalf("restore should not hard-fail, got %v", err)
	}
	if restored.Product.Route.Opened() {
		t.Fatal("product route must remain closed without durable release proof")
	}
	if len(restored.ProductTargets()) != 0 {
		t.Fatalf("expected no product targets, got %v", restored.ProductTargets())
	}
	// The campaign phase is still on-spec from the snapshot; the operator can
	// see the contradiction is a closed route with an explained reason.
	if phase := restored.Campaign.Snapshot().Phase; phase != model.OnSpec {
		t.Fatalf("phase = %q, want on-spec", phase)
	}
	latest, err := restored.ProductAudit.Latest()
	if err != nil {
		t.Fatalf("audit decision expected: %v", err)
	}
	if latest.Opened || !strings.Contains(latest.Reason, "proof") {
		t.Fatalf("audit should explain the missing proof, got %+v", latest)
	}
	if restored.Incidents.CountOpen() == 0 {
		t.Fatal("expected an open product-recovery incident explaining the closure")
	}
}

func TestRestoreKeepsClosedRouteWithoutIncident(t *testing.T) {
	dir := t.TempDir()
	plant := routeProductOnDisk(t, dir, "T-201")
	snapshot, err := plant.Snapshots.Load()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Product.Opened = false
	snapshot.Products = nil
	if err := plant.Snapshots.Save(snapshot); err != nil {
		t.Fatal(err)
	}

	restored := NewPlant(dir)
	if err := restored.Restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if restored.Product.Route.Opened() {
		t.Fatal("route recorded closed must stay closed")
	}
	if restored.Incidents.CountOpen() != 0 {
		t.Fatalf("expected no incidents for a closed route, got %d", restored.Incidents.CountOpen())
	}
}

func TestRestoreLegacySnapshotWithOnlyProductNames(t *testing.T) {
	dir := t.TempDir()
	routeProductOnDisk(t, dir, "T-201")

	// Rewrite the snapshot in the legacy shape: product names only, no
	// structured route with revision/open flags.
	snapshot, err := NewPlant(dir).Snapshots.Load()
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Product = model.ProductRoute{}
	if err := NewPlant(dir).Snapshots.Save(snapshot); err != nil {
		t.Fatal(err)
	}

	restored := NewPlant(dir)
	if err := restored.Restore(); err != nil {
		t.Fatalf("restore: %v", err)
	}
	if !restored.Product.Route.Opened() || restored.Product.Route.Target() != "T-201" {
		t.Fatalf("legacy route should reopen with durable proof, got open=%v target=%q", restored.Product.Route.Opened(), restored.Product.Route.Target())
	}
}
