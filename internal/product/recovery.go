package product

import (
	"errors"
	"time"

	"github.com/wyw14/solo-039/internal/model"
)

// ErrRouteNotProven means a route was observed in the past, but the durable
// release evidence for its revision is missing, so it must stay closed.
var ErrRouteNotProven = errors.New("routed product has no durable proof")

// ErrRouteMismatch means the snapshot and the journal disagree about the
// recorded route, so neither source is trusted on its own.
var ErrRouteMismatch = errors.New("product route snapshot does not match journal")

type Recovery struct {
	Events    []model.Event
	Decisions []Decision
}

// BuildRecovery reconstructs product routing decisions from durable events and
// layers on decisions recorded during the current process lifetime.
func BuildRecovery(events []model.Event, audit *Audit) Recovery {
	byRevision := make(map[string]Decision)
	order := make([]string, 0)
	for _, event := range events {
		revision := event.Revision.String()
		if event.Kind != "product.routed" || event.Payload == "" {
			continue
		}
		plan := model.ProductPlan{Campaign: event.Campaign, Revision: event.Revision, Target: event.Payload}
		decision := Decision{Plan: plan, Proof: model.Proof{Revision: event.Revision, Digest: proofDigest(events, revision)}, Opened: true, At: time.Unix(0, event.At), Reason: "route durable in journal"}
		if _, exists := byRevision[revision]; !exists {
			order = append(order, revision)
		}
		byRevision[revision] = decision
	}
	decisions := make([]Decision, 0, len(order))
	for _, revision := range order {
		decisions = append(decisions, byRevision[revision])
	}
	if audit != nil {
		for _, decision := range audit.All() {
			revision := decision.Plan.Revision.String()
			if _, exists := byRevision[revision]; exists {
				continue
			}
			decisions = append(decisions, decision)
		}
	}
	return Recovery{Events: append([]model.Event(nil), events...), Decisions: decisions}
}

func proofDigest(events []model.Event, revision string) string {
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind == "proof.durable" && events[i].Revision.String() == revision {
			return events[i].Payload
		}
	}
	return ""
}

func (r Recovery) Durable(revision string) bool {
	for _, event := range r.Events {
		if event.Kind == "proof.durable" && event.Revision.String() == revision {
			return true
		}
	}
	return false
}

func (r Recovery) Routed(revision string) bool {
	for _, decision := range r.Decisions {
		if decision.Opened && decision.Plan.Revision.String() == revision {
			return true
		}
	}
	return false
}

// RouteTarget returns the target tank a routed revision was sending product to.
func (r Recovery) RouteTarget(revision string) (string, bool) {
	for _, decision := range r.Decisions {
		if decision.Opened && decision.Plan.Revision.String() == revision {
			return decision.Plan.Target, true
		}
	}
	return "", false
}

func (r Recovery) Validate(revision string) error {
	if r.Routed(revision) && !r.Durable(revision) {
		return ErrRouteNotProven
	}
	return nil
}

// RestoreRoute reinstates a previously open product route only when the
// journal proves both the routing and the durable release proof for the
// plan's revision. Without that evidence the route stays closed and the
// returned error explains why.
func RestoreRoute(coordinator *Coordinator, plan model.ProductPlan, recovery Recovery) error {
	revision := plan.Revision.String()
	if err := recovery.Validate(revision); err != nil {
		coordinator.Route.Close()
		return err
	}
	target, routed := recovery.RouteTarget(revision)
	switch {
	case routed && plan.Target == "":
		plan.Target = target
	case routed && plan.Target != target:
		coordinator.Route.Close()
		return ErrRouteMismatch
	case !routed && plan.Target != "":
		// Snapshot remembers a destination but no routing decision was ever
		// durably recorded: fail safe and remain closed.
		coordinator.Route.Close()
		return errors.New("product target present without durable routing decision; route held closed")
	case !routed:
		coordinator.Route.Close()
		return nil
	}
	if !recovery.Durable(revision) {
		coordinator.Route.Close()
		return errors.New("product proof unavailable during recovery; route held closed")
	}
	if err := coordinator.Recover(plan); err != nil {
		coordinator.Route.Close()
		return err
	}
	return nil
}
