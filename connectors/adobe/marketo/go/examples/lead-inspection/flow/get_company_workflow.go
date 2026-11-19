// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package leadinspection

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/adobe/marketo/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// GetCompanyByExternalIDFlowType is the stable company lookup Flow identity.
const GetCompanyByExternalIDFlowType = "MarketoGetCompanyByExternalID"
const getCompanyByExternalIDStepType = "CallGetCompanyByExternalID"

var companyProfileAttribute = dex.DefineAttribute[marketo.CompanyProfile]("marketo-company-profile")

// GetCompanyByExternalIDFlow reads a company by external ID and stores its profile.
type GetCompanyByExternalIDFlow struct {
	dex.FlowDefaults
	connection marketo.Connection
}

// NewGetCompanyByExternalIDFlow binds the provider connection at registration.
func NewGetCompanyByExternalIDFlow(connection marketo.Connection) *GetCompanyByExternalIDFlow {
	return &GetCompanyByExternalIDFlow{connection: connection}
}
func (*GetCompanyByExternalIDFlow) GetFlowType() string { return GetCompanyByExternalIDFlowType }
func (*GetCompanyByExternalIDFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{companyProfileAttribute}}
}
func (flow *GetCompanyByExternalIDFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startGetCompanyByExternalID{}),
		dex.DefineStep(marketo.NewGetCompanyByExternalIDStep(marketo.GetCompanyByExternalIDStepConfig[marketo.GetCompanyByExternalIDInput]{
			StepType: getCompanyByExternalIDStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Marketo", Explanation: "Read one company profile by external ID."},
			Connection:  flow.connection, MapToOperationInput: mapGetCompanyByExternalIDInput,
			Found: sdkgo.GoTo(completeGetCompanyByExternalID{}),
		})),
		dex.DefineStep(completeGetCompanyByExternalID{}),
	}
}
func (flow *GetCompanyByExternalIDFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:marketo-company-profile value-type:json editable:false description:"Matching company profile"
// GetDexSummary shows the matching page.
func (*GetCompanyByExternalIDFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCompanyProfile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-company-profile": page}}, nil
}

// dex:field attribute-key:marketo-company-profile value-type:json editable:false description:"Matching company profile"
// GetDexDisplay shows the matching page.
func (*GetCompanyByExternalIDFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCompanyProfile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"marketo-company-profile": page}}, nil
}
func readCompanyProfile(ctx dex.Context) (marketo.CompanyProfile, error) {
	page, err := companyProfileAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return marketo.CompanyProfile{}, nil
	}
	return page, err
}
func mapGetCompanyByExternalIDInput(input marketo.GetCompanyByExternalIDInput) marketo.GetCompanyByExternalIDInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the email search input to the provider Step."
type startGetCompanyByExternalID struct{ dex.StepDefaults }

func (startGetCompanyByExternalID) GetStepType() string { return "StartGetCompanyByExternalID" }
func (startGetCompanyByExternalID) WaitFor(dex.Context, marketo.GetCompanyByExternalIDInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startGetCompanyByExternalID) Execute(_ dex.Context, input marketo.GetCompanyByExternalIDInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[marketo.GetCompanyByExternalIDInput](getCompanyByExternalIDStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the company profile and complete the Flow."
type completeGetCompanyByExternalID struct {
	dex.StepDefaultsNoWaitFor[marketo.GetCompanyByExternalIDResult]
}

func (completeGetCompanyByExternalID) GetStepType() string { return "CompleteGetCompanyByExternalID" }
func (completeGetCompanyByExternalID) Execute(ctx dex.Context, result marketo.GetCompanyByExternalIDResult) (*dex.StepDecision, error) {
	if err := companyProfileAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*GetCompanyByExternalIDFlow)(nil)
