// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const updateRecordOperation = "updateRecord"

// UpdateRecordInput sets named fields of one record; every other field keeps its value.
type UpdateRecordInput struct {
	// Module is the module API name, such as Deals.
	Module string `json:"module"`
	// RecordID is the record ID, such as 4150868000000376008.
	RecordID string `json:"recordId"`
	// Fields are 1 to 200 field values by API name, such as Stage and Owner; JSON null clears a
	// field, and lookups are written as {"id": "<record ID>"} with LookupFieldValue.
	Fields map[string]json.RawMessage `json:"fields"`
	// Triggers limits the automations Zoho CRM runs after the write; empty keeps Zoho CRM's default,
	// which runs workflow rules, approvals, and blueprints.
	Triggers []RecordTrigger `json:"triggers,omitempty"`
	// ShouldSkipAutomation runs no workflow rule, approval, or blueprint after the write.
	ShouldSkipAutomation bool `json:"shouldSkipAutomation,omitempty"`
}

// UpdateRecordOutput is the result of the update.
type UpdateRecordOutput struct {
	// Module is the module API name.
	Module string `json:"module"`
	// ID is the updated record's ID.
	ID string `json:"id,omitempty"`
	// ModifiedAt is the record's modification time after the write.
	ModifiedAt time.Time `json:"modifiedAt,omitzero"`
	// ProviderErrors holds Zoho CRM's codes and field names on conflict and recordRejected.
	ProviderErrors []ProviderError `json:"providerErrors,omitempty"`
}

// UpdateRecordOperation is the updateRecord Mutation.
type UpdateRecordOperation struct {
	client *Client
}

type updateRequestWire struct {
	Data    []map[string]json.RawMessage `json:"data"`
	Trigger *[]RecordTrigger             `json:"trigger,omitempty"`
}

// Definition returns the immutable connector operation definition.
func (UpdateRecordOperation) Definition() sdkgo.MutationDefinition { return UpdateRecordDefinition }

// IdempotencyKey uses the stable connector Call ID. Zoho CRM documents no idempotency key; the
// update sets absolute values, so a repeat is safe and the key only correlates the Receipt.
func (UpdateRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends PUT /crm/v8/{module}/{recordId} with the fields. Repeating it sets the same values
// again, so every unconfirmed outcome is retried; Zoho CRM runs the selected automations again on
// each repeat, and a change another user made in between to the same fields is overwritten.
func (operation UpdateRecordOperation) Invoke(call sdkgo.Call, input UpdateRecordInput) sdkgo.MutationAttempt[UpdateRecordOutput] {
	branches := mutationBranches{
		notFound: UpdateRecordBranchNotFound, conflict: UpdateRecordBranchConflict, recordRejected: UpdateRecordBranchRecordRejected,
		providerRejected: UpdateRecordBranchProviderRejected, invalidResponse: UpdateRecordBranchInvalidResponse, defect: UpdateRecordBranchDefect,
	}
	request, err := buildUpdateRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateRecordBranchDefect, UpdateRecordOutput{}, crmFailurePointer(sdkgo.FailureValidation, updateRecordOperation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := operation.client.startSession(call, updateRecordOperation)
	if failure != nil {
		return mutationAttemptForSession[UpdateRecordOutput](failure, branches)
	}
	defer cancel()
	result, written := operation.client.exchangeWrite(session, updateRecordOperation, crmRequest{
		method: http.MethodPut, path: recordPath(input.Module, input.RecordID), payload: request,
	})
	receipt := operation.client.receipt(call, result.response, input.RecordID)
	rejected := UpdateRecordOutput{Module: input.Module, ProviderErrors: result.summary.providerErrors()}
	if attempt, isTerminal := mutationAttemptForExchange(result, receipt, rejected, branches); isTerminal {
		return attempt
	}
	if written.recordID != input.RecordID {
		return sdkgo.NewMutationBranch(UpdateRecordBranchInvalidResponse, UpdateRecordOutput{Module: input.Module},
			crmFailurePointer(sdkgo.FailureProtocol, updateRecordOperation, "Zoho CRM reported a write to a record other than the one requested"), receipt)
	}
	return sdkgo.NewMutationBranch(UpdateRecordBranchUpdated, UpdateRecordOutput{Module: input.Module, ID: written.recordID, ModifiedAt: written.modifiedAt}, nil, receipt)
}

func buildUpdateRequest(input UpdateRecordInput) (updateRequestWire, error) {
	if err := validateModule(input.Module); err != nil {
		return updateRequestWire{}, err
	}
	if err := validateRecordID(input.RecordID); err != nil {
		return updateRequestWire{}, err
	}
	if err := validateWriteFields(input.Fields); err != nil {
		return updateRequestWire{}, err
	}
	trigger, err := renderTriggers(input.Triggers, input.ShouldSkipAutomation)
	if err != nil {
		return updateRequestWire{}, err
	}
	return updateRequestWire{Data: []map[string]json.RawMessage{input.Fields}, Trigger: trigger}, nil
}
