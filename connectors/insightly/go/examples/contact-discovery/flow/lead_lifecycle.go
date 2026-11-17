// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contactdiscovery

import (
	"encoding/json"
	"errors"
	"net/mail"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/insightly/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// LeadLifecycleFlowType identifies the six-operation example Flow.
	LeadLifecycleFlowType       = "InsightlyLeadLifecycle"
	startLeadLifecycleStepType  = "StartInsightlyLeadLifecycle"
	createLeadStepType          = "CreateInsightlyLead"
	readCreatedLeadStepType     = "ReadCreatedInsightlyLead"
	getLeadStepType             = "GetInsightlyLead"
	prepareLeadUpdateStepType   = "PrepareInsightlyLeadUpdate"
	updateLeadStepType          = "UpdateInsightlyLead"
	prepareLeadDeleteStepType   = "PrepareInsightlyLeadDelete"
	deleteLeadStepType          = "DeleteInsightlyLead"
	prepareLeadListStepType     = "PrepareInsightlyLeadList"
	listLeadsStepType           = "ListInsightlyLeads"
	prepareLeadSearchStepType   = "PrepareInsightlyLeadSearch"
	searchLeadStepType          = "SearchInsightlyLead"
	finishLeadLifecycleStepType = "FinishInsightlyLeadLifecycle"
)

var leadLifecycleInputAttribute = dex.DefineAttribute[LeadLifecycleInput]("insightly-lead-lifecycle-input")

// LeadLifecycleInput names a temporary lead to create, update, and delete.
type LeadLifecycleInput struct {
	// FirstName is the created lead's given name.
	FirstName string `json:"firstName"`
	// LastName is the created lead's family name.
	LastName string `json:"lastName"`
	// Email is the exact address used for the final search.
	Email string `json:"email"`
	// UpdatedLastName is the family name set by the partial update.
	UpdatedLastName string `json:"updatedLastName"`
}

// LeadLifecycleFlow demonstrates all six generic CRM operations on one lead.
type LeadLifecycleFlow struct {
	dex.FlowDefaults
	connection insightly.Connection
}

// NewLeadLifecycleFlow binds the Insightly connection at registration time.
func NewLeadLifecycleFlow(connection insightly.Connection) *LeadLifecycleFlow {
	return &LeadLifecycleFlow{connection: connection}
}

// GetFlowType returns the stable Flow identity.
func (*LeadLifecycleFlow) GetFlowType() string { return LeadLifecycleFlowType }

// GetPersistenceSchema registers the input used after connector boundaries.
func (*LeadLifecycleFlow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{leadLifecycleInputAttribute}}
}

// GetSteps returns the application and connector Steps in lifecycle order.
func (flow *LeadLifecycleFlow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(startLeadLifecycle{}),
		dex.DefineStep(insightly.NewCreateRecordStep(insightly.CreateRecordStepConfig[insightly.CreateRecordInput]{
			StepType: createLeadStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "insightly", GroupLabel: "Insightly", Explanation: "Create the temporary lead from its name and email."},
			Connection:  flow.connection, MapToOperationInput: identityCreateRecordInput,
			Created: sdkgo.GoTo(readCreatedLead{}),
		})),
		dex.DefineStep(readCreatedLead{}),
		dex.DefineStep(insightly.NewGetRecordStep(insightly.GetRecordStepConfig[insightly.GetRecordInput]{
			StepType: getLeadStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "insightly", GroupLabel: "Insightly", Explanation: "Read the newly created lead by its returned ID."},
			Connection:  flow.connection, MapToOperationInput: identityGetRecordInput,
			Found: sdkgo.GoTo(prepareLeadUpdate{}),
		})),
		dex.DefineStep(prepareLeadUpdate{}),
		dex.DefineStep(insightly.NewUpdateRecordStep(insightly.UpdateRecordStepConfig[insightly.UpdateRecordInput]{
			StepType: updateLeadStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "insightly", GroupLabel: "Insightly", Explanation: "Set the requested family name on the lead."},
			Connection:  flow.connection, MapToOperationInput: identityUpdateRecordInput,
			Updated: sdkgo.GoTo(prepareLeadDelete{}),
		})),
		dex.DefineStep(prepareLeadDelete{}),
		dex.DefineStep(insightly.NewDeleteRecordStep(insightly.DeleteRecordStepConfig[insightly.DeleteRecordInput]{
			StepType: deleteLeadStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "insightly", GroupLabel: "Insightly", Explanation: "Delete the temporary lead by its ID."},
			Connection:  flow.connection, MapToOperationInput: identityDeleteRecordInput,
			Deleted: sdkgo.GoTo(prepareLeadList{}),
		})),
		dex.DefineStep(prepareLeadList{}),
		dex.DefineStep(insightly.NewListRecordsStep(insightly.ListRecordsStepConfig[insightly.ListRecordsInput]{
			StepType: listLeadsStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "insightly", GroupLabel: "Insightly", Explanation: "List a bounded page of remaining leads."},
			Connection:  flow.connection, MapToOperationInput: identityListRecordsInput,
			Listed: sdkgo.GoTo(prepareLeadSearch{}),
		})),
		dex.DefineStep(prepareLeadSearch{}),
		dex.DefineStep(insightly.NewSearchRecordsStep(insightly.SearchRecordsStepConfig[insightly.SearchRecordsInput]{
			StepType: searchLeadStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{GroupID: "insightly", GroupLabel: "Insightly", Explanation: "Search leads by the submitted email address."},
			Connection:  flow.connection, MapToOperationInput: identitySearchRecordsInput,
			Searched: sdkgo.GoTo(finishLeadLifecycle{}),
		})),
		dex.DefineStep(finishLeadLifecycle{}),
	}
}

// GetRPCs exposes the initial input in Dex Web summary and display views.
func (flow *LeadLifecycleFlow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{dex.DefineRPC(flow.GetDexSummary, nil), dex.DefineRPC(flow.GetDexDisplay, nil)}
}

// GetDexSummary returns the submitted lifecycle input.
// dex:field attribute-key:insightly-lead-lifecycle-input value-type:json editable:false description:"Temporary lead lifecycle input"
func (*LeadLifecycleFlow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	input, err := readLeadLifecycleInput(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"insightly-lead-lifecycle-input": input}}, nil
}

// GetDexDisplay returns the submitted lifecycle input.
// dex:field attribute-key:insightly-lead-lifecycle-input value-type:json editable:false description:"Temporary lead lifecycle input"
func (*LeadLifecycleFlow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	input, err := readLeadLifecycleInput(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{"insightly-lead-lifecycle-input": input}}, nil
}

func readLeadLifecycleInput(ctx dex.Context) (LeadLifecycleInput, error) {
	input, err := leadLifecycleInputAttribute.Get(ctx)
	var missing *dex.AttributeNotFoundError
	if errors.As(err, &missing) {
		return LeadLifecycleInput{}, nil
	}
	return input, err
}

func identityCreateRecordInput(input insightly.CreateRecordInput) insightly.CreateRecordInput {
	return input
}
func identityGetRecordInput(input insightly.GetRecordInput) insightly.GetRecordInput { return input }
func identityUpdateRecordInput(input insightly.UpdateRecordInput) insightly.UpdateRecordInput {
	return input
}
func identityDeleteRecordInput(input insightly.DeleteRecordInput) insightly.DeleteRecordInput {
	return input
}
func identityListRecordsInput(input insightly.ListRecordsInput) insightly.ListRecordsInput {
	return input
}
func identitySearchRecordsInput(input insightly.SearchRecordsInput) insightly.SearchRecordsInput {
	return input
}

// dex:group group-id:insightly group-label:"Insightly"
// dex:explanation text:"Validate the temporary lead and start its creation."
type startLeadLifecycle struct{ dex.StepDefaults }

func (startLeadLifecycle) GetStepType() string { return startLeadLifecycleStepType }
func (startLeadLifecycle) WaitFor(dex.Context, LeadLifecycleInput) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}
func (startLeadLifecycle) Execute(ctx dex.Context, input LeadLifecycleInput) (*dex.StepDecision, error) {
	if strings.TrimSpace(input.FirstName) == "" || strings.TrimSpace(input.LastName) == "" || strings.TrimSpace(input.UpdatedLastName) == "" {
		return dex.ForceFail("lead names are required"), nil
	}
	address, err := mail.ParseAddress(input.Email)
	if err != nil || address.Address != input.Email {
		return dex.ForceFail("email must be one plain address"), nil
	}
	if err := leadLifecycleInputAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	fields := map[string]json.RawMessage{
		"FIRST_NAME": json.RawMessage(strconv.Quote(input.FirstName)),
		"LAST_NAME":  json.RawMessage(strconv.Quote(input.LastName)),
		"EMAIL":      json.RawMessage(strconv.Quote(input.Email)),
	}
	return dex.GoTo(sdkgo.StepRef[insightly.CreateRecordInput](createLeadStepType), insightly.CreateRecordInput{RecordType: insightly.RecordTypeLeads, Fields: fields}), nil
}

// dex:group group-id:insightly group-label:"Insightly"
// dex:explanation text:"Pass the created lead ID to the read Step."
type readCreatedLead struct {
	dex.StepDefaultsNoWaitFor[insightly.CreateRecordResult]
}

func (readCreatedLead) GetStepType() string { return readCreatedLeadStepType }
func (readCreatedLead) Execute(_ dex.Context, result insightly.CreateRecordResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[insightly.GetRecordInput](getLeadStepType), insightly.GetRecordInput{RecordType: insightly.RecordTypeLeads, ID: result.Value.ID}), nil
}

// dex:group group-id:insightly group-label:"Insightly"
// dex:explanation text:"Prepare the lead's requested family-name update."
type prepareLeadUpdate struct {
	dex.StepDefaultsNoWaitFor[insightly.GetRecordResult]
}

func (prepareLeadUpdate) GetStepType() string { return prepareLeadUpdateStepType }
func (prepareLeadUpdate) Execute(ctx dex.Context, result insightly.GetRecordResult) (*dex.StepDecision, error) {
	input, err := leadLifecycleInputAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	update := insightly.UpdateRecordInput{RecordType: insightly.RecordTypeLeads, ID: result.Value.ID, Fields: map[string]json.RawMessage{"LAST_NAME": json.RawMessage(strconv.Quote(input.UpdatedLastName))}}
	return dex.GoTo(sdkgo.StepRef[insightly.UpdateRecordInput](updateLeadStepType), update), nil
}

// dex:group group-id:insightly group-label:"Insightly"
// dex:explanation text:"Prepare deletion of the updated temporary lead."
type prepareLeadDelete struct {
	dex.StepDefaultsNoWaitFor[insightly.UpdateRecordResult]
}

func (prepareLeadDelete) GetStepType() string { return prepareLeadDeleteStepType }
func (prepareLeadDelete) Execute(_ dex.Context, result insightly.UpdateRecordResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[insightly.DeleteRecordInput](deleteLeadStepType), insightly.DeleteRecordInput{RecordType: insightly.RecordTypeLeads, ID: result.Value.ID}), nil
}

// dex:group group-id:insightly group-label:"Insightly"
// dex:explanation text:"List remaining leads after deleting the temporary record."
type prepareLeadList struct {
	dex.StepDefaultsNoWaitFor[insightly.DeleteRecordResult]
}

func (prepareLeadList) GetStepType() string { return prepareLeadListStepType }
func (prepareLeadList) Execute(_ dex.Context, _ insightly.DeleteRecordResult) (*dex.StepDecision, error) {
	return dex.GoTo(sdkgo.StepRef[insightly.ListRecordsInput](listLeadsStepType), insightly.ListRecordsInput{RecordType: insightly.RecordTypeLeads, Top: 10}), nil
}

// dex:group group-id:insightly group-label:"Insightly"
// dex:explanation text:"Search by the email entered when the temporary lead was created."
type prepareLeadSearch struct {
	dex.StepDefaultsNoWaitFor[insightly.ListRecordsResult]
}

func (prepareLeadSearch) GetStepType() string { return prepareLeadSearchStepType }
func (prepareLeadSearch) Execute(ctx dex.Context, _ insightly.ListRecordsResult) (*dex.StepDecision, error) {
	input, err := leadLifecycleInputAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[insightly.SearchRecordsInput](searchLeadStepType), insightly.SearchRecordsInput{RecordType: insightly.RecordTypeLeads, FieldName: "EMAIL", FieldValue: input.Email, Top: 10}), nil
}

// dex:group group-id:insightly group-label:"Insightly"
// dex:explanation text:"Complete with the final lead search page."
type finishLeadLifecycle struct {
	dex.StepDefaultsNoWaitFor[insightly.SearchRecordsResult]
}

func (finishLeadLifecycle) GetStepType() string { return finishLeadLifecycleStepType }
func (finishLeadLifecycle) Execute(_ dex.Context, result insightly.SearchRecordsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(result.Value), nil
}

var _ dex.Flow = (*LeadLifecycleFlow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*LeadLifecycleFlow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*LeadLifecycleFlow)(nil).GetDexDisplay
