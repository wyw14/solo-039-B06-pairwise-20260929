package product

import (
	"errors"
	"time"

	"github.com/wyw14/solo-039/internal/analyzer"
	"github.com/wyw14/solo-039/internal/campaign"
	"github.com/wyw14/solo-039/internal/journal"
	"github.com/wyw14/solo-039/internal/model"
)

type Coordinator struct {
	Route    *Route
	Campaign *campaign.State
	Analyzer *analyzer.State
	Journal  *journal.Store
	proof    model.Proof
	// OnPersist, when set, is invoked after a proof is durably appended to
	// the journal so the host can mirror it onto additional durable sinks.
	OnPersist func(model.Event) error
}

func NewCoordinator(c *campaign.State, a *analyzer.State, j *journal.Store) *Coordinator {
	return &Coordinator{Route: &Route{}, Campaign: c, Analyzer: a, Journal: j}
}

func (c *Coordinator) persistProof(proof model.Proof) error {
	if proof.Digest == "" {
		proof.Digest = model.ProofDigest(proof)
	}
	event := model.NewEvent("proof.durable", c.Campaign.Snapshot(), proof.Digest)
	event.At = time.Now().UnixNano()
	if err := c.Journal.Append(event); err != nil {
		return err
	}
	if c.OnPersist != nil {
		if err := c.OnPersist(event); err != nil {
			return err
		}
	}
	return nil
}

func (c *Coordinator) Publish(plan model.ProductPlan) error {
	if plan.Campaign != c.Campaign.Snapshot().ID {
		return errors.New("campaign mismatch")
	}
	proof, ok := c.Analyzer.Proof(plan.Revision)
	if !ok {
		return errors.New("composition proof incomplete")
	}
	if err := c.persistProof(proof); err != nil {
		return err
	}
	c.proof = proof
	return c.Route.Open(plan)
}

func (c *Coordinator) Recover(plan model.ProductPlan) error {
	if err := journal.RequireProof(c.Journal, plan.Revision.String()); err != nil {
		return err
	}
	digest, _ := journal.ProofDigest(c.Journal, plan.Revision.String())
	c.proof = model.Proof{Revision: plan.Revision, Digest: digest, Durable: true}
	return c.Route.Open(plan)
}

func (c *Coordinator) PublishWithProof(plan model.ProductPlan, proof model.Proof) error {
	if plan.Campaign != c.Campaign.Snapshot().ID {
		return errors.New("campaign mismatch")
	}
	if proof.Revision != plan.Revision || proof.Digest == "" {
		return errors.New("proof does not match product revision")
	}
	if err := c.persistProof(proof); err != nil {
		return err
	}
	c.proof = proof
	return c.Route.Open(plan)
}

func (c *Coordinator) Proof() model.Proof { return c.proof }
