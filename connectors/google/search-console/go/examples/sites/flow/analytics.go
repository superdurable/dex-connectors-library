// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sites

import (
	"errors"

	searchconsole "github.com/superdurable/dex-connectors-library/connectors/google/search-console/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const analyticsFlowType = "GoogleSearchConsoleAnalytics"

var analyticsAttribute = dex.DefineAttribute[searchconsole.SearchAnalyticsResult]("google-search-console-analytics")

// AnalyticsFlow queries search performance and retains the provider result.
type AnalyticsFlow struct {
	dex.FlowDefaults
	connection searchconsole.Connection
}

// NewAnalyticsFlow binds the provider connection at registration.
func NewAnalyticsFlow(connection searchconsole.Connection) *AnalyticsFlow {
	return &AnalyticsFlow{connection: connection}
}

func (*AnalyticsFlow) GetFlowType() string { return analyticsFlowType }
func (*AnalyticsFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{analyticsAttribute}}
}
func (flow *AnalyticsFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startAnalytics{}),
		dex.DefineStep(searchconsole.NewQuerySearchAnalyticsStep(searchconsole.QuerySearchAnalyticsStepConfig[searchconsole.QuerySearchAnalyticsInput]{
			StepType: "CallQuerySearchAnalytics", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Search Console", Explanation: "Query search traffic for the selected property and date range."},
			Connection:  flow.connection, MapToOperationInput: mapAnalyticsInput,
			Queried: sdkgo.GoTo(completeAnalytics{}),
		})),
		dex.DefineStep(completeAnalytics{}),
	}
}
func (flow *AnalyticsFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-search-console-analytics value-type:json editable:false description:"Search traffic rows"
// GetDexSummary shows search traffic rows in Dex Web.
func (*AnalyticsFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	analytics, err := readAnalytics(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-search-console-analytics": analytics}}, nil
}

// dex:field attribute-key:google-search-console-analytics value-type:json editable:false description:"Search traffic rows"
// GetDexDisplay shows search traffic rows in Dex Web.
func (*AnalyticsFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	analytics, err := readAnalytics(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-search-console-analytics": analytics}}, nil
}

func readAnalytics(ctx dex.Context) (searchconsole.SearchAnalyticsResult, error) {
	analytics, err := analyticsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return searchconsole.SearchAnalyticsResult{}, nil
	}
	return analytics, err
}
func mapAnalyticsInput(input searchconsole.QuerySearchAnalyticsInput) searchconsole.QuerySearchAnalyticsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the property and date range to the provider Step."
type startAnalytics struct{ dex.StepDefaults }

func (startAnalytics) GetStepType() string { return "StartQuerySearchAnalytics" }
func (startAnalytics) WaitFor(dex.Context, searchconsole.QuerySearchAnalyticsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startAnalytics) Execute(_ dex.Context, input searchconsole.QuerySearchAnalyticsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[searchconsole.QuerySearchAnalyticsInput]("CallQuerySearchAnalytics"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with search traffic rows."
type completeAnalytics struct {
	dex.StepDefaultsNoWaitFor[searchconsole.QuerySearchAnalyticsResult]
}

func (completeAnalytics) GetStepType() string { return "CompleteQuerySearchAnalytics" }
func (completeAnalytics) Execute(ctx dex.Context, result searchconsole.QuerySearchAnalyticsResult) (*dex.StepDecision, error) {
	if err := analyticsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*AnalyticsFlow)(nil)
