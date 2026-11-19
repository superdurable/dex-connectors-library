// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package customerinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/oracle/netsuite/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// ListFlowType is the stable customer listing Flow identity.
const ListFlowType = "OracleNetSuiteCustomerListing"

const listOperationStepType = "CallListCustomers"

var customerListAttribute = dex.DefineAttribute[oraclenetsuite.CustomerList]("oracle-netsuite-customer-list")

// ListFlow lists one page of customers.
type ListFlow struct {
	dex.FlowDefaults
	connection oraclenetsuite.Connection
}

// NewListFlow binds the connection for a customer listing.
func NewListFlow(connection oraclenetsuite.Connection) *ListFlow {
	return &ListFlow{connection: connection}
}

func (*ListFlow) GetFlowType() string { return ListFlowType }

func (*ListFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{customerListAttribute}}
}

func (flow *ListFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startListRequest{}),
		dex.DefineStep(oraclenetsuite.NewListCustomersStep(oraclenetsuite.ListCustomersStepConfig[oraclenetsuite.ListCustomersInput]{
			StepType: listOperationStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Oracle NetSuite", Explanation: "List one page of customer records from NetSuite."},
			Connection:  flow.connection, MapToOperationInput: mapListInput,
			Listed: sdkgo.GoTo(completeListResult{}),
		})),
		dex.DefineStep(completeListResult{}),
	}
}

func (flow *ListFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:oracle-netsuite-customer-list value-type:json editable:false description:"Customer page"
func (*ListFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCustomerList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"oracle-netsuite-customer-list": page}}, nil
}

// dex:field attribute-key:oracle-netsuite-customer-list value-type:json editable:false description:"Customer page"
func (*ListFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCustomerList(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"oracle-netsuite-customer-list": page}}, nil
}

func readCustomerList(ctx dex.Context) (oraclenetsuite.CustomerList, error) {
	page, err := customerListAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return oraclenetsuite.CustomerList{}, nil
	}
	return page, err
}

func mapListInput(input oraclenetsuite.ListCustomersInput) oraclenetsuite.ListCustomersInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested page size and offset to NetSuite."
type startListRequest struct{ dex.StepDefaults }

func (startListRequest) GetStepType() string { return "StartListCustomers" }
func (startListRequest) WaitFor(dex.Context, oraclenetsuite.ListCustomersInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startListRequest) Execute(_ dex.Context, input oraclenetsuite.ListCustomersInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[oraclenetsuite.ListCustomersInput](listOperationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the customer page."
type completeListResult struct {
	dex.StepDefaultsNoWaitFor[oraclenetsuite.ListCustomersResult]
}

func (completeListResult) GetStepType() string { return "CompleteListCustomers" }
func (completeListResult) Execute(ctx dex.Context, result oraclenetsuite.ListCustomersResult) (*dex.StepDecision, error) {
	if err := customerListAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*ListFlow)(nil)
