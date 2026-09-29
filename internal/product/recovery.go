package product

import (
	"errors"
	"github.com/wyw14/solo-039/internal/journal"
	"github.com/wyw14/solo-039/internal/model"
)

type Recovery struct {
	Events    []model.Event
	Decisions []Decision
}

func BuildRecovery(events []model.Event, audit *Audit) Recovery {
	decisions := make([]Decision, 0)
	if audit != nil {
		if latest, err := audit.Latest(); err == nil {
			decisions = append(decisions, latest)
		}
	}
	return Recovery{Events: append([]model.Event(nil), events...), Decisions: decisions}
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

func (r Recovery) Validate(revision string) error {
	if r.Routed(revision) && !r.Durable(revision) {
		return errors.New("routed product has no durable proof")
	}
	return nil
}

func RestoreRoute(coordinator *Coordinator, plan model.ProductPlan, store *journal.Store, recovery Recovery) error {
	if err := recovery.Validate(plan.Revision.String()); err != nil {
		return err
	}
	if !recovery.Durable(plan.Revision.String()) {
		return errors.New("product proof unavailable during recovery")
	}
	return coordinator.Recover(plan)
}

// RouteOutcome reports how a product route captured in a snapshot was handled
// during recovery.
type RouteOutcome struct {
	Target string
	Opened bool
	Reason string
}

// RestoreRouteFromSnapshot re-opens the product route that was running before a
// restart, but only when the release basis for it can be verified: the snapshot
// must name a routed target and the recovered journal must carry a durable
// proof for the same product revision. When the evidence is missing or stale
// the route is deliberately left closed and a human-readable reason is
// returned (and recorded in the audit trail) instead of failing recovery.
func RestoreRouteFromSnapshot(coordinator *Coordinator, plan model.ProductPlan, events []model.Event, audit *Audit) RouteOutcome {
	if plan.Target == "" {
		return RouteOutcome{Reason: "no routed product target recorded in snapshot"}
	}
	revision := plan.Revision.String()
	recovery := BuildRecovery(events, audit)

	if !recovery.Durable(revision) {
		coordinator.Route.Close()
		reason := "product route to " + plan.Target + " kept closed: durable release proof missing for revision " + revision
		if audit != nil {
			audit.Record(plan, model.Proof{}, false, reason)
		}
		return RouteOutcome{Target: plan.Target, Reason: reason}
	}

	if err := coordinator.Recover(plan); err != nil {
		coordinator.Route.Close()
		reason := "product route to " + plan.Target + " kept closed: " + err.Error()
		if audit != nil {
			audit.Record(plan, model.Proof{}, false, reason)
		}
		return RouteOutcome{Target: plan.Target, Reason: reason}
	}

	reason := "product route to " + plan.Target + " restored from snapshot with durable proof"
	if audit != nil {
		audit.Record(plan, coordinator.Proof(), true, reason)
	}
	return RouteOutcome{Target: plan.Target, Opened: true, Reason: reason}
}
