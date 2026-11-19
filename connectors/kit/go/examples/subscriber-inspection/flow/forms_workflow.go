// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package subscriberinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/kit/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// FormsFlowType is the form-directory example Flow identity.
const FormsFlowType = "KitFormsInspection"

const listFormsStepType = "CallListForms"

var formsPageAttribute = dex.DefineAttribute[kit.FormsPage]("kit-forms-page")

// FormsFlow lists one page of Kit forms.
type FormsFlow struct {
	dex.FlowDefaults
	connection kit.Connection
}

// NewFormsFlow binds the Kit connection at registration time.
func NewFormsFlow(connection kit.Connection) *FormsFlow { return &FormsFlow{connection: connection} }

func (*FormsFlow) GetFlowType() string { return FormsFlowType }

func (*FormsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{formsPageAttribute}}
}

func (flow *FormsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startForms{}),
		dex.DefineStep(kit.NewListFormsStep(kit.ListFormsStepConfig[kit.ListFormsInput]{
			StepType: listFormsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Kit", Explanation: "List one page of forms."},
			Connection:  flow.connection, MapToOperationInput: MapToListFormsInput,
			Listed: sdkgo.GoTo(completeForms{}),
		})),
		dex.DefineStep(completeForms{}),
	}
}

func (flow *FormsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:kit-forms-page value-type:json editable:false description:"Form page"
// GetDexSummary shows the form page in Dex Web.
func (*FormsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readFormsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kit-forms-page": page}}, nil
}

// dex:field attribute-key:kit-forms-page value-type:json editable:false description:"Form page"
// GetDexDisplay shows the form page in Dex Web.
func (*FormsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readFormsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kit-forms-page": page}}, nil
}

func readFormsPage(ctx dex.Context) (kit.FormsPage, error) {
	page, err := formsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return kit.FormsPage{}, nil
	}
	return page, err
}

// MapToListFormsInput passes the typed page request to Kit.
func MapToListFormsInput(input kit.ListFormsInput) kit.ListFormsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested form page to the provider Step."
type startForms struct{ dex.StepDefaults }

func (startForms) GetStepType() string { return "StartListForms" }
func (startForms) WaitFor(dex.Context, kit.ListFormsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startForms) Execute(_ dex.Context, input kit.ListFormsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[kit.ListFormsInput](listFormsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with one typed form page."
type completeForms struct {
	dex.StepDefaultsNoWaitFor[kit.ListFormsResult]
}

func (completeForms) GetStepType() string { return "CompleteListForms" }
func (completeForms) Execute(ctx dex.Context, result kit.ListFormsResult) (*dex.StepDecision, error) {
	if err := formsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*FormsFlow)(nil)
