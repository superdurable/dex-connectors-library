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
	// UpdateAdFlowType identifies this runnable ad Flow.
	updateAdFlowType = "TikTokUpdateAd"
	updateAdStepType = "CallUpdateAd"
)

var updateAdAttribute = dex.DefineAttribute[tiktok.AdWriteResult]("tiktok-updated-ad")

// UpdateAdFlow updates one advertiser ad.
type UpdateAdFlow struct {
	dex.FlowDefaults
	connection tiktok.Connection
}

// NewUpdateAdFlow binds the provider connection at registration time.
func NewUpdateAdFlow(connection tiktok.Connection) *UpdateAdFlow {
	return &UpdateAdFlow{connection: connection}
}

func (*UpdateAdFlow) GetFlowType() string { return updateAdFlowType }

func (*UpdateAdFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{updateAdAttribute}}
}

func (flow *UpdateAdFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startUpdateAd{}),
		dex.DefineStep(tiktok.NewUpdateAdStep(tiktok.UpdateAdStepConfig[tiktok.UpdateAdInput]{
			StepType: updateAdStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "TikTok", Explanation: "Update one ad."},
			MapToOperationInput: mapUpdateAdInput, Updated: sdkgo.GoTo(completeUpdateAd{}),
		})),
		dex.DefineStep(completeUpdateAd{}),
	}
}

func (flow *UpdateAdFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:tiktok-updated-ad value-type:json editable:false description:"Ad ID"
// GetDexSummary shows the confirmed ad ID.
func (*UpdateAdFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readUpdateAd(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-updated-ad": result}}, nil
}

// dex:field attribute-key:tiktok-updated-ad value-type:json editable:false description:"Ad ID"
// GetDexDisplay shows the confirmed ad ID.
func (*UpdateAdFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readUpdateAd(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-updated-ad": result}}, nil
}

func readUpdateAd(ctx dex.Context) (tiktok.AdWriteResult, error) {
	result, err := updateAdAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tiktok.AdWriteResult{}, nil
	}
	return result, err
}

func mapUpdateAdInput(input tiktok.UpdateAdInput) tiktok.UpdateAdInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the advertiser and ad settings to TikTok."
type startUpdateAd struct{ dex.StepDefaults }

func (startUpdateAd) GetStepType() string { return "StartUpdateAd" }
func (startUpdateAd) WaitFor(dex.Context, tiktok.UpdateAdInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startUpdateAd) Execute(_ dex.Context, input tiktok.UpdateAdInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tiktok.UpdateAdInput](updateAdStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after TikTok confirms the ad ID."
type completeUpdateAd struct {
	dex.StepDefaultsNoWaitFor[tiktok.UpdateAdResult]
}

func (completeUpdateAd) GetStepType() string { return "CompleteUpdateAd" }
func (completeUpdateAd) Execute(ctx dex.Context, result tiktok.UpdateAdResult) (*dex.StepDecision, error) {
	if err := updateAdAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*UpdateAdFlow)(nil)
