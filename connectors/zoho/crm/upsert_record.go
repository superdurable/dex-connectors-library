// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	upsertRecordOperation = "upsertRecord"
	// MaxDuplicateCheckFields is the most duplicate-check fields one upsert names.
	MaxDuplicateCheckFields = 3
)

// UpsertRecordInput creates one record, or updates the record whose duplicate-check field already
// holds the value Fields gives it.
type UpsertRecordInput struct {
	// Module is the module API name, such as Contacts.
	Module string `json:"module"`
	// DuplicateCheckFields are 1 to 3 field API names Zoho CRM checks in order, such as Email for
	// Contacts, Account_Name for Accounts, or a custom field marked Do not allow duplicate values.
	// Each must be set in Fields to a non-empty value, so a repeated attempt finds the record.
	DuplicateCheckFields []string `json:"duplicateCheckFields"`
	// Fields are 1 to 200 field values by API name, such as {"Email": "\"jane@acme.example.com\""};
	// lookups are written as {"id": "<record ID>"} with LookupFieldValue. The record ID cannot be set.
	Fields map[string]json.RawMessage `json:"fields"`
	// Triggers limits the automations Zoho CRM runs after the write; empty keeps Zoho CRM's default,
	// which runs workflow rules, approvals, and blueprints.
	Triggers []RecordTrigger `json:"triggers,omitempty"`
	// ShouldSkipAutomation runs no workflow rule, approval, or blueprint after the write.
	ShouldSkipAutomation bool `json:"shouldSkipAutomation,omitempty"`
}

// UpsertRecordOutput is the record Zoho CRM created or updated.
type UpsertRecordOutput struct {
	// Module is the module API name.
	Module string `json:"module"`
	// ID is the record ID.
	ID string `json:"id,omitempty"`
	// IsCreated reports that the answered request inserted the record. After a retry it can be
	// false even though an earlier attempt of the same Step created the record.
	IsCreated bool `json:"isCreated,omitempty"`
	// DuplicateField is the duplicate-check field that matched an existing record, or empty after an insert.
	DuplicateField string `json:"duplicateField,omitempty"`
	// CreatedAt is the record's creation time.
	CreatedAt time.Time `json:"createdAt,omitzero"`
	// ModifiedAt is the record's modification time after the write.
	ModifiedAt time.Time `json:"modifiedAt,omitzero"`
	// ProviderErrors holds Zoho CRM's codes and field names on conflict and recordRejected.
	ProviderErrors []ProviderError `json:"providerErrors,omitempty"`
}

// UpsertRecordOperation is the upsertRecord Mutation.
type UpsertRecordOperation struct {
	client *Client
}

type upsertRequestWire struct {
	Data                 []map[string]json.RawMessage `json:"data"`
	DuplicateCheckFields []string                     `json:"duplicate_check_fields"`
	Trigger              *[]RecordTrigger             `json:"trigger,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (UpsertRecordOperation) Definition() sdkgo.MutationDefinition { return UpsertRecordDefinition }

// IdempotencyKey uses the stable connector Call ID. Zoho CRM documents no idempotency key; the
// duplicate-check value makes a repeated upsert update the record instead, so the key only
// correlates the Receipt.
func (UpsertRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /crm/v8/{module}/upsert with one record. A DUPLICATE_DATA answer is sent once
// more, because a concurrent attempt of the same Step may have inserted the record first and the
// resend then updates it; a second DUPLICATE_DATA selects conflict. Every unconfirmed outcome is
// retried, because a repeated upsert converges on one record.
func (operation UpsertRecordOperation) Invoke(call sdkgo.Call, input UpsertRecordInput) sdkgo.MutationAttempt[UpsertRecordOutput] {
	branches := mutationBranches{
		conflict: UpsertRecordBranchConflict, recordRejected: UpsertRecordBranchRecordRejected, providerRejected: UpsertRecordBranchProviderRejected,
		invalidResponse: UpsertRecordBranchInvalidResponse, defect: UpsertRecordBranchDefect,
	}
	request, err := buildUpsertRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpsertRecordBranchDefect, UpsertRecordOutput{}, crmFailurePointer(sdkgo.FailureValidation, upsertRecordOperation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := operation.client.startSession(call, upsertRecordOperation)
	if failure != nil {
		return mutationAttemptForSession[UpsertRecordOutput](failure, branches)
	}
	defer cancel()
	for send := 0; ; send++ {
		result, written := operation.client.exchangeWrite(session, upsertRecordOperation, crmRequest{
			method: http.MethodPost, path: modulePath(input.Module) + "/upsert", payload: request,
		})
		if result.outcome == exchangeRejected && result.summary.code == errorCodeDuplicateData && send == 0 {
			continue
		}
		receipt := operation.client.receipt(call, result.response, written.recordID)
		rejected := UpsertRecordOutput{Module: input.Module, ProviderErrors: result.summary.providerErrors()}
		if attempt, isTerminal := mutationAttemptForExchange(result, receipt, rejected, branches); isTerminal {
			return attempt
		}
		return sdkgo.NewMutationBranch(UpsertRecordBranchUpserted, UpsertRecordOutput{
			Module: input.Module, ID: written.recordID, IsCreated: written.action == "insert", DuplicateField: written.duplicateField,
			CreatedAt: written.createdAt, ModifiedAt: written.modifiedAt,
		}, nil, receipt)
	}
}

// buildUpsertRequest validates input and returns the request body; a duplicate-check value must be present.
func buildUpsertRequest(input UpsertRecordInput) (upsertRequestWire, error) {
	if err := validateModule(input.Module); err != nil {
		return upsertRequestWire{}, err
	}
	if err := validateWriteFields(input.Fields); err != nil {
		return upsertRequestWire{}, err
	}
	if len(input.DuplicateCheckFields) == 0 || len(input.DuplicateCheckFields) > MaxDuplicateCheckFields {
		return upsertRequestWire{}, fmt.Errorf("duplicateCheckFields must name 1 to %d fields", MaxDuplicateCheckFields)
	}
	for index, fieldName := range input.DuplicateCheckFields {
		if !isFieldAPIName(fieldName) || slices.Contains(input.DuplicateCheckFields[:index], fieldName) {
			return upsertRequestWire{}, errors.New("duplicateCheckFields must be distinct Zoho CRM field API names such as Email")
		}
		if !hasNonEmptyValue(input.Fields[fieldName]) {
			return upsertRequestWire{}, fmt.Errorf("fields must set duplicate-check field %s to a non-empty value, or every attempt would insert a new record", fieldName)
		}
	}
	trigger, err := renderTriggers(input.Triggers, input.ShouldSkipAutomation)
	if err != nil {
		return upsertRequestWire{}, err
	}
	return upsertRequestWire{
		Data: []map[string]json.RawMessage{input.Fields}, DuplicateCheckFields: slices.Clone(input.DuplicateCheckFields), Trigger: trigger,
	}, nil
}

// hasNonEmptyValue rejects an absent value, null, an empty string, and an empty list or object.
func hasNonEmptyValue(value json.RawMessage) bool {
	trimmed := bytes.TrimSpace(value)
	switch string(trimmed) {
	case "", "null", `""`, "[]", "{}":
		return false
	}
	var text string
	if json.Unmarshal(trimmed, &text) == nil {
		return len(bytes.TrimSpace([]byte(text))) != 0
	}
	return true
}
