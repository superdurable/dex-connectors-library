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
	// CreateAdFlowType identifies this runnable ad Flow.
	createAdFlowType = "TikTokCreateAd"
	createAdStepType = "CallCreateAd"
)

var createAdAttribute = dex.DefineAttribute[tiktok.AdWriteResult]("tiktok-created-ad")

// CreateAdFlow creates one advertiser ad.
type CreateAdFlow struct {
	dex.FlowDefaults
	connection tiktok.Connection
}

// NewCreateAdFlow binds the provider connection at registration time.
func NewCreateAdFlow(connection tiktok.Connection) *CreateAdFlow {
	return &CreateAdFlow{connection: connection}
}

func (*CreateAdFlow) GetFlowType() string { return createAdFlowType }

func (*CreateAdFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{createAdAttribute}}
}

func (flow *CreateAdFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startCreateAd{}),
		dex.DefineStep(tiktok.NewCreateAdStep(tiktok.CreateAdStepConfig[tiktok.CreateAdInput]{
			StepType: createAdStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "TikTok", Explanation: "Create one ad."},
			MapToOperationInput: mapCreateAdInput, Created: sdkgo.GoTo(completeCreateAd{}),
		})),
		dex.DefineStep(completeCreateAd{}),
	}
}

func (flow *CreateAdFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:tiktok-created-ad value-type:json editable:false description:"Ad ID"
// GetDexSummary shows the confirmed ad ID.
func (*CreateAdFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readCreateAd(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-created-ad": result}}, nil
}

// dex:field attribute-key:tiktok-created-ad value-type:json editable:false description:"Ad ID"
// GetDexDisplay shows the confirmed ad ID.
func (*CreateAdFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	result, err := readCreateAd(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-created-ad": result}}, nil
}

func readCreateAd(ctx dex.Context) (tiktok.AdWriteResult, error) {
	result, err := createAdAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tiktok.AdWriteResult{}, nil
	}
	return result, err
}

func mapCreateAdInput(input tiktok.CreateAdInput) tiktok.CreateAdInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the advertiser and ad settings to TikTok."
type startCreateAd struct{ dex.StepDefaults }

func (startCreateAd) GetStepType() string { return "StartCreateAd" }
func (startCreateAd) WaitFor(dex.Context, tiktok.CreateAdInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startCreateAd) Execute(_ dex.Context, input tiktok.CreateAdInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tiktok.CreateAdInput](createAdStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete after TikTok confirms the ad ID."
type completeCreateAd struct {
	dex.StepDefaultsNoWaitFor[tiktok.CreateAdResult]
}

func (completeCreateAd) GetStepType() string { return "CompleteCreateAd" }
func (completeCreateAd) Execute(ctx dex.Context, result tiktok.CreateAdResult) (*dex.StepDecision, error) {
	if err := createAdAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*CreateAdFlow)(nil)
