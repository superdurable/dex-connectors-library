// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/adobe/marketo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// UpsertCompanyFlowType is the stable company upsert Flow identity.
const UpsertCompanyFlowType = "MarketoUpsertCompany"
const upsertCompanyStepType = "CallMarketoUpsertCompany"

var companyWriteAttribute = dex.DefineAttribute[marketo.CompanyWriteResult]("marketo-company-write")

// UpsertCompanyFlow creates or updates one company by external ID and stores its returned ID.
type UpsertCompanyFlow struct {
	dex.FlowDefaults
	connection marketo.Connection
}

// NewUpsertCompanyFlow binds the static Marketo connection.
func NewUpsertCompanyFlow(connection marketo.Connection) *UpsertCompanyFlow {
	return &UpsertCompanyFlow{connection: connection}
}

// GetFlowType identifies the upsert example.
func (*UpsertCompanyFlow) GetFlowType() string { return UpsertCompanyFlowType }

// GetPersistenceSchema registers the synced company result.
func (*UpsertCompanyFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{companyWriteAttribute}}
}

// GetSteps registers the upsert and completion boundaries.
func (flow *UpsertCompanyFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startUpsertCompany{}),
		dex.DefineStep(marketo.NewUpsertCompanyStep(marketo.UpsertCompanyStepConfig[marketo.UpsertCompanyInput]{
			StepType: upsertCompanyStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Marketo", Explanation: "Create or update a company by external ID."},
			Connection:  flow.connection, MapToOperationInput: mapUpsertCompanyInput,
			Synced: sdkgo.GoTo(completeUpsertCompany{}),
		})),
		dex.DefineStep(completeUpsertCompany{}),
	}
}

// GetRPCs exposes the result to Dex Web.
func (flow *UpsertCompanyFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:marketo-company-write value-type:json editable:false description:"Synced Marketo company"
// GetDexSummary returns the synced company ID.
func (*UpsertCompanyFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readCompanyWriteResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-company-write": result}}, nil
}

// dex:field attribute-key:marketo-company-write value-type:json editable:false description:"Synced Marketo company"
// GetDexDisplay returns the synced company ID.
func (*UpsertCompanyFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readCompanyWriteResult(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-company-write": result}}, nil
}

func readCompanyWriteResult(ctx dex.Context) (marketo.CompanyWriteResult, error) {
	result, err := companyWriteAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return marketo.CompanyWriteResult{}, nil
	}
	return result, err
}

func mapUpsertCompanyInput(input marketo.UpsertCompanyInput) marketo.UpsertCompanyInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the external company ID and name to Marketo."
type startUpsertCompany struct{ dex.StepDefaults }

func (startUpsertCompany) GetStepType() string { return "StartMarketoUpsertCompany" }
func (startUpsertCompany) WaitFor(dex.Context, marketo.UpsertCompanyInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startUpsertCompany) Execute(_ dex.Context, input marketo.UpsertCompanyInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[marketo.UpsertCompanyInput](upsertCompanyStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the synced company ID and complete the Flow."
type completeUpsertCompany struct {
	dex.StepDefaultsNoWaitFor[marketo.UpsertCompanyResult]
}

func (completeUpsertCompany) GetStepType() string { return "CompleteMarketoUpsertCompany" }
func (completeUpsertCompany) Execute(ctx dex.Context, result marketo.UpsertCompanyResult) (*dex.StepDecision, error) {
	if err := companyWriteAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*UpsertCompanyFlow)(nil)
