// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package insightlist runs PostHog listInsights as one Dex Flow.
package insightlist

import (
	"errors"
	"github.com/superdurable/dex-connectors-library/connectors/posthog/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "PostHogInsightList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "posthog"
	operationStepType = "CallListInsights"
)

var insightsPageAttribute = dex.DefineAttribute[posthog.InsightsPage]("posthog-insights-page")

// Flow runs ListInsights once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection posthog.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection posthog.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{insightsPageAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(posthog.NewListInsightsStep(posthog.ListInsightsStepConfig[posthog.ListInsightsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "PostHog", Explanation: "Call the provider's listInsights operation."},
			Connection:          flow.connection,
			MapToOperationInput: MapToOperationInput,
			Listed:              sdkgo.GoTo(completeResult{}),
		})),
		dex.DefineStep(completeResult{}),
	}
}

// GetRPCs exposes read-only Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:posthog-insights-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readInsightsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"posthog-insights-page": outcome}}, nil
}

// dex:field attribute-key:posthog-insights-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readInsightsPage(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"posthog-insights-page": outcome}}, nil
}

func readInsightsPage(ctx dex.Context) (posthog.InsightsPage, error) {
	outcome, err := insightsPageAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return posthog.InsightsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input posthog.ListInsightsInput) posthog.ListInsightsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListInsights" }
func (startRequest) WaitFor(dex.Context, posthog.ListInsightsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input posthog.ListInsightsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[posthog.ListInsightsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[posthog.ListInsightsResult]
}

func (completeResult) GetStepType() string { return "CompleteListInsights" }
func (completeResult) Execute(ctx dex.Context, result posthog.ListInsightsResult) (*dex.StepDecision, error) {
	if err := insightsPageAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
