// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package datasets runs Google BigQuery listDatasets as one Dex Flow.
package datasets

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/google/bigquery/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable example Flow identity.
	FlowType = "GoogleBigQueryDatasetsList"
	// ConnectionName is the static Dex Web connection used by this example.
	ConnectionName    = "google-bigquery"
	operationStepType = "CallListDatasets"
)

var datasetsAttribute = dex.DefineAttribute[bigquery.DatasetsPage]("google-bigquery-page")

// Flow runs ListDatasets once and completes with its typed output.
type Flow struct {
	dex.FlowDefaults
	connection bigquery.Connection
}

// NewFlow binds the provider connection at registration time.
func NewFlow(connection bigquery.Connection) *Flow { return &Flow{connection: connection} }

func (*Flow) GetFlowType() string { return FlowType }

func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{datasetsAttribute}}
}

func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startRequest{}),
		dex.DefineStep(bigquery.NewListDatasetsStep(bigquery.ListDatasetsStepConfig[bigquery.ListDatasetsInput]{
			StepType: operationStepType, ConnectionName: ConnectionName,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google BigQuery", Explanation: "List datasets visible in the selected Google Cloud project."},
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

// dex:field attribute-key:google-bigquery-page value-type:json editable:false description:"Provider result"
// GetDexSummary shows the last provider result in Dex Web.
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDatasets(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-bigquery-page": outcome}}, nil
}

// dex:field attribute-key:google-bigquery-page value-type:json editable:false description:"Provider result"
// GetDexDisplay shows the last provider result in Dex Web.
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	outcome, err := readDatasets(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-bigquery-page": outcome}}, nil
}

func readDatasets(ctx dex.Context) (bigquery.DatasetsPage, error) {
	outcome, err := datasetsAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return bigquery.DatasetsPage{}, nil
	}
	return outcome, err
}

// MapToOperationInput preserves the validated typed start input for the provider call.
func MapToOperationInput(input bigquery.ListDatasetsInput) bigquery.ListDatasetsInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the typed start input to the provider Step."
type startRequest struct{ dex.StepDefaults }

func (startRequest) GetStepType() string { return "StartListDatasets" }
func (startRequest) WaitFor(dex.Context, bigquery.ListDatasetsInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startRequest) Execute(_ dex.Context, input bigquery.ListDatasetsInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[bigquery.ListDatasetsInput](operationStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the provider's typed result."
type completeResult struct {
	dex.StepDefaultsNoWaitFor[bigquery.ListDatasetsResult]
}

func (completeResult) GetStepType() string { return "CompleteListDatasets" }
func (completeResult) Execute(ctx dex.Context, result bigquery.ListDatasetsResult) (*dex.StepDecision, error) {
	if err := datasetsAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
