package product

import (
	"errors"
	"github.com/wyw14/solo-039/internal/interlock"
	"github.com/wyw14/solo-039/internal/model"
)

func RouteWithPermit(coordinator *Coordinator, permit interlock.Permit, plan model.ProductPlan, proof model.Proof) error {
	if !permit.Allowed {
		return permit.Error()
	}
	if proof.Revision != plan.Revision {
		return errors.New("permit proof belongs to another product revision")
	}
	return coordinator.PublishWithProof(plan, proof)
}

func CloseOnRevision(route *Route, revision string) bool {
	if route.revision == revision {
		return false
	}
	route.Close()
	return true
}

func PublishGradeTransition(coordinator *Coordinator, plan model.ProductPlan, refluxProof model.Proof) error {
	if refluxProof.Source != "reflux" {
		return errors.New("grade transition requires reflux proof")
	}
	if refluxProof.Revision != plan.Revision || refluxProof.Digest == "" {
		return errors.New("reflux proof belongs to another generation")
	}
	if err := coordinator.persistProof(refluxProof); err != nil {
		return err
	}
	coordinator.proof = refluxProof
	return coordinator.Route.Open(plan)
}
