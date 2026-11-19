// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package transactioninspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/paddle/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// CustomerFlowType is the stable Paddle customer listing Flow identity.
const CustomerFlowType = "PaddleCustomerListing"

const customerStepType = "CallListCustomers"

var customerPageAttribute = dex.DefineAttribute[paddle.CustomerPage]("paddle-customer-page")

// CustomerFlow lists one page of Paddle customers.
type CustomerFlow struct {
	dex.FlowDefaults
	connection paddle.Connection
}

// NewCustomerFlow binds the Paddle connection for listing customers.
func NewCustomerFlow(connection paddle.Connection) *CustomerFlow { return &CustomerFlow{connection: connection} }

func (*CustomerFlow) GetFlowType() string { return CustomerFlowType }

func (*CustomerFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{customerPageAttribute}}
}

func (flow *CustomerFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCustomerList{}),
		dex.DefineStep(paddle.NewListCustomersStep(paddle.ListCustomersStepConfig[paddle.ListCustomersInput]{
			StepType: customerStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Paddle", Explanation: "List one page of Paddle customers."},
			Connection: flow.connection, MapToOperationInput: mapCustomerListInput,
			Listed: sdkgo.GoTo(completeCustomerList{}),
		})),
		dex.DefineStep(completeCustomerList{}),
	}
}

func (flow *CustomerFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:paddle-customer-page value-type:json editable:false description:"Customer page"
func (*CustomerFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCustomerPage(ctx)
	if err != nil { return nil, err }
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"paddle-customer-page": page}}, nil
}

// dex:field attribute-key:paddle-customer-page value-type:json editable:false description:"Customer page"
func (*CustomerFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readCustomerPage(ctx)
	if err != nil { return nil, err }
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"paddle-customer-page": page}}, nil
}

func readCustomerPage(ctx dex.Context) (paddle.CustomerPage, error) {
	page, err := customerPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) { return paddle.CustomerPage{}, nil }
	return page, err
}

func mapCustomerListInput(input paddle.ListCustomersInput) paddle.ListCustomersInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested page size and cursor to Paddle."
type startCustomerList struct{ dex.StepDefaults }

func (startCustomerList) GetStepType() string { return "StartListCustomers" }
func (startCustomerList) WaitFor(dex.Context, paddle.ListCustomersInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCustomerList) Execute(_ dex.Context, input paddle.ListCustomersInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[paddle.ListCustomersInput](customerStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the returned customer page."
type completeCustomerList struct{ dex.StepDefaultsNoWaitFor[paddle.ListCustomersResult] }

func (completeCustomerList) GetStepType() string { return "CompleteListCustomers" }
func (completeCustomerList) Execute(ctx dex.Context, result paddle.ListCustomersResult) (*dex.StepDecision, error) {
	if err := customerPageAttribute.Set(ctx, result.Value); err != nil { return nil, err }
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CustomerFlow)(nil)
