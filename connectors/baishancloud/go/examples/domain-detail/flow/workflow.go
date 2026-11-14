// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package domaindetail runs BaishanCloud describeDomain as one Dex Flow.
package domaindetail

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/baishancloud/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "BaishanCloudDomainDetail"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "baishancloud"
	operationStepType = "CallDescribeDomain"
)

var domainDetailOutcomeAttribute = dex.DefineAttribute[baishancloud.DomainDetail]("baishancloud-domain-detail-outcome")

// Flow runs DescribeDomain once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection baishancloud.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection baishancloud.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{domainDetailOutcomeAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(baishancloud.NewDescribeDomainStep(baishancloud.DescribeDomainStepConfig[baishancloud.DescribeDomainInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "BaishanCloud", Explanation: "Describe one BaishanCloud CDN domain without origin configuration."},
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

// dex:field attribute-key:baishancloud-domain-detail-outcome value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDomainDetailOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"baishancloud-domain-detail-outcome": outcome}}, nil
}

// dex:field attribute-key:baishancloud-domain-detail-outcome value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDomainDetailOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"baishancloud-domain-detail-outcome": outcome}}, nil
}

func readDomainDetailOutcome(ctx dex.Context) (baishancloud.DomainDetail, error) {
	outcome, err := domainDetailOutcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return baishancloud.DomainDetail{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input baishancloud.DescribeDomainInput) baishancloud.DescribeDomainInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartDescribeDomain" }
func (startRequest) WaitFor(dex.Context, baishancloud.DescribeDomainInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input baishancloud.DescribeDomainInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[baishancloud.DescribeDomainInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[baishancloud.DescribeDomainResult]
}

func (completeResult) GetStepType() string { return "CompleteDescribeDomain" }
func (completeResult) Execute(ctx dex.Context, result baishancloud.DescribeDomainResult) (*dex.StepDecision, error) {
	if err := domainDetailOutcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
