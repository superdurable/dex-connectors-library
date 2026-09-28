// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package upsertcontact demonstrates the Google Sheets upsertRow Mutation in a
// Flow started from Dex Web Start Flow.
package upsertcontact

import (
	"errors"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "GoogleSheetsUpsertContact"
	// ConnectionName is the static Dex Web connection for Google Sheets.
	ConnectionName = "google-sheets-contacts"

	recordContactStepType   = "RecordContactUpsertRequest"
	upsertContactStepType   = "UpsertContactRow"
	completeContactStepType = "CompleteContactUpsert"
)

var (
	contactRequestAttribute = dex.DefineAttribute[Input]("google-sheets-contact-request")
	contactResultAttribute  = dex.DefineAttribute[spreadsheet.UpsertRowOutput]("google-sheets-contact-result")
)

// Input is the typed contact row entered in Dex Web Start Flow.
type Input struct {
	// SpreadsheetID is the Google spreadsheet identifier.
	SpreadsheetID string `json:"spreadsheetId"`
	// SheetName is the worksheet title containing contact rows.
	SheetName string `json:"sheetName"`
	// Email is the stable row key.
	Email string `json:"email"`
	// Name is the contact display name.
	Name string `json:"name"`
	// Status is the contact lifecycle status.
	Status string `json:"status"`
}

// Flow upserts one contact row by email address.
type Flow struct {
	dex.FlowDefaults
	connection spreadsheet.Connection
}

// NewFlow binds the Google Sheets Connection at registration time.
func NewFlow(connection spreadsheet.Connection) *Flow {
	return &Flow{connection: connection}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the request, connector, and completion Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordContact{}),
		dex.DefineStep(spreadsheet.NewUpsertRowStep(spreadsheet.UpsertRowStepConfig[Input]{
			StepType: upsertContactStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "google-sheets", GroupLabel: "Google Sheets",
				Explanation: "Upsert the submitted contact in Google Sheets using email as the stable key.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToUpsertRowInput,
			Upserted: sdkgo.GoTo(completeContact{}),
		})),
		dex.DefineStep(completeContact{}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request and result Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{contactRequestAttribute, contactResultAttribute}}
}

// GetDexSummary returns the contact request and upsert result.
//
// dex:field attribute-key:google-sheets-contact-request value-type:json editable:false description:"Submitted contact row"
// dex:field attribute-key:google-sheets-contact-result value-type:json editable:false description:"Google Sheets upsert result"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, result, err := contactInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"google-sheets-contact-request": request,
		"google-sheets-contact-result":  result,
	}}, nil
}

// GetDexDisplay returns the contact request and upsert result.
//
// dex:field attribute-key:google-sheets-contact-request value-type:json editable:false description:"Spreadsheet, tab, and contact values"
// dex:field attribute-key:google-sheets-contact-result value-type:json editable:false description:"Upsert action, row number, and updated range"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, result, err := contactInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"google-sheets-contact-request": request,
		"google-sheets-contact-result":  result,
	}}, nil
}

// MapToUpsertRowInput maps the Flow input to the provider Mutation input.
func (*Flow) MapToUpsertRowInput(input Input) spreadsheet.UpsertRowInput {
	return spreadsheet.UpsertRowInput{
		SpreadsheetID: input.SpreadsheetID,
		SheetName:     input.SheetName,
		KeyColumn:     "email",
		KeyValue:      input.Email,
		Values: map[string]string{
			"email":  input.Email,
			"name":   input.Name,
			"status": input.Status,
		},
	}
}

func contactInspection(ctx dex.Context) (Input, spreadsheet.UpsertRowOutput, error) {
	request, err := optionalAttribute(ctx, contactRequestAttribute)
	if err != nil {
		return Input{}, spreadsheet.UpsertRowOutput{}, err
	}
	result, err := optionalAttribute(ctx, contactResultAttribute)
	if err != nil {
		return Input{}, spreadsheet.UpsertRowOutput{}, err
	}
	return request, result, nil
}

func optionalAttribute[T any](ctx dex.Context, attribute dex.Attribute[T]) (T, error) {
	value, err := attribute.Get(ctx)
	var missingAttribute *dex.AttributeNotFoundError
	if errors.As(err, &missingAttribute) {
		var zero T
		return zero, nil
	}
	return value, err
}

// dex:group group-id:google-sheets group-label:"Google Sheets"
// dex:explanation text:"Validate and record the contact before calling Google Sheets."
type recordContact struct {
	dex.StepDefaults
}

func (recordContact) GetStepType() string { return recordContactStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordContact) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (recordContact) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	input.SpreadsheetID = strings.TrimSpace(input.SpreadsheetID)
	input.SheetName = strings.TrimSpace(input.SheetName)
	input.Email = strings.TrimSpace(input.Email)
	input.Name = strings.TrimSpace(input.Name)
	input.Status = strings.TrimSpace(input.Status)
	if input.SpreadsheetID == "" || input.SheetName == "" || input.Email == "" || input.Name == "" {
		return dex.ForceFail("spreadsheetId, sheetName, email, and name are required"), nil
	}
	if err := contactRequestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](upsertContactStepType), input), nil
}

// dex:group group-id:google-sheets group-label:"Google Sheets"
// dex:explanation text:"Persist the upsert result and complete the Flow."
type completeContact struct {
	dex.StepDefaultsNoWaitFor[spreadsheet.UpsertRowResult]
}

func (completeContact) GetStepType() string { return completeContactStepType }

func (completeContact) Execute(ctx dex.Context, result spreadsheet.UpsertRowResult) (*dex.StepDecision, error) {
	if err := contactResultAttribute.Set(ctx, result.Value); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
