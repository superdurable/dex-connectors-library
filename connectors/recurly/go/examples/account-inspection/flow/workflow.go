// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package accountinspection runs Recurly getAccount as one Dex Flow.
package accountinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/recurly/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "RecurlyAccountInspection"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "recurly"
	operationStepType = "CallGetAccount"
)

var accountAttribute = dex.DefineAttribute[recurly.AccountIdentity]("recurly-account")

// Flow runs GetAccount once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection recurly.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection recurly.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{accountAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(recurly.NewGetAccountStep(recurly.GetAccountStepConfig[recurly.GetAccountInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Recurly", Explanation: "Read a account from Recurly."},
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

// dex:field attribute-key:recurly-account value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"recurly-account": outcome}}, nil
}

// dex:field attribute-key:recurly-account value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"recurly-account": outcome}}, nil
}

func readAccount(ctx dex.Context) (recurly.AccountIdentity, error) {
	outcome, err := accountAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return recurly.AccountIdentity{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input recurly.GetAccountInput) recurly.GetAccountInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartGetAccount" }
func (startRequest) WaitFor(dex.Context, recurly.GetAccountInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input recurly.GetAccountInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[recurly.GetAccountInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[recurly.GetAccountResult]
}

func (completeResult) GetStepType() string { return "CompleteGetAccount" }
func (completeResult) Execute(ctx dex.Context, result recurly.GetAccountResult) (*dex.StepDecision, error) {
	if err := accountAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
