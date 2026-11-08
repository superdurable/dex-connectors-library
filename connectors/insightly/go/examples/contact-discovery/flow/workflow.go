// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package contactdiscovery runs Insightly listContacts as one Dex Flow.
package contactdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/insightly/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "InsightlyContactDiscovery"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "insightly"
	operationStepType = "CallListContacts"
)

var contactsPageAttribute = dex.DefineAttribute[insightly.ContactsPage]("insightly-contacts-page")

// Flow runs ListContacts once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection insightly.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection insightly.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contactsPageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(insightly.NewListContactsStep(insightly.ListContactsStepConfig[insightly.ListContactsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Insightly", Explanation: "List a bounded page of contacts for the connected Insightly user."},
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

// dex:field attribute-key:insightly-contacts-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readContactsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"insightly-contacts-page": outcome}}, nil
}

// dex:field attribute-key:insightly-contacts-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readContactsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"insightly-contacts-page": outcome}}, nil
}

func readContactsPage(ctx dex.Context) (insightly.ContactsPage, error) {
	outcome, err := contactsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return insightly.ContactsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input insightly.ListContactsInput) insightly.ListContactsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListContacts" }
func (startRequest) WaitFor(dex.Context, insightly.ListContactsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input insightly.ListContactsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[insightly.ListContactsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[insightly.ListContactsResult]
}

func (completeResult) GetStepType() string { return "CompleteListContacts" }
func (completeResult) Execute(ctx dex.Context, result insightly.ListContactsResult) (*dex.StepDecision, error) {
	if err := contactsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
