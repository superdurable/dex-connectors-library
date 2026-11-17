// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package adinspection

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/meta/ads-insights/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const insightsStepType = "CallGetInsights"

var insightsAttribute = dex.DefineAttribute[metaadsinsights.InsightsPage]("meta-ads-insights-report")

// InsightsReportFlow reads one page of Meta ad performance metrics.
type InsightsReportFlow struct {
	dex.FlowDefaults
	connection metaadsinsights.Connection
}

// NewInsightsReportFlow binds the static Meta Marketing connection.
func NewInsightsReportFlow(connection metaadsinsights.Connection) *InsightsReportFlow {
	return &InsightsReportFlow{connection: connection}
}

// GetFlowType identifies the example insights Flow.
func (*InsightsReportFlow) GetFlowType() string { return "MetaAdsInsightsReport" }

// GetPersistenceSchema registers the durable report result.
func (*InsightsReportFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{insightsAttribute}}
}

// GetSteps wires the operation-specific Insights Query.
func (flow *InsightsReportFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startInsightsReport{}),
		dex.DefineStep(metaadsinsights.NewGetInsightsStep(metaadsinsights.GetInsightsStepConfig[metaadsinsights.GetInsightsInput]{
			StepType: insightsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Meta Ads Insights", Explanation: "Read ad performance metrics."},
			Connection: flow.connection, MapToOperationInput: insightsInput,
			Listed: sdkgo.GoTo(completeInsightsReport{}),
		})),
		dex.DefineStep(completeInsightsReport{}),
	}
}

// GetRPCs exposes the last report in Dex Web.
func (flow *InsightsReportFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:meta-ads-insights-report value-type:json editable:false description:"Ad performance report"
// GetDexSummary returns the last report page.
func (*InsightsReportFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	report, err := readInsightsReport(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meta-ads-insights-report": report}}, nil
}

// dex:field attribute-key:meta-ads-insights-report value-type:json editable:false description:"Ad performance report"
// GetDexDisplay returns the last report page.
func (*InsightsReportFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	report, err := readInsightsReport(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"meta-ads-insights-report": report}}, nil
}

func readInsightsReport(ctx dex.Context) (metaadsinsights.InsightsPage, error) {
	report, err := insightsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return metaadsinsights.InsightsPage{}, nil
	}
	return report, err
}

func insightsInput(input metaadsinsights.GetInsightsInput) metaadsinsights.GetInsightsInput { return input }

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the requested ad object and date preset to the provider Step."
type startInsightsReport struct{ dex.StepDefaults }

func (startInsightsReport) GetStepType() string { return "StartInsightsReport" }
func (startInsightsReport) WaitFor(dex.Context, metaadsinsights.GetInsightsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startInsightsReport) Execute(_ dex.Context, input metaadsinsights.GetInsightsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[metaadsinsights.GetInsightsInput](insightsStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Save the metrics page and complete the report Flow."
type completeInsightsReport struct{ dex.StepDefaultsNoWaitFor[metaadsinsights.GetInsightsResult] }

func (completeInsightsReport) GetStepType() string { return "CompleteInsightsReport" }
func (completeInsightsReport) Execute(ctx dex.Context, result metaadsinsights.GetInsightsResult) (*dex.StepDecision, error) {
	if err := insightsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*InsightsReportFlow)(nil)
