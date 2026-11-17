// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package emailmetrics demonstrates Resend email metrics retrieval.
package emailmetrics

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/resend/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is this example's stable Flow identity.
	FlowType = "ResendGetEmailMetrics"
	// ConnectionName is the static Dex Web connection name.
	ConnectionName = "resend"
)

// Request supplies the metric filters.
type Request resend.GetEmailMetricsInput

var outcomeAttribute = dex.DefineAttribute[resend.EmailMetrics]("resend-email-metrics")

// Flow retrieves aggregate Resend email metrics.
type Flow struct {
	dex.FlowDefaults
	connection resend.Connection
}

// NewFlow binds a Resend connection.
func NewFlow(connection resend.Connection) *Flow { return &Flow{connection: connection} }

// GetFlowType returns the stable Flow identity.
func (*Flow) GetFlowType() string { return FlowType }

// GetRPCs exposes Dex Web summary and display views.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetPersistenceSchema registers the metrics result.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{outcomeAttribute}}
}

// GetDexSummary returns the email metrics.
// dex:field attribute-key:resend-email-metrics value-type:json editable:false description:"Email metrics"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readEmailMetricsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-email-metrics": value}}, nil
}

// GetDexDisplay returns the email metrics.
// dex:field attribute-key:resend-email-metrics value-type:json editable:false description:"Email metrics"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	value, err := readEmailMetricsOutcome(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"resend-email-metrics": value}}, nil
}

func readEmailMetricsOutcome(ctx dex.Context) (resend.EmailMetrics, error) {
	value, err := outcomeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return resend.EmailMetrics{}, nil
	}
	return value, err
}

// GetSteps registers the email metrics query path.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(prepareEmailMetrics{}),
		dex.DefineStep(resend.NewGetEmailMetricsStep(resend.GetEmailMetricsStepConfig[Request]{
			StepType: "GetEmailMetrics", ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "resend-email-metrics", GroupLabel: "Resend email metrics", Explanation: "Retrieve delivery metrics for the selected filters."},
			MapToOperationInput: func(request Request) resend.GetEmailMetricsInput { return resend.GetEmailMetricsInput(request) },
			Found:               sdkgo.GoTo(completeEmailMetrics{}),
		})),
		dex.DefineStep(completeEmailMetrics{}),
	}
}

// dex:group group-id:resend-email-metrics-prepare group-label:"Prepare email metrics"
// dex:explanation text:"Pass the selected metric filters into the provider query."
type prepareEmailMetrics struct{ dex.StepDefaults }

func (prepareEmailMetrics) GetStepType() string { return "PrepareEmailMetrics" }
func (prepareEmailMetrics) WaitFor(dex.Context, Request) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (prepareEmailMetrics) Execute(_ dex.Context, request Request) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[Request]("GetEmailMetrics"), request), nil
}

// dex:group group-id:resend-email-metrics-complete group-label:"Complete email metrics query"
// dex:explanation text:"Complete after Resend returns the aggregate metrics."
type completeEmailMetrics struct {
	dex.StepDefaultsNoWaitFor[resend.GetEmailMetricsResult]
}

func (completeEmailMetrics) GetStepType() string { return "CompleteEmailMetrics" }
func (completeEmailMetrics) Execute(ctx dex.Context, result resend.GetEmailMetricsResult) (*dex.StepDecision, error) {
	if err := outcomeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
