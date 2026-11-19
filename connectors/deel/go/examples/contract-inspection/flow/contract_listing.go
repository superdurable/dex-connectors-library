// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contractinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/deel/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ContractListingFlowType is the stable identity of the contract listing example.
const ContractListingFlowType = "DeelContractListing"

var contractPageAttribute = dex.DefineAttribute[deel.ContractPage]("deel-contract-page")

// ContractListingFlow lists one bounded page of contract summaries.
type ContractListingFlow struct {
	dex.FlowDefaults
	connection deel.Connection
}

// NewContractListingFlow binds the static Deel connection.
func NewContractListingFlow(connection deel.Connection) *ContractListingFlow {
	return &ContractListingFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*ContractListingFlow) GetFlowType() string { return ContractListingFlowType }

// GetPersistenceSchema registers the selected contract page.
func (*ContractListingFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contractPageAttribute}}
}

// GetRPCs exposes the contract page in Dex Web.
func (flow *ContractListingFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary shows the last contract page in Dex Web.
// dex:field attribute-key:deel-contract-page value-type:json editable:false description:"Contract summaries"
func (*ContractListingFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readContractPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"deel-contract-page": page}}, nil
}

// GetDexDisplay shows the last contract page in Dex Web.
// dex:field attribute-key:deel-contract-page value-type:json editable:false description:"Contract summaries"
func (*ContractListingFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readContractPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"deel-contract-page": page}}, nil
}

func readContractPage(ctx dex.Context) (deel.ContractPage, error) {
	page, err := contractPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return deel.ContractPage{}, nil
	}
	return page, err
}

// GetSteps registers the request, connector, and completion Steps.
func (flow *ContractListingFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startContractListing{}),
		dex.DefineStep(deel.NewListContractsStep(deel.ListContractsStepConfig[deel.ListContractsInput]{
			StepType: "CallListContracts", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Deel", Explanation: "List one page of Deel contracts."},
			Connection:  flow.connection, MapToOperationInput: mapContractListingInput,
			Listed: sdkgo.GoTo(completeContractListing{}),
		})),
		dex.DefineStep(completeContractListing{}),
	}
}

func mapContractListingInput(input deel.ListContractsInput) deel.ListContractsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the contract page request to the provider Step."
type startContractListing struct{ dex.StepDefaults }

func (startContractListing) GetStepType() string { return "StartContractListing" }
func (startContractListing) WaitFor(dex.Context, deel.ListContractsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startContractListing) Execute(_ dex.Context, input deel.ListContractsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[deel.ListContractsInput]("CallListContracts"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the contract page and complete the Flow."
type completeContractListing struct {
	dex.StepDefaultsNoWaitFor[deel.ListContractsResult]
}

func (completeContractListing) GetStepType() string { return "CompleteContractListing" }
func (completeContractListing) Execute(ctx dex.Context, result deel.ListContractsResult) (*dex.StepDecision, error) {
	if err := contractPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ContractListingFlow)(nil)
