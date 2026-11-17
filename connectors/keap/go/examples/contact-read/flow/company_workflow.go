// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactread

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/keap/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CompanyFlowType identifies the company lookup example Flow.
const CompanyFlowType = "KeapCompanyLookup"

const getCompanyStepType = "CallGetCompany"

var companyProfileAttribute = dex.DefineAttribute[keap.CompanyProfile]("keap-company-profile")

// CompanyFlow reads a Keap company using one connector Step.
type CompanyFlow struct {
	dex.FlowDefaults
	connection keap.Connection
}

// NewCompanyFlow binds the named Keap connection at registration.
func NewCompanyFlow(connection keap.Connection) *CompanyFlow {
	return &CompanyFlow{connection: connection}
}

func (*CompanyFlow) GetFlowType() string { return CompanyFlowType }

func (*CompanyFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{companyProfileAttribute}}
}

func (flow *CompanyFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCompanyLookup{}),
		dex.DefineStep(keap.NewGetCompanyStep(keap.GetCompanyStepConfig[keap.CompanyIDInput]{
			StepType: getCompanyStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Keap", Explanation: "Read one CRM company."},
			Connection:  flow.connection, MapToOperationInput: mapCompanyID,
			Found: sdkgo.GoTo(completeCompanyLookup{}),
		})),
		dex.DefineStep(completeCompanyLookup{}),
	}
}

func (flow *CompanyFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:keap-company-profile value-type:json editable:false description:"Company profile"
// GetDexSummary shows the company profile in Dex Web.
func (*CompanyFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	profile, err := readCompanyProfile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"keap-company-profile": profile}}, nil
}

// dex:field attribute-key:keap-company-profile value-type:json editable:false description:"Company profile"
// GetDexDisplay shows the company profile in Dex Web.
func (*CompanyFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	profile, err := readCompanyProfile(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"keap-company-profile": profile}}, nil
}

func readCompanyProfile(ctx dex.Context) (keap.CompanyProfile, error) {
	profile, err := companyProfileAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return keap.CompanyProfile{}, nil
	}
	return profile, err
}

func mapCompanyID(input keap.CompanyIDInput) keap.CompanyIDInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the selected company ID to the provider Step."
type startCompanyLookup struct{ dex.StepDefaults }

func (startCompanyLookup) GetStepType() string { return "StartCompanyLookup" }
func (startCompanyLookup) WaitFor(dex.Context, keap.CompanyIDInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCompanyLookup) Execute(_ dex.Context, input keap.CompanyIDInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[keap.CompanyIDInput](getCompanyStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the returned company and complete the Flow."
type completeCompanyLookup struct {
	dex.StepDefaultsNoWaitFor[keap.GetCompanyResult]
}

func (completeCompanyLookup) GetStepType() string { return "CompleteCompanyLookup" }
func (completeCompanyLookup) Execute(ctx dex.Context, result keap.GetCompanyResult) (*dex.StepDecision, error) {
	if err := companyProfileAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CompanyFlow)(nil)
