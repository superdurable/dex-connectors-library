// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package contactiddiscovery runs Nimble listContactIDs as one Dex Flow.
package contactiddiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/nimble/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "NimbleContactIDDiscovery"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "nimble"
	operationStepType = "CallListContactIDs"
)

var contactIDsPageAttribute = dex.DefineAttribute[nimble.ContactIDsPage]("nimble-contact-ids-page")

// Flow runs ListContactIDs once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection nimble.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection nimble.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contactIDsPageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(nimble.NewListContactIDsStep(nimble.ListContactIDsStepConfig[nimble.ListContactIDsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Nimble", Explanation: "List a bounded page of contact IDs for the connected Nimble user."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:nimble-contact-ids-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readContactIDsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"nimble-contact-ids-page": outcome}}, nil
}

// dex:field attribute-key:nimble-contact-ids-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readContactIDsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"nimble-contact-ids-page": outcome}}, nil
}

func readContactIDsPage(ctx dex.Context) (nimble.ContactIDsPage, error) {
	outcome, err := contactIDsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return nimble.ContactIDsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input nimble.ListContactIDsInput) nimble.ListContactIDsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListContactIDs" }
func (startRequest) WaitFor(dex.Context, nimble.ListContactIDsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input nimble.ListContactIDsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[nimble.ListContactIDsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[nimble.ListContactIDsResult]
}

func (completeResult) GetStepType() string { return "CompleteListContactIDs" }
func (completeResult) Execute(ctx dex.Context, result nimble.ListContactIDsResult) (*dex.StepDecision, error) {
	if err := contactIDsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
