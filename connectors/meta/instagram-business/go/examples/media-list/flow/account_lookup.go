// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package medialist

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meta/instagram-business/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// AccountLookupFlowType is the stable account lookup Flow identity.
	AccountLookupFlowType = "InstagramBusinessAccountLookup"
	accountLookupStepType = "CallGetAccount"
)

var accountAttribute = dex.DefineAttribute[instagrambusiness.InstagramAccount]("instagram-business-account")

// AccountLookupFlow reads a professional account's public metadata.
type AccountLookupFlow struct {
	dex.FlowDefaults
	connection instagrambusiness.Connection
}

// NewAccountLookupFlow binds the provider connection at registration time.
func NewAccountLookupFlow(connection instagrambusiness.Connection) *AccountLookupFlow {
	return &AccountLookupFlow{connection: connection}
}

func (*AccountLookupFlow) GetFlowType() string { return AccountLookupFlowType }

func (*AccountLookupFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{accountAttribute}}
}

func (flow *AccountLookupFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAccountLookup{}),
		dex.DefineStep(instagrambusiness.NewGetAccountStep(instagrambusiness.GetAccountStepConfig[instagrambusiness.GetAccountInput]{
			StepType: accountLookupStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Instagram Business", Explanation: "Read the professional account's public metadata."},
			Connection:          flow.connection,
			MapToOperationInput: mapToAccountLookupInput,
			Found:               sdkgo.GoTo(completeAccountLookup{}),
		})),
		dex.DefineStep(completeAccountLookup{}),
	}
}

func (flow *AccountLookupFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:instagram-business-account value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last account result in Dex Web.
func (*AccountLookupFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	account, err := readAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"instagram-business-account": account}}, nil
}

// dex:field attribute-key:instagram-business-account value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last account result in Dex Web.
func (*AccountLookupFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	account, err := readAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"instagram-business-account": account}}, nil
}

func readAccount(ctx dex.Context) (instagrambusiness.InstagramAccount, error) {
	account, err := accountAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return instagrambusiness.InstagramAccount{}, nil
	}
	return account, err
}

func mapToAccountLookupInput(input instagrambusiness.GetAccountInput) instagrambusiness.GetAccountInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the account ID to the provider Step."
type startAccountLookup struct{ dex.StepDefaults }

func (startAccountLookup) GetStepType() string { return "StartGetAccount" }
func (startAccountLookup) WaitFor(dex.Context, instagrambusiness.GetAccountInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAccountLookup) Execute(_ dex.Context, input instagrambusiness.GetAccountInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[instagrambusiness.GetAccountInput](accountLookupStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Store the account and complete the Flow."
type completeAccountLookup struct {
	dex.StepDefaultsNoWaitFor[instagrambusiness.GetAccountResult]
}

func (completeAccountLookup) GetStepType() string { return "CompleteGetAccount" }
func (completeAccountLookup) Execute(ctx dex.Context, result instagrambusiness.GetAccountResult) (*dex.StepDecision, error) {
	if err := accountAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AccountLookupFlow)(nil)
