// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package insightly

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// RecordType selects one primary Insightly CRM resource.
type RecordType string

const (
	// RecordTypeContacts selects contacts.
	RecordTypeContacts RecordType = "contacts"
	// RecordTypeLeads selects leads.
	RecordTypeLeads RecordType = "leads"
	// RecordTypeOrganisations selects organisations.
	RecordTypeOrganisations RecordType = "organisations"
	// RecordTypeOpportunities selects opportunities.
	RecordTypeOpportunities RecordType = "opportunities"
	// RecordTypeProjects selects projects.
	RecordTypeProjects RecordType = "projects"
	// RecordTypeTasks selects tasks.
	RecordTypeTasks RecordType = "tasks"
)

var insightlyFieldNamePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,99}$`)

// Record is one CRM resource with the complete provider field graph.
type Record struct {
	// RecordType is the selected CRM resource.
	RecordType RecordType `json:"recordType"`
	// ID is Insightly's positive numeric record identifier.
	ID int64 `json:"id"`
	// Fields holds the provider JSON fields, including custom fields and subcollections.
	Fields map[string]json.RawMessage `json:"fields"`
}

// RecordsPage is one bounded page of CRM records.
type RecordsPage struct {
	// Records are the records on this page.
	Records []Record `json:"records"`
}

// ListRecordsInput selects a bounded page of primary CRM records.
type ListRecordsInput struct {
	// RecordType selects contacts, leads, organisations, opportunities, projects, or tasks.
	RecordType RecordType `json:"recordType"`
	// Top is the page size from 1 through 500.
	Top int `json:"top"`
	// Skip is the number of records before this page.
	Skip int `json:"skip,omitempty"`
}

// GetRecordInput identifies a primary CRM record.
type GetRecordInput struct {
	// RecordType selects the CRM resource.
	RecordType RecordType `json:"recordType"`
	// ID is the positive numeric record identifier.
	ID int64 `json:"id"`
}

// SearchRecordsInput selects one page by an Insightly field's exact value.
type SearchRecordsInput struct {
	// RecordType selects the CRM resource.
	RecordType RecordType `json:"recordType"`
	// FieldName is a provider field name such as EMAIL_ADDRESS.
	FieldName string `json:"fieldName"`
	// FieldValue is the value to match.
	FieldValue string `json:"fieldValue"`
	// Top is the page size from 1 through 500.
	Top int `json:"top"`
	// Skip is the number of records before this page.
	Skip int `json:"skip,omitempty"`
}

// ListRecordsOperation reads one resource page.
type ListRecordsOperation struct{ client *Client }

// GetRecordOperation reads one resource by ID.
type GetRecordOperation struct{ client *Client }

// SearchRecordsOperation searches one resource by an exact field value.
type SearchRecordsOperation struct{ client *Client }

// ListRecords binds the generic CRM list Query to this client.
func (client *Client) ListRecords() ListRecordsOperation { return ListRecordsOperation{client: client} }

// GetRecord binds the generic CRM read Query to this client.
func (client *Client) GetRecord() GetRecordOperation { return GetRecordOperation{client: client} }

// SearchRecords binds the generic CRM search Query to this client.
func (client *Client) SearchRecords() SearchRecordsOperation {
	return SearchRecordsOperation{client: client}
}

// Definition returns the generated list Query contract.
func (ListRecordsOperation) Definition() sdkgo.QueryDefinition { return ListRecordsDefinition }

// Definition returns the generated read Query contract.
func (GetRecordOperation) Definition() sdkgo.QueryDefinition { return GetRecordDefinition }

// Definition returns the generated search Query contract.
func (SearchRecordsOperation) Definition() sdkgo.QueryDefinition { return SearchRecordsDefinition }

// Invoke reads one bounded page from the selected resource.
func (operation ListRecordsOperation) Invoke(call sdkgo.Call, input ListRecordsInput) sdkgo.QueryAttempt[RecordsPage] {
	const operationID = "listRecords"
	if err := input.RecordType.validate(); err != nil {
		return queryBranch(operationID, ListRecordsBranchDefect, RecordsPage{}, sdkgo.FailureValidation, err.Error())
	}
	if err := validatePage(input.Top, input.Skip); err != nil {
		return queryBranch(operationID, ListRecordsBranchDefect, RecordsPage{}, sdkgo.FailureValidation, err.Error())
	}
	query := url.Values{"top": {strconv.Itoa(input.Top)}, "skip": {strconv.Itoa(input.Skip)}}
	return invokeQuery(call, operation.client, operationID, input.RecordType.path()+"?"+query.Encode(), queryBranches{found: ListRecordsBranchListed, rejected: ListRecordsBranchProviderRejected, invalid: ListRecordsBranchInvalidResponse, defect: ListRecordsBranchDefect}, func(body []byte) (RecordsPage, error) { return decodeRecordsPage(input.RecordType, body) })
}

// Invoke reads one record by its positive numeric ID.
func (operation GetRecordOperation) Invoke(call sdkgo.Call, input GetRecordInput) sdkgo.QueryAttempt[Record] {
	const operationID = "getRecord"
	if err := input.RecordType.validate(); err != nil {
		return queryBranch(operationID, GetRecordBranchDefect, Record{}, sdkgo.FailureValidation, err.Error())
	}
	if input.ID <= 0 {
		return queryBranch(operationID, GetRecordBranchDefect, Record{}, sdkgo.FailureValidation, "record ID must be positive")
	}
	return invokeQuery(call, operation.client, operationID, input.RecordType.path()+"/"+strconv.FormatInt(input.ID, 10), queryBranches{found: GetRecordBranchFound, notFound: GetRecordBranchNotFound, rejected: GetRecordBranchProviderRejected, invalid: GetRecordBranchInvalidResponse, defect: GetRecordBranchDefect}, func(body []byte) (Record, error) { return decodeRecord(input.RecordType, body) })
}

// Invoke searches the selected resource for one provider field value.
func (operation SearchRecordsOperation) Invoke(call sdkgo.Call, input SearchRecordsInput) sdkgo.QueryAttempt[RecordsPage] {
	const operationID = "searchRecords"
	if err := input.RecordType.validate(); err != nil {
		return queryBranch(operationID, SearchRecordsBranchDefect, RecordsPage{}, sdkgo.FailureValidation, err.Error())
	}
	if !insightlyFieldNamePattern.MatchString(input.FieldName) || input.FieldValue == "" {
		return queryBranch(operationID, SearchRecordsBranchDefect, RecordsPage{}, sdkgo.FailureValidation, "search field name or value is invalid")
	}
	if err := validatePage(input.Top, input.Skip); err != nil {
		return queryBranch(operationID, SearchRecordsBranchDefect, RecordsPage{}, sdkgo.FailureValidation, err.Error())
	}
	query := url.Values{"field_name": {input.FieldName}, "field_value": {input.FieldValue}, "top": {strconv.Itoa(input.Top)}, "skip": {strconv.Itoa(input.Skip)}}
	return invokeQuery(call, operation.client, operationID, input.RecordType.path()+"/Search?"+query.Encode(), queryBranches{found: SearchRecordsBranchSearched, rejected: SearchRecordsBranchProviderRejected, invalid: SearchRecordsBranchInvalidResponse, defect: SearchRecordsBranchDefect}, func(body []byte) (RecordsPage, error) { return decodeRecordsPage(input.RecordType, body) })
}

func (recordType RecordType) validate() error {
	switch recordType {
	case RecordTypeContacts, RecordTypeLeads, RecordTypeOrganisations, RecordTypeOpportunities, RecordTypeProjects, RecordTypeTasks:
		return nil
	default:
		return errors.New("record type must be contacts, leads, organisations, opportunities, projects, or tasks")
	}
}

func (recordType RecordType) path() string {
	switch recordType {
	case RecordTypeContacts:
		return "/Contacts"
	case RecordTypeLeads:
		return "/Leads"
	case RecordTypeOrganisations:
		return "/Organisations"
	case RecordTypeOpportunities:
		return "/Opportunities"
	case RecordTypeProjects:
		return "/Projects"
	case RecordTypeTasks:
		return "/Tasks"
	default:
		return ""
	}
}

func (recordType RecordType) idField() string {
	switch recordType {
	case RecordTypeContacts:
		return "CONTACT_ID"
	case RecordTypeLeads:
		return "LEAD_ID"
	case RecordTypeOrganisations:
		return "ORGANISATION_ID"
	case RecordTypeOpportunities:
		return "OPPORTUNITY_ID"
	case RecordTypeProjects:
		return "PROJECT_ID"
	case RecordTypeTasks:
		return "TASK_ID"
	default:
		return ""
	}
}

func validatePage(top, skip int) error {
	if top < 1 || top > 500 || skip < 0 {
		return errors.New("page size must be 1 through 500 and skip must be nonnegative")
	}
	return nil
}

func decodeRecord(recordType RecordType, body []byte) (Record, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return Record{}, errors.New("Insightly record JSON is invalid")
	}
	var id int64
	if err := json.Unmarshal(fields[recordType.idField()], &id); err != nil || id <= 0 {
		return Record{}, fmt.Errorf("Insightly %s record ID is missing", recordType)
	}
	return Record{RecordType: recordType, ID: id, Fields: fields}, nil
}

func decodeRecordsPage(recordType RecordType, body []byte) (RecordsPage, error) {
	var rawRecords []json.RawMessage
	if err := json.Unmarshal(body, &rawRecords); err != nil || rawRecords == nil {
		return RecordsPage{}, errors.New("Insightly record page JSON is invalid")
	}
	page := RecordsPage{Records: make([]Record, 0, len(rawRecords))}
	for _, rawRecord := range rawRecords {
		record, err := decodeRecord(recordType, rawRecord)
		if err != nil {
			return RecordsPage{}, err
		}
		page.Records = append(page.Records, record)
	}
	return page, nil
}
