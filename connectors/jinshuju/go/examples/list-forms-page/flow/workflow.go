// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package formspage runs Jinshuju listFormsPage as one Dex Flow.
package formspage

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/jinshuju/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "JinshujuFormsPage"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "jinshuju"
	operationStepType = "CallListFormsPage"
)

var formsPageOutcomeAttribute = dex.DefineAttribute[jinshuju.ListFormsPageOutput]("jinshuju-list-forms-page-outcome")

// Flow runs ListFormsPage once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection jinshuju.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection jinshuju.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{formsPageOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(jinshuju.NewListFormsPageStep(jinshuju.ListFormsPageStepConfig[jinshuju.ListFormsPageInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Jinshuju", Explanation: "Call the provider's listFormsPage operation."},
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

// dex:field attribute-key:jinshuju-list-forms-page-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListFormsPageOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"jinshuju-list-forms-page-outcome": outcome}}, nil
}

// dex:field attribute-key:jinshuju-list-forms-page-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readListFormsPageOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"jinshuju-list-forms-page-outcome": outcome}}, nil
}

func readListFormsPageOutcome(ctx dex.Context) (jinshuju.ListFormsPageOutput, error) {
	outcome, err := formsPageOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return jinshuju.ListFormsPageOutput{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input jinshuju.ListFormsPageInput) jinshuju.ListFormsPageInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListFormsPage" }
func (startRequest) WaitFor(dex.Context, jinshuju.ListFormsPageInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input jinshuju.ListFormsPageInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[jinshuju.ListFormsPageInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[jinshuju.ListFormsPageResult]
}

func (completeResult) GetStepType() string { return "CompleteListFormsPage" }
func (completeResult) Execute(ctx dex.Context, result jinshuju.ListFormsPageResult) (*dex.StepDecision, error) {
	if err := formsPageOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
