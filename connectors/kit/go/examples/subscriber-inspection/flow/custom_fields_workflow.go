// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package subscriberinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/kit/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CustomFieldsFlowType is the custom-field directory example Flow identity.
const CustomFieldsFlowType = "KitCustomFieldsInspection"

const listCustomFieldsStepType = "CallListCustomFields"

var customFieldsPageAttribute = dex.DefineAttribute[kit.CustomFieldsPage]("kit-custom-fields-page")

// CustomFieldsFlow lists one page of Kit customFields.
type CustomFieldsFlow struct {
	dex.FlowDefaults
	connection kit.Connection
}

// NewCustomFieldsFlow binds the Kit connection at registration time.
func NewCustomFieldsFlow(connection kit.Connection) *CustomFieldsFlow {
	return &CustomFieldsFlow{connection: connection}
}

func (*CustomFieldsFlow) GetFlowType() string { return CustomFieldsFlowType }

func (*CustomFieldsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{customFieldsPageAttribute}}
}

func (flow *CustomFieldsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCustomFields{}),
		dex.DefineStep(kit.NewListCustomFieldsStep(kit.ListCustomFieldsStepConfig[kit.ListCustomFieldsInput]{
			StepType: listCustomFieldsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Kit", Explanation: "List one page of customFields."},
			Connection:  flow.connection, MapToOperationInput: MapToListCustomFieldsInput,
			Listed: sdkgo.GoTo(completeCustomFields{}),
		})),
		dex.DefineStep(completeCustomFields{}),
	}
}

func (flow *CustomFieldsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:kit-custom-fields-page value-type:json editable:false description:"Custom field page"
// GetDexSummary shows the customField page in Dex Web.
func (*CustomFieldsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCustomFieldsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kit-custom-fields-page": page}}, nil
}

// dex:field attribute-key:kit-custom-fields-page value-type:json editable:false description:"Custom field page"
// GetDexDisplay shows the customField page in Dex Web.
func (*CustomFieldsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCustomFieldsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"kit-custom-fields-page": page}}, nil
}

func readCustomFieldsPage(ctx dex.Context) (kit.CustomFieldsPage, error) {
	page, err := customFieldsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return kit.CustomFieldsPage{}, nil
	}
	return page, err
}

// MapToListCustomFieldsInput passes the typed page request to Kit.
func MapToListCustomFieldsInput(input kit.ListCustomFieldsInput) kit.ListCustomFieldsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested customField page to the provider Step."
type startCustomFields struct{ dex.StepDefaults }

func (startCustomFields) GetStepType() string { return "StartListCustomFields" }
func (startCustomFields) WaitFor(dex.Context, kit.ListCustomFieldsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCustomFields) Execute(_ dex.Context, input kit.ListCustomFieldsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[kit.ListCustomFieldsInput](listCustomFieldsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with one typed customField page."
type completeCustomFields struct {
	dex.StepDefaultsNoWaitFor[kit.ListCustomFieldsResult]
}

func (completeCustomFields) GetStepType() string { return "CompleteListCustomFields" }
func (completeCustomFields) Execute(ctx dex.Context, result kit.ListCustomFieldsResult) (*dex.StepDecision, error) {
	if err := customFieldsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CustomFieldsFlow)(nil)
