// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package contactlookup runs Dynamics 365 Sales findContact as one Dex Flow.
package contactlookup

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/microsoft/dynamics-365-sales/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "MicrosoftDynamics365SalesContactLookup"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "microsoft-dynamics-365-sales"
	operationStepType = "CallFindContact"
)

var contactAttribute = dex.DefineAttribute[dynamics365sales.Contact]("dynamics365sales-contact-lookup")

// Flow runs FindContact once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection dynamics365sales.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection dynamics365sales.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contactAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(dynamics365sales.NewFindContactStep(dynamics365sales.FindContactStepConfig[dynamics365sales.FindContactInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Dynamics 365 Sales", Explanation: "Find one Dataverse contact by email."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:dynamics365sales-contact-lookup value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readContact(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dynamics365sales-contact-lookup": outcome}}, nil
}

// dex:field attribute-key:dynamics365sales-contact-lookup value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readContact(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"dynamics365sales-contact-lookup": outcome}}, nil
}

func readContact(ctx dex.Context) (dynamics365sales.Contact, error) {
	outcome, err := contactAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return dynamics365sales.Contact{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input dynamics365sales.FindContactInput) dynamics365sales.FindContactInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartFindContact" }
func (startRequest) WaitFor(dex.Context, dynamics365sales.FindContactInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input dynamics365sales.FindContactInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[dynamics365sales.FindContactInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[dynamics365sales.FindContactResult]
}

func (completeResult) GetStepType() string { return "CompleteFindContact" }
func (completeResult) Execute(ctx dex.Context, result dynamics365sales.FindContactResult) (*dex.StepDecision, error) {
	if err := contactAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
