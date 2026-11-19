// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package property

import (
	"errors"

	analytics4 "github.com/superdurable/dex-connectors-library/connectors/google/analytics/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// RealtimeFlowType is the stable identity of the GA4 realtime example.
const RealtimeFlowType = "GoogleAnalytics4Realtime"

const realtimeStepType = "CallRunRealtimeReport"

var realtimeAttribute = dex.DefineAttribute[analytics4.ReportPage]("google-analytics-4-realtime")

// RealtimeFlow runs a bounded GA4 realtime report.
type RealtimeFlow struct {
	dex.FlowDefaults
	connection analytics4.Connection
}

// NewRealtimeFlow binds the Analytics connection at registration time.
func NewRealtimeFlow(connection analytics4.Connection) *RealtimeFlow {
	return &RealtimeFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*RealtimeFlow) GetFlowType() string { return RealtimeFlowType }

// GetPersistenceSchema registers the report page Attribute.
func (*RealtimeFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{realtimeAttribute}}
}

// GetSteps registers start, provider, and completion Steps.
func (flow *RealtimeFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRealtime{}),
		dex.DefineStep(analytics4.NewRunRealtimeReportStep(analytics4.RunRealtimeReportStepConfig[analytics4.RunRealtimeReportInput]{
			StepType: realtimeStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Analytics 4", Explanation: "Read a bounded realtime report from the last 30 minutes."},
			MapToOperationInput: func(input analytics4.RunRealtimeReportInput) analytics4.RunRealtimeReportInput { return input },
			Reported:            sdkgo.GoTo(completeRealtime{}),
		})),
		dex.DefineStep(completeRealtime{}),
	}
}

// GetRPCs registers read-only Dex Web report views.
func (flow *RealtimeFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-analytics-4-realtime value-type:json editable:false description:"Realtime report page"
// GetDexSummary returns the last realtime report when available.
func (*RealtimeFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readRealtime(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-analytics-4-realtime": page}}, nil
}

// dex:field attribute-key:google-analytics-4-realtime value-type:json editable:false description:"Realtime report page"
// GetDexDisplay returns the last realtime report when available.
func (*RealtimeFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readRealtime(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-analytics-4-realtime": page}}, nil
}

func readRealtime(ctx dex.Context) (analytics4.ReportPage, error) {
	page, err := realtimeAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return analytics4.ReportPage{}, nil
	}
	return page, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the property and metric selections to the realtime Step."
type startRealtime struct{ dex.StepDefaults }

func (startRealtime) GetStepType() string { return "StartRealtimeReport" }
func (startRealtime) WaitFor(dex.Context, analytics4.RunRealtimeReportInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRealtime) Execute(_ dex.Context, input analytics4.RunRealtimeReportInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[analytics4.RunRealtimeReportInput](realtimeStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the realtime report page."
type completeRealtime struct {
	dex.StepDefaultsNoWaitFor[analytics4.RunRealtimeReportResult]
}

func (completeRealtime) GetStepType() string { return "CompleteRealtimeReport" }
func (completeRealtime) Execute(ctx dex.Context, result analytics4.RunRealtimeReportResult) (*dex.StepDecision, error) {
	if err := realtimeAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*RealtimeFlow)(nil)
