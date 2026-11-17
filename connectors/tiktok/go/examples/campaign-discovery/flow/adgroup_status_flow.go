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
	// AdgroupStatusFlowType identifies this runnable status Flow.
	adgroupStatusFlowType = "TikTokAdgroupStatus"
	adgroupStatusStepType = "CallUpdateAdgroupStatus"
)

var adgroupStatusAttribute = dex.DefineAttribute[tiktok.AdvertisingStatusResult]("tiktok-adgroup-status")

// AdgroupStatusFlow sets the operation status of one adgroup.
type AdgroupStatusFlow struct {
	dex.FlowDefaults
	connection tiktok.Connection
}

// NewAdgroupStatusFlow binds the provider connection at registration time.
func NewAdgroupStatusFlow(connection tiktok.Connection) *AdgroupStatusFlow {
	return &AdgroupStatusFlow{connection: connection}
}

func (*AdgroupStatusFlow) GetFlowType() string { return adgroupStatusFlowType }

func (*AdgroupStatusFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{adgroupStatusAttribute}}
}

func (flow *AdgroupStatusFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAdgroupStatus{}),
		dex.DefineStep(tiktok.NewUpdateAdgroupStatusStep(tiktok.UpdateAdgroupStatusStepConfig[tiktok.UpdateAdgroupStatusInput]{
			StepType: adgroupStatusStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "TikTok", Explanation: "Set adgroup operation status."},
			MapToOperationInput: mapAdgroupStatusInput, Updated: sdkgo.GoTo(completeAdgroupStatus{}),
		})),
		dex.DefineStep(completeAdgroupStatus{}),
	}
}

func (flow *AdgroupStatusFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:tiktok-adgroup-status value-type:json editable:false description:"Confirmed status"
// GetDexSummary shows the confirmed status.
func (*AdgroupStatusFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readAdgroupStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-adgroup-status": result}}, nil
}

// dex:field attribute-key:tiktok-adgroup-status value-type:json editable:false description:"Confirmed status"
// GetDexDisplay shows the confirmed status.
func (*AdgroupStatusFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readAdgroupStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-adgroup-status": result}}, nil
}

func readAdgroupStatus(ctx dex.Context) (tiktok.AdvertisingStatusResult, error) {
	result, err := adgroupStatusAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tiktok.AdvertisingStatusResult{}, nil
	}
	return result, err
}

func mapAdgroupStatusInput(input tiktok.UpdateAdgroupStatusInput) tiktok.UpdateAdgroupStatusInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the selected adgroup and absolute status to TikTok."
type startAdgroupStatus struct{ dex.StepDefaults }

func (startAdgroupStatus) GetStepType() string { return "StartUpdateAdgroupStatus" }
func (startAdgroupStatus) WaitFor(dex.Context, tiktok.UpdateAdgroupStatusInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAdgroupStatus) Execute(_ dex.Context, input tiktok.UpdateAdgroupStatusInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tiktok.UpdateAdgroupStatusInput](adgroupStatusStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after TikTok confirms the absolute status."
type completeAdgroupStatus struct {
	dex.StepDefaultsNoWaitFor[tiktok.UpdateAdgroupStatusResult]
}

func (completeAdgroupStatus) GetStepType() string { return "CompleteUpdateAdgroupStatus" }
func (completeAdgroupStatus) Execute(ctx dex.Context, result tiktok.UpdateAdgroupStatusResult) (*dex.StepDecision, error) {
	if err := adgroupStatusAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AdgroupStatusFlow)(nil)
