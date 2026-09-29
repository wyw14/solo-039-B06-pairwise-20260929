package journal

import (
	"errors"
	"testing"

	"github.com/wyw14/solo-039/internal/model"
)

func TestStoreSinkObservesAppendedEvents(t *testing.T) {
	store := New()
	var seen []model.Event
	store.AttachSink(func(event model.Event) error {
		seen = append(seen, event)
		return nil
	})
	event := model.NewEvent("proof.durable", model.NewCampaign("g"), "digest")
	if err := store.Append(event); err != nil {
		t.Fatalf("append: %v", err)
	}
	if len(seen) != 1 || seen[0].ID != event.ID {
		t.Fatalf("sink did not observe the appended event: %+v", seen)
	}
}

func TestStoreSinkFailureRollsBackInMemoryAppend(t *testing.T) {
	store := New()
	store.AttachSink(func(model.Event) error { return errors.New("disk full") })
	if err := store.Append(model.NewEvent("proof.durable", model.NewCampaign("g"), "d")); err == nil {
		t.Fatal("expected sink failure to surface")
	}
	if len(store.Events()) != 0 {
		t.Fatal("event must not be visible after a failed durable write")
	}
}

func TestStoreLoadReplaysDurableEvents(t *testing.T) {
	campaign := model.NewCampaign("g")
	existing := model.NewEvent("plant.started", campaign, "a")
	stored := []model.Event{
		existing,
		model.NewEvent("proof.durable", campaign, "b"),
		model.NewEvent("product.routed", campaign, "c"),
	}
	store := New()
	store.Load(stored)

	// Reloading the same events (restart against the same log) must not duplicate.
	store.Load(stored)
	if len(store.Events()) != len(stored) {
		t.Fatalf("reload duplicated events: got %d want %d", len(store.Events()), len(stored))
	}
	if err := RequireProof(store, campaign.Revision.String()); err != nil {
		t.Fatalf("replayed durable proof must satisfy RequireProof: %v", err)
	}

	// Load must not invoke the durable sink.
	sinkCalls := 0
	store.AttachSink(func(model.Event) error { sinkCalls++; return nil })
	store.Load([]model.Event{model.NewEvent("feed.transition", campaign, "d")})
	if sinkCalls != 0 {
		t.Fatalf("replay must not trigger the sink, got %d calls", sinkCalls)
	}
}
