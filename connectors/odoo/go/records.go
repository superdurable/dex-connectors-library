// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package odoo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

var odooModelIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_.]{1,127}$`)
var odooFieldIdentifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,127}$`)

// Record is an Odoo model row containing only explicitly selected safe fields.
type Record struct {
	// ID is the positive Odoo record ID.
	ID int64 `json:"id"`
	// Fields contains the selected non-secret model fields.
	Fields map[string]json.RawMessage `json:"fields"`
}

// RecordsPage contains one bounded page of Odoo model rows.
type RecordsPage struct {
	// Records are the selected rows in Odoo search order.
	Records []Record `json:"records"`
}

// ListRecordsInput selects a model, safe fields, and a bounded page.
type ListRecordsInput struct {
	// Model is res.partner, crm.lead, mail.activity, or a custom x_ model.
	Model string `json:"model"`
	// Fields lists one to twenty non-secret fields to return.
	Fields []string `json:"fields"`
	// Limit is one through one hundred.
	Limit int `json:"limit"`
	// Offset skips this many rows.
	Offset int `json:"offset"`
}

// GetRecordInput selects a model row and its returned fields.
type GetRecordInput struct {
	// Model is a supported Odoo model name.
	Model string `json:"model"`
	// ID is a positive record ID.
	ID int64 `json:"id"`
	// Fields lists one to twenty non-secret fields.
	Fields []string `json:"fields"`
}

// CreateRecordInput supplies a model row and optional read-back selector.
type CreateRecordInput struct {
	// Model is a supported Odoo model name.
	Model string `json:"model"`
	// Values contains non-secret JSON field values.
	Values map[string]json.RawMessage `json:"values"`
	// ReadBackField is a field with a stable value to search after ambiguous sends; blank disables read-back.
	ReadBackField string `json:"readBackField,omitempty"`
}

// CreatedRecord identifies the new Odoo row.
type CreatedRecord struct {
	// ID is the positive new record ID.
	ID int64 `json:"id"`
}

// UpdateRecordInput sets selected model fields to absolute values.
type UpdateRecordInput struct {
	// Model is a supported Odoo model name.
	Model string `json:"model"`
	// ID is a positive record ID.
	ID int64 `json:"id"`
	// Values contains non-secret absolute field values.
	Values map[string]json.RawMessage `json:"values"`
}

// UpdatedRecord identifies the updated Odoo row.
type UpdatedRecord struct {
	// ID is the positive record ID.
	ID int64 `json:"id"`
}

// DeleteRecordInput selects a model row to delete.
type DeleteRecordInput struct {
	// Model is a supported Odoo model name.
	Model string `json:"model"`
	// ID is a positive record ID.
	ID int64 `json:"id"`
}

// DeletedRecord identifies the removed Odoo row.
type DeletedRecord struct {
	// ID is the positive record ID.
	ID int64 `json:"id"`
}

func validOdooModel(model string) bool {
	if !odooModelIdentifier.MatchString(model) || strings.Contains(model, "..") || strings.HasSuffix(model, ".") {
		return false
	}
	return model == "res.partner" || model == "crm.lead" || model == "mail.activity" || strings.HasPrefix(model, "x_")
}

func validOdooField(field string) bool {
	if !odooFieldIdentifier.MatchString(field) || field == "id" {
		return false
	}
	for _, sensitive := range []string{"password", "secret", "token", "api_key", "private_key", "credential"} {
		if strings.Contains(field, sensitive) {
			return false
		}
	}
	return true
}

func validOdooFields(fields []string) bool {
	if len(fields) < 1 || len(fields) > 20 {
		return false
	}
	seen := make(map[string]bool, len(fields))
	for _, field := range fields {
		if !validOdooField(field) || seen[field] {
			return false
		}
		seen[field] = true
	}
	return true
}

func validOdooValues(values map[string]json.RawMessage) bool {
	if len(values) < 1 || len(values) > 20 {
		return false
	}
	for field, value := range values {
		if !validOdooField(field) || len(value) == 0 || len(value) > 65536 || !json.Valid(value) {
			return false
		}
	}
	return true
}

func selectedOdooFields(fields []string) []string {
	selected := append([]string{"id"}, fields...)
	return selected
}

func decodeRecords(body []byte, fields []string, limit int) ([]Record, error) {
	var wire []map[string]json.RawMessage
	if err := json.Unmarshal(body, &wire); err != nil || wire == nil || len(wire) > limit {
		return nil, errors.New("record page is invalid")
	}
	records := make([]Record, 0, len(wire))
	for _, raw := range wire {
		var id int64
		if json.Unmarshal(raw["id"], &id) != nil || id <= 0 {
			return nil, errors.New("record ID is invalid")
		}
		selected := make(map[string]json.RawMessage, len(fields))
		for _, field := range fields {
			value, present := raw[field]
			if !present {
				return nil, fmt.Errorf("record field %s is missing", field)
			}
			selected[field] = value
		}
		records = append(records, Record{ID: id, Fields: selected})
	}
	return records, nil
}

// ListRecordsOperation lists rows in a selected Odoo model.
type ListRecordsOperation struct{ client *Client }

// ListRecords binds the selected model list Query.
func (client *Client) ListRecords() ListRecordsOperation { return ListRecordsOperation{client} }

// Definition returns the generated model list contract.
func (ListRecordsOperation) Definition() sdkgo.QueryDefinition { return ListRecordsDefinition }

// Invoke lists a bounded page and returns only selected non-secret fields.
func (operation ListRecordsOperation) Invoke(call sdkgo.Call, input ListRecordsInput) sdkgo.QueryAttempt[RecordsPage] {
	branches := contactQueryBranches{found: ListRecordsBranchListed, rejected: ListRecordsBranchProviderRejected, invalid: ListRecordsBranchInvalidResponse, defect: ListRecordsBranchDefect}
	if !validOdooModel(input.Model) || !validOdooFields(input.Fields) || input.Limit < 1 || input.Limit > 100 || input.Offset < 0 {
		return contactQueryBranch[RecordsPage]("listRecords", branches.defect, sdkgo.FailureValidation, "model, fields, or page is invalid")
	}
	body, _ := json.Marshal(map[string]any{"domain": []any{}, "fields": selectedOdooFields(input.Fields), "limit": input.Limit, "offset": input.Offset})
	return invokeContactQuery(call, operation.client, "listRecords", input.Model, "search_read", body, branches, func(body []byte) (RecordsPage, bool, error) {
		records, err := decodeRecords(body, input.Fields, input.Limit)
		return RecordsPage{Records: records}, true, err
	})
}

// GetRecordOperation reads a selected model row.
type GetRecordOperation struct{ client *Client }

// GetRecord binds the selected model read Query.
func (client *Client) GetRecord() GetRecordOperation { return GetRecordOperation{client} }

// Definition returns the generated model read contract.
func (GetRecordOperation) Definition() sdkgo.QueryDefinition { return GetRecordDefinition }

// Invoke reads one row and returns only selected non-secret fields.
func (operation GetRecordOperation) Invoke(call sdkgo.Call, input GetRecordInput) sdkgo.QueryAttempt[Record] {
	branches := contactQueryBranches{found: GetRecordBranchFound, notFound: GetRecordBranchNotFound, rejected: GetRecordBranchProviderRejected, invalid: GetRecordBranchInvalidResponse, defect: GetRecordBranchDefect}
	if !validOdooModel(input.Model) || input.ID <= 0 || !validOdooFields(input.Fields) {
		return contactQueryBranch[Record]("getRecord", branches.defect, sdkgo.FailureValidation, "model, ID, or fields are invalid")
	}
	body, _ := json.Marshal(map[string]any{"ids": []int64{input.ID}, "fields": selectedOdooFields(input.Fields)})
	return invokeContactQuery(call, operation.client, "getRecord", input.Model, "read", body, branches, func(body []byte) (Record, bool, error) {
		records, err := decodeRecords(body, input.Fields, 1)
		if err != nil {
			return Record{}, false, err
		}
		if len(records) == 0 {
			return Record{}, false, nil
		}
		if records[0].ID != input.ID {
			return Record{}, false, errors.New("unexpected record ID")
		}
		return records[0], true, nil
	})
}

// CreateRecordOperation creates a row with a bounded send count.
type CreateRecordOperation struct{ client *Client }

// CreateRecord binds selected model creation.
func (client *Client) CreateRecord() CreateRecordOperation { return CreateRecordOperation{client} }

// Definition returns the generated model creation contract.
func (CreateRecordOperation) Definition() sdkgo.MutationDefinition { return CreateRecordDefinition }

// IdempotencyKey identifies the call; Odoo accepts no idempotency header.
func (CreateRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one row, optionally reading it back by a stable field after an ambiguous send.
func (operation CreateRecordOperation) Invoke(call sdkgo.Call, input CreateRecordInput) sdkgo.MutationAttempt[CreatedRecord] {
	if !validOdooModel(input.Model) || !validOdooValues(input.Values) || (input.ReadBackField != "" && (!validOdooField(input.ReadBackField) || len(input.Values[input.ReadBackField]) == 0)) {
		return contactMutationBranch[CreatedRecord]("createRecord", CreateRecordBranchDefect, sdkgo.FailureValidation, "model, values, or read-back field is invalid")
	}
	body, _ := json.Marshal(map[string]any{"vals_list": []any{input.Values}})
	fence, err := singledispatch.NewFence(call, CreateRecordDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return contactMutationBranch[CreatedRecord]("createRecord", CreateRecordBranchDefect, sdkgo.FailureLocalDefect, "dispatch checkpoint is unavailable")
	}
	decision := fence.Decide()
	if decision.MaximumPriorSends > 0 || decision.Kind != singledispatch.SendAllowed {
		id, readError := operation.client.readBackRecord(call, input)
		if id > 0 {
			return sdkgo.NewMutationBranch(CreateRecordBranchCreated, CreatedRecord{ID: id}, nil, sdkgo.Receipt{})
		}
		if readError != nil {
			return sdkgo.NewMutationRetry[CreatedRecord](contactFailure("createRecord", sdkgo.FailureAvailability, "record read-back is unavailable"), 0)
		}
	}
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(CreatedRecord{}, contactFailure("createRecord", sdkgo.FailureTransport, "record creation may have succeeded"), sdkgo.Receipt{})
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.callModel(call, requestContext, input.Model, "create", body)
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[CreatedRecord](contactFailure("createRecord", sdkgo.FailureAvailability, "record was not dispatched"), 0)
	}
	if errors.Is(err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CreatedRecord{}, contactFailure("createRecord", sdkgo.FailureAuthentication, "earlier create may have succeeded"), sdkgo.Receipt{})
		}
		if fence.RecordNotSent() != nil {
			return sdkgo.NewMutationRetry[CreatedRecord](contactFailure("createRecord", sdkgo.FailureAvailability, "no-send checkpoint failed"), 0)
		}
		return contactMutationBranch[CreatedRecord]("createRecord", CreateRecordBranchDefect, sdkgo.FailureAuthentication, "credentials are unavailable")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[CreatedRecord](contactFailure("createRecord", sdkgo.FailureAvailability, "retry checkpoint failed"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if response.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[CreatedRecord](contactFailure("createRecord", kind, "Odoo temporarily rejected the create"), response.retryAfter)
	}
	if err != nil || response.status >= 500 {
		failure := contactFailure("createRecord", sdkgo.FailureAvailability, "record create outcome is unknown")
		if canResend {
			return sdkgo.NewMutationRetry[CreatedRecord](failure, response.retryAfter)
		}
		return sdkgo.NewMutationUncertain(CreatedRecord{}, failure, sdkgo.Receipt{})
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CreatedRecord{}, contactFailure("createRecord", sdkgo.FailureProviderRejection, "earlier create may have succeeded"), sdkgo.Receipt{})
		}
		return contactMutationBranch[CreatedRecord]("createRecord", CreateRecordBranchProviderRejected, sdkgo.FailureProviderRejection, "Odoo rejected record creation")
	}
	var ids []int64
	if json.Unmarshal(response.body, &ids) != nil || len(ids) != 1 || ids[0] <= 0 {
		return sdkgo.NewMutationUncertain(CreatedRecord{}, contactFailure("createRecord", sdkgo.FailureProtocol, "Odoo accepted creation without a valid ID"), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateRecordBranchCreated, CreatedRecord{ID: ids[0]}, nil, sdkgo.Receipt{})
}

func (client *Client) readBackRecord(call sdkgo.Call, input CreateRecordInput) (int64, error) {
	if input.ReadBackField == "" {
		return 0, nil
	}
	var target any
	if json.Unmarshal(input.Values[input.ReadBackField], &target) != nil {
		return 0, errors.New("read-back value is invalid")
	}
	fields := make([]string, 0, len(input.Values))
	for field := range input.Values {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	body, _ := json.Marshal(map[string]any{"domain": [][]any{{input.ReadBackField, "=", target}}, "fields": selectedOdooFields(fields), "limit": 2})
	response := client.callModel(call, call.Context, input.Model, "search_read", body)
	if response.err != nil || response.status != http.StatusOK {
		return 0, errors.New("record read-back failed")
	}
	records, err := decodeRecords(response.body, fields, 2)
	if err != nil {
		return 0, err
	}
	if len(records) != 1 {
		return 0, nil
	}
	for field, expected := range input.Values {
		var actualValue, expectedValue any
		if json.Unmarshal(records[0].Fields[field], &actualValue) != nil || json.Unmarshal(expected, &expectedValue) != nil {
			return 0, errors.New("record read-back comparison failed")
		}
		if !reflect.DeepEqual(actualValue, expectedValue) {
			return 0, nil
		}
	}
	return records[0].ID, nil
}

// UpdateRecordOperation sets selected model fields.
type UpdateRecordOperation struct{ client *Client }

// UpdateRecord binds selected model replacement.
func (client *Client) UpdateRecord() UpdateRecordOperation { return UpdateRecordOperation{client} }

// Definition returns the generated update contract.
func (UpdateRecordOperation) Definition() sdkgo.MutationDefinition { return UpdateRecordDefinition }

// IdempotencyKey identifies the absolute field assignment across retries.
func (UpdateRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets selected fields to absolute JSON values.
func (operation UpdateRecordOperation) Invoke(call sdkgo.Call, input UpdateRecordInput) sdkgo.MutationAttempt[UpdatedRecord] {
	if !validOdooModel(input.Model) || input.ID <= 0 || !validOdooValues(input.Values) {
		return contactMutationBranch[UpdatedRecord]("updateRecord", UpdateRecordBranchDefect, sdkgo.FailureValidation, "model, ID, or values are invalid")
	}
	body, _ := json.Marshal(map[string]any{"ids": []int64{input.ID}, "vals": input.Values})
	return invokeContactBooleanMutation(call, operation.client, "updateRecord", input.Model, "write", body, input.ID, UpdateRecordBranchUpdated, UpdateRecordBranchNotFound, UpdateRecordBranchProviderRejected, UpdateRecordBranchInvalidResponse, UpdateRecordBranchDefect, UpdatedRecord{ID: input.ID})
}

// DeleteRecordOperation deletes one model row.
type DeleteRecordOperation struct{ client *Client }

// DeleteRecord binds selected model deletion.
func (client *Client) DeleteRecord() DeleteRecordOperation { return DeleteRecordOperation{client} }

// Definition returns the generated deletion contract.
func (DeleteRecordOperation) Definition() sdkgo.MutationDefinition { return DeleteRecordDefinition }

// IdempotencyKey identifies repeated deletion of the same model row.
func (DeleteRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes the selected model row.
func (operation DeleteRecordOperation) Invoke(call sdkgo.Call, input DeleteRecordInput) sdkgo.MutationAttempt[DeletedRecord] {
	if !validOdooModel(input.Model) || input.ID <= 0 {
		return contactMutationBranch[DeletedRecord]("deleteRecord", DeleteRecordBranchDefect, sdkgo.FailureValidation, "model or ID is invalid")
	}
	body, _ := json.Marshal(map[string]any{"ids": []int64{input.ID}})
	return invokeContactBooleanMutation(call, operation.client, "deleteRecord", input.Model, "unlink", body, input.ID, DeleteRecordBranchDeleted, DeleteRecordBranchNotFound, DeleteRecordBranchProviderRejected, DeleteRecordBranchInvalidResponse, DeleteRecordBranchDefect, DeletedRecord{ID: input.ID})
}
