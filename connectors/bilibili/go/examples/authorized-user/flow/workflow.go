// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package authorizeduser runs Bilibili getAuthorizedUser as one Dex Flow.
package authorizeduser

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/bilibili/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "BilibiliAuthorizedUser"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "bilibili"
	operationStepType = "CallGetAuthorizedUser"
)

var userAttribute = dex.DefineAttribute[bilibili.AuthorizedUser]("bilibili-authorized-user")

// Flow runs GetAuthorizedUser once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection bilibili.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection bilibili.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{userAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(bilibili.NewGetAuthorizedUserStep(bilibili.GetAuthorizedUserStepConfig[bilibili.GetAuthorizedUserInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Bilibili", Explanation: "Read the authorized Bilibili authorized account."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Found:               sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:bilibili-authorized-user value-type:json editable:false description:"Bilibili authorized account information"
// GetDexSummary shows the last Bilibili authorized account information in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUser(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bilibili-authorized-user": outcome}}, nil
}

// dex:field attribute-key:bilibili-authorized-user value-type:json editable:false description:"Bilibili authorized account information"
// GetDexDisplay shows the last Bilibili authorized account information in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readUser(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"bilibili-authorized-user": outcome}}, nil
}

func readUser(ctx dex.Context) (bilibili.AuthorizedUser, error) {
	outcome, err := userAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return bilibili.AuthorizedUser{}, nil
	}
	return outcome, err
}

// MapToOperationInput passes the typed start input for the provider call.
func MapToOperationInput(input bilibili.GetAuthorizedUserInput) bilibili.GetAuthorizedUserInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetAuthorizedUser" }
func (startRequest) WaitFor(dex.Context, bilibili.GetAuthorizedUserInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input bilibili.GetAuthorizedUserInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[bilibili.GetAuthorizedUserInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[bilibili.GetAuthorizedUserResult]
}

func (completeResult) GetStepType() string { return "CompleteGetAuthorizedUser" }
func (completeResult) Execute(ctx dex.Context, result bilibili.GetAuthorizedUserResult) (*dex.StepDecision, error) {
	if err := userAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
