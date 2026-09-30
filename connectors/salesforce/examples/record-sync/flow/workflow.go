// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package recordsync demonstrates every Salesforce operation in one Flow
// started from Dex Web Start Flow: find the records whose match field equals
// a value, link the one match to an external system's ID or create the record
// idempotently by that ID, and read the result back.
package recordsync

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/superdurable/dex-connectors-library/connectors/salesforce"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// FlowType is the stable Flow identity used by Dex Web Start Flow.
	FlowType = "SalesforceRecordSync"
	// ConnectionName is the static Dex Web connection for Salesforce.
	ConnectionName = "salesforce-crm"

	recordRequestStepType          = "RecordSyncRequest"
	findMatchesStepType            = "FindMatchingRecords"
	reviewMatchesStepType          = "ReviewMatchingRecords"
	linkRecordStepType             = "LinkExistingRecord"
	createRecordStepType           = "CreateRecordByExternalId"
	prepareLinkedReadBackStepType  = "PrepareLinkedReadBack"
	prepareCreatedReadBackStepType = "PrepareCreatedReadBack"
	reportLinkRejectedStepType     = "ReportLinkRejected"
	reportCreateRejectedStepType   = "ReportCreateRejected"
	readBackStepType               = "ReadBackRecord"
	completeStepType               = "CompleteRecordSync"

	defaultSObjectType = "Contact"
	defaultMatchField  = "Email"
	// maxMatchCandidates bounds the match query; two or more matches are already ambiguous.
	maxMatchCandidates = "5"
	matchQuery         = "SELECT Id, :externalIdField FROM :sObjectType WHERE :matchField = :matchValue ORDER BY CreatedDate ASC LIMIT " + maxMatchCandidates
)

var (
	requestAttribute = dex.DefineAttribute[Input]("salesforce-record-sync-request")
	outcomeAttribute = dex.DefineAttribute[Outcome]("salesforce-record-sync-outcome")
)

// Input is the typed request entered in Dex Web Start Flow.
type Input struct {
	// MatchValue is the value the configured match field must equal, such as an email address.
	MatchValue string `json:"matchValue"`
	// ExternalID is the other system's key for the record, such as ERP-88213.
	ExternalID string `json:"externalId"`
	// Fields maps field API names to text values to write, such as {"LastName": "Raman"}.
	// Each value is sent as a JSON string, which suits text, email, phone,
	// picklist, and date fields; write numbers and booleans as exact JSON
	// values through the operation input instead.
	Fields map[string]string `json:"fields"`
}

// SyncConfiguration is what the FindMatchingRecords Step's units save in Dex
// Web. Every Salesforce Step in the Flow uses the same object and fields.
type SyncConfiguration struct {
	// SObjectType is the object API name; blank means Contact.
	SObjectType string `json:"sObjectType,omitempty"`
	// MatchField is the field compared with Input.MatchValue; blank means Email.
	MatchField string `json:"matchField,omitempty"`
	// ExternalIDField is the object's External ID field; it is required.
	ExternalIDField string `json:"externalIdField,omitempty"`
}

// Status is the business outcome of one record sync.
type Status string

const (
	// StatusLinked means the one matching record was updated and now carries the external ID.
	StatusLinked Status = "linked"
	// StatusCreated means no record matched, so the record was created by its external ID.
	StatusCreated Status = "created"
	// StatusUpdatedByExternalID means the upsert found a record that already
	// carried the external ID, such as one created by an earlier attempt.
	StatusUpdatedByExternalID Status = "updatedByExternalId"
	// StatusAmbiguousMatch means more than one record matched, so nothing was written.
	StatusAmbiguousMatch Status = "ambiguousMatch"
	// StatusConflictingExternalID means the one match already carries a different external ID, so nothing was written.
	StatusConflictingExternalID Status = "conflictingExternalId"
	// StatusRejected means Salesforce rejected the field values; ProviderErrors says why.
	StatusRejected Status = "rejected"
)

// Outcome is the durable result of the Flow and its completion output.
type Outcome struct {
	// Status is the business outcome.
	Status Status `json:"status"`
	// SObjectType is the object the Flow synchronized.
	SObjectType string `json:"sObjectType,omitempty"`
	// RecordID is the linked or created record's ID.
	RecordID string `json:"recordId,omitempty"`
	// CandidateRecordIDs lists the records that made the match ambiguous or conflicting.
	CandidateRecordIDs []string `json:"candidateRecordIds,omitempty"`
	// ProviderErrors lists Salesforce's error codes and fields when it rejected the values.
	ProviderErrors []salesforce.ProviderError `json:"providerErrors,omitempty"`
	// Record is the record read back after the write.
	Record *salesforce.Record `json:"record,omitempty"`
}

// WriteRequest is the explicit domain value a write Step needs.
type WriteRequest struct {
	// RecordID is the matched record to link; blank for a create.
	RecordID string `json:"recordId,omitempty"`
	// ExternalID is the other system's key.
	ExternalID string `json:"externalId"`
	// Fields are the text values to write.
	Fields map[string]string `json:"fields"`
}

// ReadBackRequest names the record and fields to read after a write.
type ReadBackRequest struct {
	// RecordID is the written record.
	RecordID string `json:"recordId"`
	// Fields lists the written fields and the external ID field.
	Fields []string `json:"fields"`
}

// Flow matches one record by a field value and links or creates it by external ID.
type Flow struct {
	dex.FlowDefaults
	connection    salesforce.Connection
	configuration SyncConfiguration
}

// NewFlow binds the Salesforce Connection and the sync configuration loaded at
// startup, applying the Contact and Email defaults to blank values.
func NewFlow(connection salesforce.Connection, loaded sdkgo.ConnectorLoadedConfiguration[SyncConfiguration]) *Flow {
	configuration := loaded.Value
	if strings.TrimSpace(configuration.SObjectType) == "" {
		configuration.SObjectType = defaultSObjectType
	}
	if strings.TrimSpace(configuration.MatchField) == "" {
		configuration.MatchField = defaultMatchField
	}
	return &Flow{connection: connection, configuration: configuration}
}

// SyncConfigurationRef identifies the units saved on the FindMatchingRecords Step.
func SyncConfigurationRef() sdkgo.ConnectorConfigurationRef {
	return sdkgo.ConnectorConfigurationRef{
		ConnectorID: salesforce.ConnectorID, ConnectionName: ConnectionName, OperationID: "queryRecords",
		FlowType: FlowType, StepType: findMatchesStepType,
	}
}

// GetFlowType returns FlowType.
func (*Flow) GetFlowType() string { return FlowType }

// GetSteps returns the request, Salesforce, and outcome Steps.
func (flow *Flow) GetSteps() []dex.StepDef {
	return []dex.StepDef{
		dex.DefineStartStep(recordSyncRequest{configuration: flow.configuration}),
		dex.DefineStep(salesforce.NewQueryRecordsStep(salesforce.QueryRecordsStepConfig[Input]{
			StepType: findMatchesStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "salesforce", GroupLabel: "Salesforce",
				Explanation: "Find up to five records whose match field equals the value, with every value bound as a SOQL literal.",
			},
			ConfigurationUI: sdkgo.ConnectorConfigurationUI{Units: []sdkgo.ConnectorUIUnit{
				{
					ID: "recordObject", UnitID: salesforce.UIUnitSObjectPicker, Label: "Record object",
					Description: "Choose the object this Flow matches, links, and creates, such as Contact or Lead; every Salesforce Step in this Flow uses it, and blank means Contact.",
					Bindings:    []sdkgo.ConnectorUIBinding{{Port: salesforce.UISObjectPickerPortSObjectType, JSONPointer: "/sObjectType"}},
				},
				{
					ID: "matchField", UnitID: salesforce.UIUnitFieldNameInput, Label: "Match field",
					Description: "Enter the field of the chosen object whose value must equal the Start Flow matchValue, such as Email; blank means Email.",
					Bindings:    []sdkgo.ConnectorUIBinding{{Port: salesforce.UIFieldNameInputPortFieldName, JSONPointer: "/matchField"}},
				},
				{
					ID: "externalIdField", UnitID: salesforce.UIUnitFieldNameInput, Label: "External ID field", Required: true,
					Description: "Enter the chosen object's field marked External ID and Unique, such as ERP_Id__c; the Flow stamps it on a matched record and creates records by it, so it cannot be blank.",
					Bindings:    []sdkgo.ConnectorUIBinding{{Port: salesforce.UIFieldNameInputPortFieldName, JSONPointer: "/externalIdField"}},
				},
			}},
			Connection: flow.connection, MapToOperationInput: flow.MapToQueryRecordsInput,
			Found:    sdkgo.GoTo(reviewMatchingRecords{configuration: flow.configuration}),
			NotFound: sdkgo.GoTo(reviewMatchingRecords{configuration: flow.configuration}),
		})),
		dex.DefineStep(reviewMatchingRecords{configuration: flow.configuration}),
		dex.DefineStep(salesforce.NewUpdateRecordStep(salesforce.UpdateRecordStepConfig[WriteRequest]{
			StepType: linkRecordStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "salesforce", GroupLabel: "Salesforce",
				Explanation: "Write the fields and the external ID to the one matching record by its ID.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToUpdateRecordInput,
			Updated:        sdkgo.GoTo(prepareLinkedReadBack{configuration: flow.configuration}),
			RecordRejected: sdkgo.GoTo(reportLinkRejected{}),
		})),
		dex.DefineStep(salesforce.NewUpsertRecordByExternalIDStep(salesforce.UpsertRecordByExternalIDStepConfig[WriteRequest]{
			StepType: createRecordStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "salesforce", GroupLabel: "Salesforce",
				Explanation: "Create the record by its external ID, so a retried attempt updates the same record instead of adding one.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToUpsertRecordByExternalIDInput,
			Upserted:       sdkgo.GoTo(prepareCreatedReadBack{configuration: flow.configuration}),
			RecordRejected: sdkgo.GoTo(reportCreateRejected{}),
		})),
		dex.DefineStep(prepareLinkedReadBack{configuration: flow.configuration}),
		dex.DefineStep(prepareCreatedReadBack{configuration: flow.configuration}),
		dex.DefineStep(reportLinkRejected{}),
		dex.DefineStep(reportCreateRejected{}),
		dex.DefineStep(salesforce.NewGetRecordStep(salesforce.GetRecordStepConfig[ReadBackRequest]{
			StepType: readBackStepType, ConnectionName: ConnectionName,
			Annotations: sdkgo.StepAnnotations{
				GroupID: "salesforce", GroupLabel: "Salesforce",
				Explanation: "Read the written fields back from Salesforce to confirm the change.",
			},
			Connection: flow.connection, MapToOperationInput: flow.MapToGetRecordInput,
			Found: sdkgo.GoTo(completeRecordSync{configuration: flow.configuration}),
		})),
		dex.DefineStep(completeRecordSync{configuration: flow.configuration}),
	}
}

// GetRPCs returns the summary and display RPCs used by Dex Web.
func (flow *Flow) GetRPCs() []dex.RPCDef {
	return []dex.RPCDef{
		dex.DefineRPC(flow.GetDexSummary, nil),
		dex.DefineRPC(flow.GetDexDisplay, nil),
	}
}

// GetPersistenceSchema registers the request and outcome Attributes.
func (*Flow) GetPersistenceSchema() dex.PersistenceSchema {
	return dex.PersistenceSchema{Attributes: []dex.AttributeDef{requestAttribute, outcomeAttribute}}
}

// GetDexSummary returns the request and outcome.
//
// dex:field attribute-key:salesforce-record-sync-request value-type:json editable:false description:"Match value, external ID, and field values"
// dex:field attribute-key:salesforce-record-sync-outcome value-type:json editable:false description:"Record sync outcome"
func (*Flow) GetDexSummary(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := recordSyncInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"salesforce-record-sync-request": request,
		"salesforce-record-sync-outcome": outcome,
	}}, nil
}

// GetDexDisplay returns the request and outcome.
//
// dex:field attribute-key:salesforce-record-sync-request value-type:json editable:false description:"Match value, external ID, and field values"
// dex:field attribute-key:salesforce-record-sync-outcome value-type:json editable:false description:"Matched, linked, or created record and its read-back"
func (*Flow) GetDexDisplay(ctx dex.Context, _ dex.None) (*dex.RPCResult[map[string]any], error) {
	request, outcome, err := recordSyncInspection(ctx)
	if err != nil {
		return nil, err
	}
	return &dex.RPCResult[map[string]any]{Output: map[string]any{
		"salesforce-record-sync-request": request,
		"salesforce-record-sync-outcome": outcome,
	}}, nil
}

// MapToQueryRecordsInput binds the configured names as identifiers and the match value as a string literal.
func (flow *Flow) MapToQueryRecordsInput(input Input) salesforce.QueryRecordsInput {
	return salesforce.QueryRecordsInput{SOQL: matchQuery, Bindings: map[string]salesforce.SOQLValue{
		"externalIdField": salesforce.SOQLIdentifier(flow.configuration.ExternalIDField),
		"sObjectType":     salesforce.SOQLIdentifier(flow.configuration.SObjectType),
		"matchField":      salesforce.SOQLIdentifier(flow.configuration.MatchField),
		"matchValue":      salesforce.SOQLString(input.MatchValue),
	}}
}

// MapToUpdateRecordInput writes the fields and stamps the external ID on the matched record.
func (flow *Flow) MapToUpdateRecordInput(request WriteRequest) salesforce.UpdateRecordInput {
	fields := jsonTextFields(request.Fields)
	fields[flow.configuration.ExternalIDField] = jsonText(request.ExternalID)
	return salesforce.UpdateRecordInput{SObjectType: flow.configuration.SObjectType, RecordID: request.RecordID, Fields: fields}
}

// MapToUpsertRecordByExternalIDInput creates the record keyed by the external ID.
func (flow *Flow) MapToUpsertRecordByExternalIDInput(request WriteRequest) salesforce.UpsertRecordByExternalIDInput {
	return salesforce.UpsertRecordByExternalIDInput{
		SObjectType: flow.configuration.SObjectType, ExternalIDField: flow.configuration.ExternalIDField,
		ExternalIDValue: request.ExternalID, Fields: jsonTextFields(request.Fields),
	}
}

// MapToGetRecordInput reads back the written record.
func (flow *Flow) MapToGetRecordInput(request ReadBackRequest) salesforce.GetRecordInput {
	return salesforce.GetRecordInput{SObjectType: flow.configuration.SObjectType, RecordID: request.RecordID, Fields: request.Fields}
}

func recordSyncInspection(ctx dex.Context) (Input, Outcome, error) {
	request, err := optionalAttribute(ctx, requestAttribute)
	if err != nil {
		return Input{}, Outcome{}, err
	}
	outcome, err := optionalAttribute(ctx, outcomeAttribute)
	if err != nil {
		return Input{}, Outcome{}, err
	}
	return request, outcome, nil
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

// dex:group group-id:record-sync group-label:"Record sync"
// dex:explanation text:"Validate and record the match value, external ID, and field values."
type recordSyncRequest struct {
	dex.StepDefaults
	configuration SyncConfiguration
}

func (recordSyncRequest) GetStepType() string { return recordRequestStepType }

// WaitFor skips immediately because Dex Web invokes the start Step's WaitFor.
func (recordSyncRequest) WaitFor(dex.Context, Input) (*dex.Wait, error) {
	return dex.SkipWaitImmediately(), nil
}

func (step recordSyncRequest) Execute(ctx dex.Context, input Input) (*dex.StepDecision, error) {
	if strings.TrimSpace(step.configuration.ExternalIDField) == "" {
		return dex.ForceFail("configure the External ID field on the FindMatchingRecords Step and restart the Worker"), nil
	}
	input.MatchValue, input.ExternalID = strings.TrimSpace(input.MatchValue), strings.TrimSpace(input.ExternalID)
	if input.MatchValue == "" || input.ExternalID == "" {
		return dex.ForceFail("matchValue and externalId are required"), nil
	}
	if hasReservedField(input.Fields, step.configuration.ExternalIDField) {
		return dex.ForceFail("fields must not set Id or the External ID field, which the Flow writes itself"), nil
	}
	if err := requestAttribute.Set(ctx, input); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[Input](findMatchesStepType), input), nil
}

// dex:group group-id:record-sync group-label:"Record sync"
// dex:explanation text:"Link the one match, create when nothing matches, and stop on an ambiguous or conflicting match."
type reviewMatchingRecords struct {
	dex.StepDefaultsNoWaitFor[salesforce.QueryRecordsResult]
	configuration SyncConfiguration
}

func (reviewMatchingRecords) GetStepType() string { return reviewMatchesStepType }

func (step reviewMatchingRecords) Execute(ctx dex.Context, result salesforce.QueryRecordsResult) (*dex.StepDecision, error) {
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	matches := result.Value.Records
	outcome := Outcome{SObjectType: step.configuration.SObjectType, CandidateRecordIDs: recordIDs(matches)}
	writeRequest := WriteRequest{ExternalID: request.ExternalID, Fields: request.Fields}
	if len(matches) == 0 {
		return dex.GoTo(sdkgo.StepRef[WriteRequest](createRecordStepType), writeRequest), nil
	}
	existingExternalID, _ := matches[0].StringField(step.configuration.ExternalIDField)
	switch {
	case len(matches) > 1:
		outcome.Status = StatusAmbiguousMatch
	case existingExternalID != "" && existingExternalID != request.ExternalID:
		outcome.Status = StatusConflictingExternalID
	default:
		writeRequest.RecordID = matches[0].ID
		return dex.GoTo(sdkgo.StepRef[WriteRequest](linkRecordStepType), writeRequest), nil
	}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:record-sync group-label:"Record sync"
// dex:explanation text:"Record the linked record and read back the fields the link wrote."
type prepareLinkedReadBack struct {
	dex.StepDefaultsNoWaitFor[salesforce.UpdateRecordResult]
	configuration SyncConfiguration
}

func (prepareLinkedReadBack) GetStepType() string { return prepareLinkedReadBackStepType }

func (step prepareLinkedReadBack) Execute(ctx dex.Context, result salesforce.UpdateRecordResult) (*dex.StepDecision, error) {
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := Outcome{Status: StatusLinked, SObjectType: step.configuration.SObjectType, RecordID: result.Value.ID}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ReadBackRequest](readBackStepType), step.configuration.readBackRequest(outcome.RecordID, request)), nil
}

// dex:group group-id:record-sync group-label:"Record sync"
// dex:explanation text:"Record the created or externally matched record and read back the fields the upsert wrote."
type prepareCreatedReadBack struct {
	dex.StepDefaultsNoWaitFor[salesforce.UpsertRecordByExternalIDResult]
	configuration SyncConfiguration
}

func (prepareCreatedReadBack) GetStepType() string { return prepareCreatedReadBackStepType }

func (step prepareCreatedReadBack) Execute(ctx dex.Context, result salesforce.UpsertRecordByExternalIDResult) (*dex.StepDecision, error) {
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome := Outcome{Status: StatusCreated, SObjectType: step.configuration.SObjectType, RecordID: result.Value.ID}
	if !result.Value.IsCreated {
		outcome.Status = StatusUpdatedByExternalID
	}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GoTo(sdkgo.StepRef[ReadBackRequest](readBackStepType), step.configuration.readBackRequest(outcome.RecordID, request)), nil
}

// dex:group group-id:record-sync group-label:"Record sync"
// dex:explanation text:"Complete as rejected with Salesforce's error codes when it refuses the link."
type reportLinkRejected struct {
	dex.StepDefaultsNoWaitFor[salesforce.UpdateRecordResult]
}

func (reportLinkRejected) GetStepType() string { return reportLinkRejectedStepType }

func (reportLinkRejected) Execute(ctx dex.Context, result salesforce.UpdateRecordResult) (*dex.StepDecision, error) {
	outcome := Outcome{Status: StatusRejected, ProviderErrors: result.Value.ProviderErrors}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:record-sync group-label:"Record sync"
// dex:explanation text:"Complete as rejected with Salesforce's error codes when it refuses the create."
type reportCreateRejected struct {
	dex.StepDefaultsNoWaitFor[salesforce.UpsertRecordByExternalIDResult]
}

func (reportCreateRejected) GetStepType() string { return reportCreateRejectedStepType }

func (reportCreateRejected) Execute(ctx dex.Context, result salesforce.UpsertRecordByExternalIDResult) (*dex.StepDecision, error) {
	outcome := Outcome{Status: StatusRejected, ProviderErrors: result.Value.ProviderErrors}
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// dex:group group-id:record-sync group-label:"Record sync"
// dex:explanation text:"Confirm the read-back carries the external ID and complete the Flow."
type completeRecordSync struct {
	dex.StepDefaultsNoWaitFor[salesforce.GetRecordResult]
	configuration SyncConfiguration
}

func (completeRecordSync) GetStepType() string { return completeStepType }

func (step completeRecordSync) Execute(ctx dex.Context, result salesforce.GetRecordResult) (*dex.StepDecision, error) {
	request, err := requestAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	outcome, err := outcomeAttribute.Get(ctx)
	if err != nil {
		return nil, err
	}
	readBackExternalID, _ := result.Value.StringField(step.configuration.ExternalIDField)
	if readBackExternalID != request.ExternalID {
		return dex.ForceFail("the read-back record does not carry the external ID"), nil
	}
	record := result.Value
	outcome.Record = &record
	if err := outcomeAttribute.Set(ctx, outcome); err != nil {
		return nil, err
	}
	return dex.GracefulComplete(outcome), nil
}

// readBackRequest reads the external ID field and every field the request wrote, in a stable order.
func (configuration SyncConfiguration) readBackRequest(recordID string, request Input) ReadBackRequest {
	writtenFields := make([]string, 0, len(request.Fields))
	for name := range request.Fields {
		writtenFields = append(writtenFields, name)
	}
	sort.Strings(writtenFields)
	return ReadBackRequest{RecordID: recordID, Fields: append([]string{configuration.ExternalIDField}, writtenFields...)}
}

// hasReservedField reports whether fields sets Id or the External ID field, which the Flow writes itself.
func hasReservedField(fields map[string]string, externalIDField string) bool {
	for name := range fields {
		if strings.EqualFold(name, externalIDField) || strings.EqualFold(name, "Id") {
			return true
		}
	}
	return false
}

func recordIDs(records []salesforce.Record) []string {
	ids := make([]string, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.ID)
	}
	return ids
}

func jsonTextFields(fields map[string]string) map[string]json.RawMessage {
	encoded := make(map[string]json.RawMessage, len(fields)+1)
	for name, value := range fields {
		encoded[name] = jsonText(value)
	}
	return encoded
}

func jsonText(value string) json.RawMessage {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err) // A Go string always encodes as JSON.
	}
	return encoded
}

var _ dex.Flow = (*Flow)(nil)
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexSummary
var _ dex.RPC[dex.None, map[string]any] = (*Flow)(nil).GetDexDisplay
