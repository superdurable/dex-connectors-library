// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package campaigndiscovery

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/tiktok/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// CreateAdgroupFlowType identifies this runnable adgroup Flow.
	createAdgroupFlowType = "TikTokCreateAdgroup"
	createAdgroupStepType = "CallCreateAdgroup"
)

var createAdgroupAttribute = dex.DefineAttribute[tiktok.AdgroupWriteResult]("tiktok-created-adgroup")

// CreateAdgroupFlow creates one advertiser adgroup.
type CreateAdgroupFlow struct {
	dex.FlowDefaults
	connection tiktok.Connection
}

// NewCreateAdgroupFlow binds the provider connection at registration time.
func NewCreateAdgroupFlow(connection tiktok.Connection) *CreateAdgroupFlow {
	return &CreateAdgroupFlow{connection: connection}
}

func (*CreateAdgroupFlow) GetFlowType() string { return createAdgroupFlowType }

func (*CreateAdgroupFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createAdgroupAttribute}}
}

func (flow *CreateAdgroupFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateAdgroup{}),
		dex.DefineStep(tiktok.NewCreateAdgroupStep(tiktok.CreateAdgroupStepConfig[tiktok.CreateAdgroupInput]{
			StepType: createAdgroupStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "TikTok", Explanation: "Create one adgroup."},
			MapToOperationInput: mapCreateAdgroupInput, Created: sdkgo.GoTo(completeCreateAdgroup{}),
		})),
		dex.DefineStep(completeCreateAdgroup{}),
	}
}

func (flow *CreateAdgroupFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:tiktok-created-adgroup value-type:json editable:false description:"Adgroup ID"
// GetDexSummary shows the confirmed adgroup ID.
func (*CreateAdgroupFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readCreateAdgroup(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-created-adgroup": result}}, nil
}

// dex:field attribute-key:tiktok-created-adgroup value-type:json editable:false description:"Adgroup ID"
// GetDexDisplay shows the confirmed adgroup ID.
func (*CreateAdgroupFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readCreateAdgroup(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-created-adgroup": result}}, nil
}

func readCreateAdgroup(ctx dex.Context) (tiktok.AdgroupWriteResult, error) {
	result, err := createAdgroupAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tiktok.AdgroupWriteResult{}, nil
	}
	return result, err
}

func mapCreateAdgroupInput(input tiktok.CreateAdgroupInput) tiktok.CreateAdgroupInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the advertiser and adgroup settings to TikTok."
type startCreateAdgroup struct{ dex.StepDefaults }

func (startCreateAdgroup) GetStepType() string { return "StartCreateAdgroup" }
func (startCreateAdgroup) WaitFor(dex.Context, tiktok.CreateAdgroupInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateAdgroup) Execute(_ dex.Context, input tiktok.CreateAdgroupInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tiktok.CreateAdgroupInput](createAdgroupStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after TikTok confirms the adgroup ID."
type completeCreateAdgroup struct {
	dex.StepDefaultsNoWaitFor[tiktok.CreateAdgroupResult]
}

func (completeCreateAdgroup) GetStepType() string { return "CompleteCreateAdgroup" }
func (completeCreateAdgroup) Execute(ctx dex.Context, result tiktok.CreateAdgroupResult) (*dex.StepDecision, error) {
	if err := createAdgroupAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateAdgroupFlow)(nil)
