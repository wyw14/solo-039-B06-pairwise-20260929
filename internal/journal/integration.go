package journal

import (
	"errors"
	"github.com/wyw14/solo-039/internal/model"
)

// proofEvent returns the durable proof event for the given revision anywhere
// in the journal, not just the most recent one: a newer generation must not
// erase evidence recorded for an earlier product route.
func proofEvent(store *Store, revision string) (model.Event, bool) {
	for _, event := range store.Events() {
		if event.Kind == "proof.durable" && event.Revision.String() == revision && event.Payload != "" {
			return event, true
		}
	}
	return model.Event{}, false
}

func RequireProof(store *Store, revision string) error {
	if _, ok := proofEvent(store, revision); !ok {
		return errors.New("proof is not durable")
	}
	return nil
}

// ProofDigest returns the recorded digest of the durable proof for revision.
func ProofDigest(store *Store, revision string) (string, bool) {
	event, ok := proofEvent(store, revision)
	if !ok {
		return "", false
	}
	return event.Payload, true
}
