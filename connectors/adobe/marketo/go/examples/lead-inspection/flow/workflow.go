// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package leadinspection runs Marketo getLead as one Dex Flow.
package leadinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/adobe/marketo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "MarketoLeadInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "adobe-marketo"
	operationStepType = "CallGetLead"
)

var leadAttribute = dex.DefineAttribute[marketo.LeadProfile]("marketo-lead")

// Flow runs GetLead once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection marketo.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection marketo.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{leadAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(marketo.NewGetLeadStep(marketo.GetLeadStepConfig[marketo.GetLeadInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Marketo", Explanation: "Read a lead's name from Marketo."},
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

// dex:field attribute-key:marketo-lead value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readLead(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-lead": outcome}}, nil
}

// dex:field attribute-key:marketo-lead value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readLead(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-lead": outcome}}, nil
}

func readLead(ctx dex.Context) (marketo.LeadProfile, error) {
	outcome, err := leadAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return marketo.LeadProfile{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input marketo.GetLeadInput) marketo.GetLeadInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetLead" }
func (startRequest) WaitFor(dex.Context, marketo.GetLeadInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input marketo.GetLeadInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[marketo.GetLeadInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[marketo.GetLeadResult]
}

func (completeResult) GetStepType() string { return "CompleteGetLead" }
func (completeResult) Execute(ctx dex.Context, result marketo.GetLeadResult) (*dex.StepDecision, error) {
	if err := leadAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
