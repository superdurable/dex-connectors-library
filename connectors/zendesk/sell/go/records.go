// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// SellResource selects one of the four supported Sell record families.
type SellResource string

const (
	// SellContacts are people and organization contacts.
	SellContacts SellResource = "contacts"
	// SellDeals are sales opportunities.
	SellDeals SellResource = "deals"
	// SellTasks are work items, optionally attached to another record.
	SellTasks SellResource = "tasks"
	// SellNotes are annotations attached to another record.
	SellNotes SellResource = "notes"
)

// SellRecord contains provider identity and common non-secret fields.
type SellRecord struct {
	// ID is the positive Sell record ID.
	ID int64 `json:"id"`
	// Resource identifies the record family.
	Resource SellResource `json:"resource"`
	// Name is a contact or deal name when present.
	Name string `json:"name,omitempty"`
	// EmailAddress is a contact email when present.
	EmailAddress string `json:"emailAddress,omitempty"`
	// Content is task or note text when present.
	Content string `json:"content,omitempty"`
	// Status is the account-defined status when present.
	Status string `json:"status,omitempty"`
	// Completed reports a task's completion state when present.
	Completed *bool `json:"completed,omitempty"`
	// RelatedResourceType is the attached lead, contact, or deal family when present.
	RelatedResourceType string `json:"relatedResourceType,omitempty"`
	// RelatedResourceID is the attached Sell record ID when present.
	RelatedResourceID int64 `json:"relatedResourceId,omitempty"`
}

// RecordsPage contains one bounded page of Sell records.
type RecordsPage struct {
	// Resource is the selected record family.
	Resource SellResource `json:"resource"`
	// Records are the records returned on this page.
	Records []SellRecord `json:"records"`
	// Page is the one-based page number.
	Page int `json:"page"`
	// PerPage is the requested maximum page size.
	PerPage int `json:"perPage"`
}

// ListRecordsInput selects one page of contacts, deals, tasks, or notes.
type ListRecordsInput struct {
	// Resource is contacts, deals, tasks, or notes.
	Resource SellResource `json:"resource"`
	// Page is a positive one-based page number.
	Page int `json:"page"`
	// PerPage is the page size, from 1 through 100.
	PerPage int `json:"perPage"`
}

// GetRecordInput identifies one contact, deal, task, or note.
type GetRecordInput struct {
	// Resource is contacts, deals, tasks, or notes.
	Resource SellResource `json:"resource"`
	// RecordID is the positive decimal record ID.
	RecordID string `json:"recordId"`
}

// CreateRecordInput supplies writable provider fields for one new record.
type CreateRecordInput struct {
	// Resource is contacts, deals, tasks, or notes.
	Resource SellResource `json:"resource"`
	// Fields uses the selected resource's Sell API v2 JSON field names.
	Fields map[string]json.RawMessage `json:"fields"`
}

// UpdateRecordInput supplies absolute field values for one existing record.
type UpdateRecordInput struct {
	// Resource is contacts, deals, tasks, or notes.
	Resource SellResource `json:"resource"`
	// RecordID is the positive decimal record ID.
	RecordID string `json:"recordId"`
	// Fields uses the selected resource's Sell API v2 JSON field names; omitted keys stay unchanged.
	Fields map[string]json.RawMessage `json:"fields"`
}

// DeleteRecordInput identifies the record to delete.
type DeleteRecordInput struct {
	// Resource is contacts, deals, tasks, or notes.
	Resource SellResource `json:"resource"`
	// RecordID is the positive decimal record ID.
	RecordID string `json:"recordId"`
}

// DeleteRecordOutput identifies the record deleted by Sell.
type DeleteRecordOutput struct {
	// Resource is the deleted record family.
	Resource SellResource `json:"resource"`
	// RecordID is the deleted record's ID.
	RecordID string `json:"recordId"`
}

// ListRecordsOperation lists one Sell record family.
type ListRecordsOperation struct{ client *Client }

// GetRecordOperation reads one Sell record.
type GetRecordOperation struct{ client *Client }

// CreateRecordOperation creates one Sell record under a bounded send cap.
type CreateRecordOperation struct{ client *Client }

// UpdateRecordOperation sets fields on one Sell record.
type UpdateRecordOperation struct{ client *Client }

// DeleteRecordOperation deletes one Sell record.
type DeleteRecordOperation struct{ client *Client }

// ListRecords binds the list Query to this client.
func (client *Client) ListRecords() ListRecordsOperation { return ListRecordsOperation{client} }

// GetRecord binds the get Query to this client.
func (client *Client) GetRecord() GetRecordOperation { return GetRecordOperation{client} }

// CreateRecord binds the create Mutation to this client.
func (client *Client) CreateRecord() CreateRecordOperation { return CreateRecordOperation{client} }

// UpdateRecord binds the update Mutation to this client.
func (client *Client) UpdateRecord() UpdateRecordOperation { return UpdateRecordOperation{client} }

// DeleteRecord binds the delete Mutation to this client.
func (client *Client) DeleteRecord() DeleteRecordOperation { return DeleteRecordOperation{client} }

// Definition returns the generated list Query contract.
func (ListRecordsOperation) Definition() sdkgo.QueryDefinition { return ListRecordsDefinition }

// Definition returns the generated get Query contract.
func (GetRecordOperation) Definition() sdkgo.QueryDefinition { return GetRecordDefinition }

// Definition returns the generated create Mutation contract.
func (CreateRecordOperation) Definition() sdkgo.MutationDefinition { return CreateRecordDefinition }

// Definition returns the generated update Mutation contract.
func (UpdateRecordOperation) Definition() sdkgo.MutationDefinition { return UpdateRecordDefinition }

// Definition returns the generated delete Mutation contract.
func (DeleteRecordOperation) Definition() sdkgo.MutationDefinition { return DeleteRecordDefinition }

// IdempotencyKey correlates attempts; Sell does not deduplicate this key.
func (CreateRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates retries of one absolute update.
func (UpdateRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey correlates retries of one delete.
func (DeleteRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke reads a bounded record page.
func (operation ListRecordsOperation) Invoke(call sdkgo.Call, input ListRecordsInput) sdkgo.QueryAttempt[RecordsPage] {
	branches := queryBranches{found: ListRecordsBranchListed, rejected: ListRecordsBranchProviderRejected, invalid: ListRecordsBranchInvalidResponse, defect: ListRecordsBranchDefect}
	if !input.Resource.valid() || input.Page < 1 || input.PerPage < 1 || input.PerPage > 100 {
		return queryBranch("listRecords", branches.defect, RecordsPage{}, sdkgo.FailureValidation, "resource, page, or page size is invalid")
	}
	query := url.Values{"page": {strconv.Itoa(input.Page)}, "per_page": {strconv.Itoa(input.PerPage)}}
	return invokeQuery(call, operation.client, "listRecords", "/"+string(input.Resource)+"?"+query.Encode(), branches, func(body []byte) (RecordsPage, error) {
		return decodeRecordsPage(body, input)
	})
}

// Invoke reads one record by ID.
func (operation GetRecordOperation) Invoke(call sdkgo.Call, input GetRecordInput) sdkgo.QueryAttempt[SellRecord] {
	branches := queryBranches{found: GetRecordBranchFound, notFound: GetRecordBranchNotFound, rejected: GetRecordBranchProviderRejected, invalid: GetRecordBranchInvalidResponse, defect: GetRecordBranchDefect}
	if !input.Resource.valid() || !isPositiveLeadID(input.RecordID) {
		return queryBranch("getRecord", branches.defect, SellRecord{}, sdkgo.FailureValidation, "resource or record ID is invalid")
	}
	return invokeQuery(call, operation.client, "getRecord", "/"+string(input.Resource)+"/"+input.RecordID, branches, func(body []byte) (SellRecord, error) {
		return decodeSellRecord(body, input.Resource)
	})
}

// Invoke creates a record, retrying an unconfirmed POST only within the send cap.
func (operation CreateRecordOperation) Invoke(call sdkgo.Call, input CreateRecordInput) sdkgo.MutationAttempt[SellRecord] {
	const operationName = "createRecord"
	if err := validateRecordFields(input.Resource, input.Fields, true); err != nil {
		return leadWriteBranch(CreateRecordBranchDefect, SellRecord{}, operationName, sdkgo.FailureValidation, err.Error(), call)
	}
	payload, err := json.Marshal(map[string]any{"data": input.Fields})
	if err != nil {
		return leadWriteBranch(CreateRecordBranchDefect, SellRecord{}, operationName, sdkgo.FailureValidation, "record input could not be encoded", call)
	}
	fence, err := singledispatch.NewFence(call, CreateRecordDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return leadWriteBranch(CreateRecordBranchDefect, SellRecord{}, operationName, sdkgo.FailureLocalDefect, "record dispatch fence could not be constructed", call)
	}
	decision := fence.Decide()
	if decision.Marker.ProviderID != "" && decision.Marker.CallID == call.ID {
		body, status, _, readErr := operation.client.get(call, "/"+string(input.Resource)+"/"+decision.Marker.ProviderID)
		if readErr == nil && status == http.StatusOK {
			record, decodeErr := decodeSellRecord(body, input.Resource)
			if decodeErr == nil {
				return sdkgo.NewMutationBranch(CreateRecordBranchCreated, record, nil, leadWriteReceipt(call, decision.Marker.ProviderID))
			}
		}
		return sdkgo.NewMutationRetry[SellRecord](queryFailure(operationName, sdkgo.FailureAvailability, "created record read-back is unavailable"), 0)
	}
	if decision.Kind != singledispatch.SendAllowed && decision.Kind != singledispatch.ReadBackThenResendAllowed {
		return sdkgo.NewMutationUncertain(SellRecord{}, queryFailure(operationName, sdkgo.FailureTransport, "an earlier record create may have succeeded"), leadWriteReceipt(call, ""))
	}
	var body []byte
	var status int
	var retryAfter time.Duration
	var sendErr error
	err = fence.Dispatch(func(ctx context.Context) error {
		body, status, retryAfter, sendErr = operation.client.request(ctx, call, http.MethodPost, "/"+string(input.Resource), payload)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[SellRecord](queryFailure(operationName, sdkgo.FailureAvailability, "record dispatch checkpoint is unavailable"), 0)
	}
	if errors.Is(sendErr, errCredential) {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[SellRecord](queryFailure(operationName, sdkgo.FailureAvailability, "record no-send checkpoint is unavailable"), 0)
		}
		return leadWriteBranch(CreateRecordBranchDefect, SellRecord{}, operationName, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid", call)
	}
	if status == http.StatusTooManyRequests || status == http.StatusServiceUnavailable {
		if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[SellRecord](queryFailure(operationName, sdkgo.FailureAvailability, "record no-send checkpoint is unavailable"), 0)
		}
		return sdkgo.NewMutationRetry[SellRecord](queryFailure(operationName, sdkgo.FailureRateLimit, "Zendesk Sell deferred the record create"), retryAfter)
	}
	if status == http.StatusCreated || status == http.StatusOK {
		if sendErr == nil {
			record, decodeErr := decodeSellRecord(body, input.Resource)
			if decodeErr == nil {
				providerID := strconv.FormatInt(record.ID, 10)
				_ = fence.RecordProviderID(providerID)
				return sdkgo.NewMutationBranch(CreateRecordBranchCreated, record, nil, leadWriteReceipt(call, providerID))
			}
		}
	} else if status >= 400 && status < 500 && decision.MaximumPriorSends == 0 {
		return leadWriteBranch(CreateRecordBranchProviderRejected, SellRecord{}, operationName, sdkgo.FailureProviderRejection, "Zendesk Sell rejected the record create", call)
	}
	if decision.MaximumPriorSends+1 < call.MaxSends() {
		return sdkgo.NewMutationRetry[SellRecord](queryFailure(operationName, sdkgo.FailureTransport, "record create response is unconfirmed"), retryAfter)
	}
	return sdkgo.NewMutationUncertain(SellRecord{}, queryFailure(operationName, sdkgo.FailureTransport, "record create may have succeeded"), leadWriteReceipt(call, ""))
}

// Invoke sets absolute values on an existing record; repeating the same PUT is safe.
func (operation UpdateRecordOperation) Invoke(call sdkgo.Call, input UpdateRecordInput) sdkgo.MutationAttempt[SellRecord] {
	const operationName = "updateRecord"
	if !isPositiveLeadID(input.RecordID) {
		return leadWriteBranch(UpdateRecordBranchDefect, SellRecord{}, operationName, sdkgo.FailureValidation, "record ID must be positive", call)
	}
	if err := validateRecordFields(input.Resource, input.Fields, false); err != nil {
		return leadWriteBranch(UpdateRecordBranchDefect, SellRecord{}, operationName, sdkgo.FailureValidation, err.Error(), call)
	}
	payload, err := json.Marshal(map[string]any{"data": input.Fields})
	if err != nil {
		return leadWriteBranch(UpdateRecordBranchDefect, SellRecord{}, operationName, sdkgo.FailureValidation, "record input could not be encoded", call)
	}
	body, status, retryAfter, requestErr := operation.client.request(call.Context, call, http.MethodPut, "/"+string(input.Resource)+"/"+input.RecordID, payload)
	if errors.Is(requestErr, errCredential) {
		return leadWriteBranch(UpdateRecordBranchDefect, SellRecord{}, operationName, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid", call)
	}
	if requestErr != nil && !errors.Is(requestErr, providerhttp.ErrBodyTooLarge) || status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[SellRecord](queryFailure(operationName, sdkgo.FailureTransport, "Zendesk Sell update is temporarily unavailable"), retryAfter)
	}
	if status == http.StatusNotFound {
		return leadWriteBranch(UpdateRecordBranchNotFound, SellRecord{}, operationName, sdkgo.FailureNotFound, "record does not exist", call)
	}
	if status != http.StatusOK {
		return leadWriteBranch(UpdateRecordBranchProviderRejected, SellRecord{}, operationName, sdkgo.FailureProviderRejection, "Zendesk Sell rejected the record update", call)
	}
	if requestErr != nil {
		return leadWriteBranch(UpdateRecordBranchInvalidResponse, SellRecord{}, operationName, sdkgo.FailureResponseTooLarge, "record response exceeded the configured limit", call)
	}
	record, err := decodeSellRecord(body, input.Resource)
	if err != nil {
		return leadWriteBranch(UpdateRecordBranchInvalidResponse, SellRecord{}, operationName, sdkgo.FailureProtocol, "Zendesk Sell returned invalid record JSON", call)
	}
	return sdkgo.NewMutationBranch(UpdateRecordBranchUpdated, record, nil, leadWriteReceipt(call, input.RecordID))
}

// Invoke deletes an existing record; repeating the DELETE cannot create another.
func (operation DeleteRecordOperation) Invoke(call sdkgo.Call, input DeleteRecordInput) sdkgo.MutationAttempt[DeleteRecordOutput] {
	const operationName = "deleteRecord"
	if !input.Resource.valid() || !isPositiveLeadID(input.RecordID) {
		return leadWriteBranch(DeleteRecordBranchDefect, DeleteRecordOutput{}, operationName, sdkgo.FailureValidation, "resource or record ID is invalid", call)
	}
	_, status, retryAfter, requestErr := operation.client.request(call.Context, call, http.MethodDelete, "/"+string(input.Resource)+"/"+input.RecordID, nil)
	if errors.Is(requestErr, errCredential) {
		return leadWriteBranch(DeleteRecordBranchDefect, DeleteRecordOutput{}, operationName, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid", call)
	}
	if requestErr != nil || status == http.StatusTooManyRequests || status == http.StatusRequestTimeout || status >= 500 {
		return sdkgo.NewMutationRetry[DeleteRecordOutput](queryFailure(operationName, sdkgo.FailureTransport, "Zendesk Sell delete is temporarily unavailable"), retryAfter)
	}
	if status == http.StatusNotFound {
		return leadWriteBranch(DeleteRecordBranchNotFound, DeleteRecordOutput{}, operationName, sdkgo.FailureNotFound, "record does not exist", call)
	}
	if status != http.StatusNoContent {
		return leadWriteBranch(DeleteRecordBranchProviderRejected, DeleteRecordOutput{}, operationName, sdkgo.FailureProviderRejection, "Zendesk Sell rejected the record delete", call)
	}
	return sdkgo.NewMutationBranch(DeleteRecordBranchDeleted, DeleteRecordOutput{Resource: input.Resource, RecordID: input.RecordID}, nil, leadWriteReceipt(call, input.RecordID))
}

func (resource SellResource) valid() bool {
	switch resource {
	case SellContacts, SellDeals, SellTasks, SellNotes:
		return true
	}
	return false
}

func validateRecordFields(resource SellResource, fields map[string]json.RawMessage, isCreate bool) error {
	if !resource.valid() {
		return fmt.Errorf("unsupported Sell resource")
	}
	if len(fields) == 0 || len(fields) > 32 {
		return fmt.Errorf("one through 32 record fields are required")
	}
	for name, value := range fields {
		if !allowedRecordField(resource, name) || !json.Valid(value) {
			return fmt.Errorf("record field %q is invalid", name)
		}
	}
	if !isCreate {
		return nil
	}
	switch resource {
	case SellContacts:
		if !hasNonemptyTextField(fields, "last_name") && !hasNonemptyTextField(fields, "name") {
			return fmt.Errorf("a contact needs last_name or name")
		}
	case SellDeals:
		if !hasNonemptyTextField(fields, "name") {
			return fmt.Errorf("a deal needs name")
		}
	case SellTasks:
		if !hasNonemptyTextField(fields, "content") {
			return fmt.Errorf("a task needs content")
		}
	case SellNotes:
		if !hasNonemptyTextField(fields, "content") || !hasNonemptyTextField(fields, "resource_type") || len(fields["resource_id"]) == 0 {
			return fmt.Errorf("a note needs content, resource_type, and resource_id")
		}
	}
	return nil
}

func allowedRecordField(resource SellResource, name string) bool {
	allowed := map[SellResource]string{
		SellContacts: "|name|first_name|last_name|is_organization|contact_id|parent_organization_id|owner_id|customer_status|prospect_status|title|description|industry|website|email|phone|mobile|tags|custom_fields|",
		SellDeals:    "|name|value|currency|hot|stage_id|source_id|loss_reason_id|unqualified_reason_id|contact_id|owner_id|estimated_close_date|customized_win_likelihood|tags|custom_fields|",
		SellTasks:    "|content|due_date|owner_id|resource_type|resource_id|completed|remind_at|",
		SellNotes:    "|resource_type|resource_id|content|is_important|tags|type|",
	}
	return strings.Contains(allowed[resource], "|"+name+"|")
}

func hasNonemptyTextField(fields map[string]json.RawMessage, name string) bool {
	var value string
	return json.Unmarshal(fields[name], &value) == nil && strings.TrimSpace(value) != ""
}

func decodeRecordsPage(body []byte, input ListRecordsInput) (RecordsPage, error) {
	var envelope struct {
		Items []struct {
			Data json.RawMessage `json:"data"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return RecordsPage{}, err
	}
	if envelope.Items == nil {
		return RecordsPage{}, fmt.Errorf("record page missing")
	}
	records := make([]SellRecord, 0, len(envelope.Items))
	for _, item := range envelope.Items {
		record, err := decodeSellRecordData(item.Data, input.Resource)
		if err != nil {
			return RecordsPage{}, err
		}
		records = append(records, record)
	}
	return RecordsPage{Resource: input.Resource, Records: records, Page: input.Page, PerPage: input.PerPage}, nil
}

func decodeSellRecord(body []byte, resource SellResource) (SellRecord, error) {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return SellRecord{}, err
	}
	return decodeSellRecordData(envelope.Data, resource)
}

func decodeSellRecordData(body []byte, resource SellResource) (SellRecord, error) {
	var wire struct {
		ID           int64  `json:"id"`
		Name         string `json:"name"`
		FirstName    string `json:"first_name"`
		LastName     string `json:"last_name"`
		Email        string `json:"email"`
		Content      string `json:"content"`
		Status       string `json:"status"`
		Completed    *bool  `json:"completed"`
		ResourceType string `json:"resource_type"`
		ResourceID   int64  `json:"resource_id"`
	}
	if len(body) == 0 || string(body) == "null" {
		return SellRecord{}, fmt.Errorf("record data missing")
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return SellRecord{}, err
	}
	if wire.ID <= 0 {
		return SellRecord{}, fmt.Errorf("record ID missing")
	}
	name := wire.Name
	if name == "" {
		name = strings.TrimSpace(wire.FirstName + " " + wire.LastName)
	}
	return SellRecord{ID: wire.ID, Resource: resource, Name: name, EmailAddress: wire.Email, Content: wire.Content, Status: wire.Status, Completed: wire.Completed, RelatedResourceType: wire.ResourceType, RelatedResourceID: wire.ResourceID}, nil
}
