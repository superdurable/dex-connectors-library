// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package upsertcontact

import (
	"errors"

	"github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const spreadsheetDetailFlowType = "GoogleSheetsSpreadsheetDetail"

var spreadsheetDetailAttribute = dex.DefineAttribute[spreadsheet.SpreadsheetInfo]("google-sheets-spreadsheet")

// SpreadsheetDetailFlow reads one spreadsheet and retains the provider result.
type SpreadsheetDetailFlow struct {
	dex.FlowDefaults
	connection spreadsheet.Connection
}

// NewSpreadsheetDetailFlow binds the provider connection at registration.
func NewSpreadsheetDetailFlow(connection spreadsheet.Connection) *SpreadsheetDetailFlow {
	return &SpreadsheetDetailFlow{connection: connection}
}

func (*SpreadsheetDetailFlow) GetFlowType() string { return spreadsheetDetailFlowType }
func (*SpreadsheetDetailFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{spreadsheetDetailAttribute}}
}
func (flow *SpreadsheetDetailFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startSpreadsheetDetail{}),
		dex.DefineStep(spreadsheet.NewGetSpreadsheetStep(spreadsheet.GetSpreadsheetStepConfig[spreadsheet.GetSpreadsheetInput]{
			StepType: "CallGetSpreadsheet", ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "provider", GroupLabel: "Google Sheets", Explanation: "Read the selected spreadsheet title and tabs."},
			Connection:  flow.connection, MapToOperationInput: mapSpreadsheetDetailInput,
			Found: sdkgo.GoTo(completeSpreadsheetDetail{}),
		})),
		dex.DefineStep(completeSpreadsheetDetail{}),
	}
}
func (flow *SpreadsheetDetailFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// dex:field attribute-key:google-sheets-spreadsheet value-type:json editable:false description:"Selected spreadsheet"
// GetDexSummary shows the selected spreadsheet in Dex Web.
func (*SpreadsheetDetailFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	info, err := readSpreadsheetDetail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-sheets-spreadsheet": info}}, nil
}

// dex:field attribute-key:google-sheets-spreadsheet value-type:json editable:false description:"Selected spreadsheet"
// GetDexDisplay shows the selected spreadsheet in Dex Web.
func (*SpreadsheetDetailFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	info, err := readSpreadsheetDetail(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"google-sheets-spreadsheet": info}}, nil
}

func readSpreadsheetDetail(ctx dex.Context) (spreadsheet.SpreadsheetInfo, error) {
	info, err := spreadsheetDetailAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return spreadsheet.SpreadsheetInfo{}, nil
	}
	return info, err
}
func mapSpreadsheetDetailInput(input spreadsheet.GetSpreadsheetInput) spreadsheet.GetSpreadsheetInput {
	return input
}

// dex:group group-id:request group-label:"Request"
// dex:explanation text:"Pass the spreadsheet ID to the provider Step."
type startSpreadsheetDetail struct{ dex.StepDefaults }

func (startSpreadsheetDetail) GetStepType() string { return "StartGetSpreadsheet" }
func (startSpreadsheetDetail) WaitFor(dex.Context, spreadsheet.GetSpreadsheetInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startSpreadsheetDetail) Execute(_ dex.Context, input spreadsheet.GetSpreadsheetInput) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[spreadsheet.GetSpreadsheetInput]("CallGetSpreadsheet"), input), nil
}

// dex:group group-id:result group-label:"Result"
// dex:explanation text:"Complete the Flow with the selected spreadsheet."
type completeSpreadsheetDetail struct {
	dex.StepDefaultsNoWaitFor[spreadsheet.GetSpreadsheetResult]
}

func (completeSpreadsheetDetail) GetStepType() string { return "CompleteGetSpreadsheet" }
func (completeSpreadsheetDetail) Execute(ctx dex.Context, result spreadsheet.GetSpreadsheetResult) (*dex.StepDecision, error) {
	if err := spreadsheetDetailAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*SpreadsheetDetailFlow)(nil)
