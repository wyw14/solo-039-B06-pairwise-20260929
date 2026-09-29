package journal

import (
	"errors"
	"github.com/wyw14/solo-039/internal/model"
	"sync"
)

type Store struct {
	mu       sync.Mutex
	events   []model.Event
	failNext bool
	sink     func(model.Event) error
}

func New() *Store { return &Store{events: make([]model.Event, 0, 64)} }

// AttachSink installs a durable destination that observes every successfully
// appended event (for example the on-disk event log). The sink is not invoked
// when the in-memory append itself fails.
func (s *Store) AttachSink(sink func(model.Event) error) {
	s.mu.Lock()
	s.sink = sink
	s.mu.Unlock()
}

func (s *Store) FailNext() { s.mu.Lock(); s.failNext = true; s.mu.Unlock() }

func (s *Store) Append(event model.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failNext {
		s.failNext = false
		return errors.New("journal short write")
	}
	s.events = append(s.events, event)
	if s.sink != nil {
		if err := s.sink(event); err != nil {
			s.events = s.events[:len(s.events)-1]
			return err
		}
	}
	return nil
}

// Load replays durable events back into the store after a restart. Events are
// appended in the order given, duplicates (by event ID) are skipped, and the
// durable sink is intentionally not triggered for replays.
func (s *Store) Load(events []model.Event) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	known := make(map[string]struct{}, len(s.events))
	for _, event := range s.events {
		known[event.ID.String()] = struct{}{}
	}
	added := 0
	for _, event := range events {
		id := event.ID.String()
		if _, ok := known[id]; ok {
			continue
		}
		s.events = append(s.events, event)
		known[id] = struct{}{}
		added++
	}
	return added
}

func (s *Store) Events() []model.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	copyEvents := make([]model.Event, len(s.events))
	copy(copyEvents, s.events)
	return copyEvents
}

func (s *Store) Last(kind string) (model.Event, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.events) - 1; i >= 0; i-- {
		if s.events[i].Kind == kind {
			return s.events[i], true
		}
	}
	return model.Event{}, false
}
