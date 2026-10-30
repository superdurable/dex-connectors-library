// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package contactsync defines a runnable Brevo contact upsert Flow.
package contactsync

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/brevo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Dex Flow identity.
	FlowType = "BrevoContactSync"
	// ConnectionName is the static Dex Web connection used by the Flow.
	ConnectionName = "brevo"
	upsertStepType = "UpsertBrevoContact"
)

var outcomeAttribute = dex.DefineAttribute[brevo.ContactWriteResult]("brevo-contact-sync-outcome")

// Request is the Dex Web Start Flow input.
type Request struct {
	// Email identifies the Brevo contact.
	Email string `json:"email"`
	// FirstName is the contact's first name.
	FirstName string `json:"firstName"`
}

// Flow creates or updates one Brevo contact.
type Flow struct {
	dex.FlowDefaults
	connection brevo.Connection
}

// NewFlow binds the configured Brevo connection.
func NewFlow(connection brevo.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(start{}),
		dex.DefineStep(brevo.NewCreateOrUpdateContactStep(brevo.CreateOrUpdateContactStepConfig[Request]{
			StepType: upsertStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "contact-sync", GroupLabel: "Contact sync", Explanation: "Create or update the contact by email in Brevo."},
			MapToOperationInput: MapToCreateOrUpdateContactInput,
			Saved:               sdkgo.GoTo(saved{}),
		})),
		dex.DefineStep(saved{}),
	}
}

// MapToCreateOrUpdateContactInput maps a validated request to absolute contact fields.
func MapToCreateOrUpdateContactInput(request Request) brevo.CreateOrUpdateContactInput {
	return brevo.CreateOrUpdateContactInput{Email: request.Email, Attributes: map[string]string{"FNAME": request.FirstName}}
}

// GetRPCs registers the Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetDexSummary returns the completed provider outcome.
// dex:field attribute-key:brevo-contact-sync-outcome value-type:json editable:false description:"Contact save outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := optionalOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"brevo-contact-sync-outcome": outcome}}, nil
}

// GetDexDisplay returns the completed provider outcome.
// dex:field attribute-key:brevo-contact-sync-outcome value-type:json editable:false description:"Contact save outcome"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := optionalOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"brevo-contact-sync-outcome": outcome}}, nil
}

func optionalOutcome(ctx dex.Context) (brevo.ContactWriteResult, error) {
	outcome, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return brevo.ContactWriteResult{}, nil
	}
	return outcome, err
}

// dex:group group-id:contact-sync group-label:"Contact sync"
// dex:explanation text:"Validate and trim the contact request before calling Brevo."
type start struct{ dex.StepDefaults }

func (start) GetStepType() string                             { return "StartBrevoContactSync" }
func (start) WaitFor(dex.Context, Request) (*dex.Wait, error) { return dex.SkipWaitImmediately(), nil }
func (start) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	request.Email = strings.TrimSpace(request.Email)
	request.FirstName = strings.TrimSpace(request.FirstName)
	if request.Email == "" || request.FirstName == "" {
		return dex.ForceFail("email and firstName are required"), nil
	}
	return dex.GoTo(sdkgo.StepRef[Request](upsertStepType), request), nil
}

// dex:group group-id:contact-sync group-label:"Contact sync"
// dex:explanation text:"Complete with Brevo's contact create or update confirmation."
type saved struct {
	dex.StepDefaultsNoWaitFor[brevo.CreateOrUpdateContactResult]
}

func (saved) GetStepType() string { return "BrevoContactSaved" }
func (saved) Execute(ctx dex.Context, result brevo.CreateOrUpdateContactResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
