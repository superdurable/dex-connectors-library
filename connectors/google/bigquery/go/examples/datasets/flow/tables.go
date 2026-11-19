// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package datasets

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/google/bigquery/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// TablesFlowType is the stable identity of the table listing example.
const TablesFlowType = "GoogleBigQueryTablesList"

const tablesStepType = "CallListTables"

var tablesAttribute = dex.DefineAttribute[bigquery.TablesPage]("google-bigquery-tables-page")

// TablesFlow lists one bounded page of BigQuery tables.
type TablesFlow struct {
	dex.FlowDefaults
	connection bigquery.Connection
}

// NewTablesFlow binds the provider connection at registration time.
func NewTablesFlow(connection bigquery.Connection) *TablesFlow {
	return &TablesFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*TablesFlow) GetFlowType() string { return TablesFlowType }

// GetPersistenceSchema registers the table page Attribute.
func (*TablesFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{tablesAttribute}}
}

// GetSteps registers start, provider, and completion Steps.
func (flow *TablesFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startTables{}),
		dex.DefineStep(bigquery.NewListTablesStep(bigquery.ListTablesStepConfig[bigquery.ListTablesInput]{
			StepType: tablesStepType, ConnectionName: ConnectionName, Connection: flow.connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google BigQuery", Explanation: "List one page of tables in the selected BigQuery dataset."},
			MapToOperationInput: func(input bigquery.ListTablesInput) bigquery.ListTablesInput { return input },
			Listed:              sdkgo.GoTo(completeTables{}),
		})),
		dex.DefineStep(completeTables{}),
	}
}

// GetRPCs registers read-only Dex Web table views.
func (flow *TablesFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-bigquery-tables-page value-type:json editable:false description:"Table page"
// GetDexSummary returns the last table page when available.
func (*TablesFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readTables(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-bigquery-tables-page": page}}, nil
}

// dex:field attribute-key:google-bigquery-tables-page value-type:json editable:false description:"Table page"
// GetDexDisplay returns the last table page when available.
func (*TablesFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	page, err := readTables(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-bigquery-tables-page": page}}, nil
}

func readTables(ctx dex.Context) (bigquery.TablesPage, error) {
	page, err := tablesAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return bigquery.TablesPage{}, nil
	}
	return page, err
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the project, dataset, and page to the table Step."
type startTables struct{ dex.StepDefaults }

func (startTables) GetStepType() string { return "StartListTables" }
func (startTables) WaitFor(dex.Context, bigquery.ListTablesInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startTables) Execute(_ dex.Context, input bigquery.ListTablesInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[bigquery.ListTablesInput](tablesStepType), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete with the selected table page."
type completeTables struct {
	dex.StepDefaultsNoWaitFor[bigquery.ListTablesResult]
}

func (completeTables) GetStepType() string { return "CompleteListTables" }
func (completeTables) Execute(ctx dex.Context, result bigquery.ListTablesResult) (*dex.StepDecision, error) {
	if err := tablesAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*TablesFlow)(nil)
