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
	// AdgroupsFlowType is the stable example Flow identity.
	AdgroupsFlowType = "TikTokAdgroupsDiscovery"
	adgroupsStepType = "CallListAdgroups"
)

var adgroupsAttribute = dex.DefineAttribute[tiktok.AdgroupsPage]("tiktok-adgroups")

// AdgroupsFlow runs ListAdgroups once and completes with its typed output.
type AdgroupsFlow struct {
	dex.FlowDefaults
	connection tiktok.Connection
}

// NewAdgroupsFlow binds the provider connection at registration time.
func NewAdgroupsFlow(connection tiktok.Connection) *AdgroupsFlow {
	return &AdgroupsFlow{connection: connection}
}

func (*AdgroupsFlow) GetFlowType() string { return AdgroupsFlowType }

func (*AdgroupsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{adgroupsAttribute}}
}

func (flow *AdgroupsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAdgroupsRequest{}),
		dex.DefineStep(tiktok.NewListAdgroupsStep(tiktok.ListAdgroupsStepConfig[tiktok.ListAdgroupsInput]{
			StepType: adgroupsStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "TikTok", Explanation: "List one page of TikTok adgroups."},
			Connection:          flow.connection,
			MapToOperationInput: mapAdgroupsInput,
			Listed:              sdkgo.GoTo(completeAdgroupsResult{}),
		})),
		dex.DefineStep(completeAdgroupsResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *AdgroupsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:tiktok-adgroups value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*AdgroupsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAdgroups(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-adgroups": outcome}}, nil
}

// dex:field attribute-key:tiktok-adgroups value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*AdgroupsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readAdgroups(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"tiktok-adgroups": outcome}}, nil
}

func readAdgroups(ctx dex.Context) (tiktok.AdgroupsPage, error) {
	outcome, err := adgroupsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return tiktok.AdgroupsPage{}, nil
	}
	return outcome, err
}

// mapAdgroupsInput preserves the validated typed start input for the provider call.
func mapAdgroupsInput(input tiktok.ListAdgroupsInput) tiktok.ListAdgroupsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startAdgroupsRequest struct{ dex.StepDefaults }

func (startAdgroupsRequest) GetStepType() string { return "StartListAdgroups" }
func (startAdgroupsRequest) WaitFor(dex.Context, tiktok.ListAdgroupsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAdgroupsRequest) Execute(_ dex.Context, input tiktok.ListAdgroupsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[tiktok.ListAdgroupsInput](adgroupsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeAdgroupsResult struct {
	dex.StepDefaultsNoWaitFor[tiktok.ListAdgroupsResult]
}

func (completeAdgroupsResult) GetStepType() string { return "CompleteListAdgroups" }
func (completeAdgroupsResult) Execute(ctx dex.Context, result tiktok.ListAdgroupsResult) (*dex.StepDecision, error) {
	if err := adgroupsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AdgroupsFlow)(nil)
