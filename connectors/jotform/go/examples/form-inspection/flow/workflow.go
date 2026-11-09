// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package forminspection runs Jotform listForms as one Dex Flow.
package forminspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/jotform/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "JotformFormInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "jotform"
	operationStepType = "CallListForms"
)

var formsPageAttribute = dex.DefineAttribute[jotform.FormsPage]("jotform-forms-page")

// Flow runs ListForms once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection jotform.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection jotform.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{formsPageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(jotform.NewListFormsStep(jotform.ListFormsStepConfig[jotform.ListFormsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Jotform", Explanation: "Call the provider's listForms operation."},
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

// dex:field attribute-key:jotform-forms-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readFormPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"jotform-forms-page": outcome}}, nil
}

// dex:field attribute-key:jotform-forms-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readFormPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"jotform-forms-page": outcome}}, nil
}

func readFormPage(ctx dex.Context) (jotform.FormsPage, error) {
	outcome, err := formsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return jotform.FormsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input jotform.ListFormsInput) jotform.ListFormsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListForms" }
func (startRequest) WaitFor(dex.Context, jotform.ListFormsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input jotform.ListFormsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[jotform.ListFormsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[jotform.ListFormsResult]
}

func (completeResult) GetStepType() string { return "CompleteListForms" }
func (completeResult) Execute(ctx dex.Context, result jotform.ListFormsResult) (*dex.StepDecision, error) {
	if err := formsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
