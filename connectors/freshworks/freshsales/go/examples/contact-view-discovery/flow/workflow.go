// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package contactviewdiscovery runs Freshsales listContactViews as one Dex Flow.
package contactviewdiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/freshworks/freshsales/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "FreshsalesContactViewDiscovery"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "freshsales"
	operationStepType = "CallListContactViews"
)

var contactViewsAttribute = dex.DefineAttribute[freshsales.ContactViews]("freshsales-contact-views")

// Flow runs ListContactViews once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection freshsales.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection freshsales.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contactViewsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(freshsales.NewListContactViewsStep(freshsales.ListContactViewsStepConfig[freshsales.ListContactViewsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Freshsales", Explanation: "Call the provider's listContactViews operation."},
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

// dex:field attribute-key:freshsales-contact-views value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readContactViews(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"freshsales-contact-views": outcome}}, nil
}

// dex:field attribute-key:freshsales-contact-views value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readContactViews(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"freshsales-contact-views": outcome}}, nil
}

func readContactViews(ctx dex.Context) (freshsales.ContactViews, error) {
	outcome, err := contactViewsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return freshsales.ContactViews{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input freshsales.ListContactViewsInput) freshsales.ListContactViewsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListContactViews" }
func (startRequest) WaitFor(dex.Context, freshsales.ListContactViewsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input freshsales.ListContactViewsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[freshsales.ListContactViewsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[freshsales.ListContactViewsResult]
}

func (completeResult) GetStepType() string { return "CompleteListContactViews" }
func (completeResult) Execute(ctx dex.Context, result freshsales.ListContactViewsResult) (*dex.StepDecision, error) {
	if err := contactViewsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
