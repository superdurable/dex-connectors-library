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
	// AdStatusFlowType identifies this runnable status Flow.
	adStatusFlowType = "TikTokAdStatus"
	adStatusStepType = "CallUpdateAdStatus"
)

var adStatusAttribute = dex.DefineAttribute[tiktok.AdvertisingStatusResult]("tiktok-ad-status")

// AdStatusFlow sets the operation status of one ad.
type AdStatusFlow struct {
	dex.FlowDefaults
	connection tiktok.Connection
}

// NewAdStatusFlow binds the provider connection at registration time.
func NewAdStatusFlow(connection tiktok.Connection) *AdStatusFlow {
	return &AdStatusFlow{connection: connection}
}

func (*AdStatusFlow) GetFlowType() string { return adStatusFlowType }

func (*AdStatusFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{adStatusAttribute}}
}

func (flow *AdStatusFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAdStatus{}),
		dex.DefineStep(tiktok.NewUpdateAdStatusStep(tiktok.UpdateAdStatusStepConfig[tiktok.UpdateAdStatusInput]{
			StepType: adStatusStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "TikTok", Explanation: "Set ad operation status."},
			MapToOperationInput: mapAdStatusInput, Updated: sdkgo.GoTo(completeAdStatus{}),
		})),
		dex.DefineStep(completeAdStatus{}),
	}
}

func (flow *AdStatusFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:tiktok-ad-status value-type:json editable:false description:"Confirmed status"
// GetDexSummary shows the confirmed status.
func (*AdStatusFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readAdStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-ad-status": result}}, nil
}

// dex:field attribute-key:tiktok-ad-status value-type:json editable:false description:"Confirmed status"
// GetDexDisplay shows the confirmed status.
func (*AdStatusFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readAdStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-ad-status": result}}, nil
}

func readAdStatus(ctx dex.Context) (tiktok.AdvertisingStatusResult, error) {
	result, err := adStatusAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tiktok.AdvertisingStatusResult{}, nil
	}
	return result, err
}

func mapAdStatusInput(input tiktok.UpdateAdStatusInput) tiktok.UpdateAdStatusInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the selected ad and absolute status to TikTok."
type startAdStatus struct{ dex.StepDefaults }

func (startAdStatus) GetStepType() string { return "StartUpdateAdStatus" }
func (startAdStatus) WaitFor(dex.Context, tiktok.UpdateAdStatusInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAdStatus) Execute(_ dex.Context, input tiktok.UpdateAdStatusInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tiktok.UpdateAdStatusInput](adStatusStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after TikTok confirms the absolute status."
type completeAdStatus struct {
	dex.StepDefaultsNoWaitFor[tiktok.UpdateAdStatusResult]
}

func (completeAdStatus) GetStepType() string { return "CompleteUpdateAdStatus" }
func (completeAdStatus) Execute(ctx dex.Context, result tiktok.UpdateAdStatusResult) (*dex.StepDecision, error) {
	if err := adStatusAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AdStatusFlow)(nil)
