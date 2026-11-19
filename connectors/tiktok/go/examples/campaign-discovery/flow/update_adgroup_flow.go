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
	// UpdateAdgroupFlowType identifies this runnable adgroup Flow.
	updateAdgroupFlowType = "TikTokUpdateAdgroup"
	updateAdgroupStepType = "CallUpdateAdgroup"
)

var updateAdgroupAttribute = dex.DefineAttribute[tiktok.AdgroupWriteResult]("tiktok-updated-adgroup")

// UpdateAdgroupFlow updates one advertiser adgroup.
type UpdateAdgroupFlow struct {
	dex.FlowDefaults
	connection tiktok.Connection
}

// NewUpdateAdgroupFlow binds the provider connection at registration time.
func NewUpdateAdgroupFlow(connection tiktok.Connection) *UpdateAdgroupFlow {
	return &UpdateAdgroupFlow{connection: connection}
}

func (*UpdateAdgroupFlow) GetFlowType() string { return updateAdgroupFlowType }

func (*UpdateAdgroupFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{updateAdgroupAttribute}}
}

func (flow *UpdateAdgroupFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startUpdateAdgroup{}),
		dex.DefineStep(tiktok.NewUpdateAdgroupStep(tiktok.UpdateAdgroupStepConfig[tiktok.UpdateAdgroupInput]{
			StepType: updateAdgroupStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "TikTok", Explanation: "Update one adgroup."},
			MapToOperationInput: mapUpdateAdgroupInput, Updated: sdkgo.GoTo(completeUpdateAdgroup{}),
		})),
		dex.DefineStep(completeUpdateAdgroup{}),
	}
}

func (flow *UpdateAdgroupFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:tiktok-updated-adgroup value-type:json editable:false description:"Adgroup ID"
// GetDexSummary shows the confirmed adgroup ID.
func (*UpdateAdgroupFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readUpdateAdgroup(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-updated-adgroup": result}}, nil
}

// dex:field attribute-key:tiktok-updated-adgroup value-type:json editable:false description:"Adgroup ID"
// GetDexDisplay shows the confirmed adgroup ID.
func (*UpdateAdgroupFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readUpdateAdgroup(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-updated-adgroup": result}}, nil
}

func readUpdateAdgroup(ctx dex.Context) (tiktok.AdgroupWriteResult, error) {
	result, err := updateAdgroupAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tiktok.AdgroupWriteResult{}, nil
	}
	return result, err
}

func mapUpdateAdgroupInput(input tiktok.UpdateAdgroupInput) tiktok.UpdateAdgroupInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the advertiser and adgroup settings to TikTok."
type startUpdateAdgroup struct{ dex.StepDefaults }

func (startUpdateAdgroup) GetStepType() string { return "StartUpdateAdgroup" }
func (startUpdateAdgroup) WaitFor(dex.Context, tiktok.UpdateAdgroupInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startUpdateAdgroup) Execute(_ dex.Context, input tiktok.UpdateAdgroupInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tiktok.UpdateAdgroupInput](updateAdgroupStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after TikTok confirms the adgroup ID."
type completeUpdateAdgroup struct {
	dex.StepDefaultsNoWaitFor[tiktok.UpdateAdgroupResult]
}

func (completeUpdateAdgroup) GetStepType() string { return "CompleteUpdateAdgroup" }
func (completeUpdateAdgroup) Execute(ctx dex.Context, result tiktok.UpdateAdgroupResult) (*dex.StepDecision, error) {
	if err := updateAdgroupAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*UpdateAdgroupFlow)(nil)
