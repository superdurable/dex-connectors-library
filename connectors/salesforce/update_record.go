// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const updateRecordOperationID = "updateRecord"

// UpdateRecordInput identifies one record and the fields to change.
type UpdateRecordInput struct {
	// SObjectType is the record's object API name, such as Opportunity.
	SObjectType string `json:"sObjectType"`
	// RecordID is the 15- or 18-character Salesforce record ID.
	RecordID string `json:"recordId"`
	// Fields maps at least one field API name to its exact JSON value, such as
	// "StageName": "Closed Won"; JSON null clears a field. Fields that are not
	// listed keep their values.
	Fields map[string]json.RawMessage `json:"fields"`
}

// UpdateRecordOutput reports the updated record or why Salesforce refused the change.
type UpdateRecordOutput struct {
	// ID is the updated record's ID on updated, as given in the input.
	ID string `json:"id,omitempty"`
	// ProviderErrors lists Salesforce's error codes and fields on recordRejected and providerRejected.
	ProviderErrors []ProviderError `json:"providerErrors,omitempty"`
}

// UpdateRecordOperation implements the updateRecord connector operation.
type UpdateRecordOperation struct{ client *Client }

var updateRecordBranches = operationBranches{
	operationID: updateRecordOperationID, notFound: UpdateRecordBranchNotFound,
	requestRejected: UpdateRecordBranchRecordRejected, providerRejected: UpdateRecordBranchProviderRejected,
	defect: UpdateRecordBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (UpdateRecordOperation) Definition() sdkgo.MutationDefinition { return UpdateRecordDefinition }

// IdempotencyKey derives the receipt key from the stable connector call ID.
// Setting the same field values again converges on the same record, so the key is not sent.
func (UpdateRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one PATCH to /sobjects/{type}/{id} and classifies its attempt.
// A lost response, timeout, or 5xx returns Retry, because setting the same
// values again leaves the record in the same state.
func (operation UpdateRecordOperation) Invoke(call sdkgo.Call, input UpdateRecordInput) sdkgo.MutationAttempt[UpdateRecordOutput] {
	client := operation.client
	body, err := validateUpdateRecordInput(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateRecordBranchDefect, UpdateRecordOutput{}, salesforceFailurePointer(sdkgo.FailureValidation, updateRecordOperationID, err.Error()), sdkgo.Receipt{})
	}
	credential, outcome := client.resolveCredential(call, updateRecordBranches)
	if outcome != nil {
		return mutationAttemptFromOutcome(client, call, updateRecordBranches, outcome, UpdateRecordOutput{})
	}
	response, outcome := client.send(call, &credential, updateRecordBranches, salesforceRequest{
		method: http.MethodPatch, path: client.dataPath("sobjects", input.SObjectType, input.RecordID),
		body: body, responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return mutationAttemptFromOutcome(client, call, updateRecordBranches, outcome, UpdateRecordOutput{ProviderErrors: outcome.providerErrors})
	}
	if response.status == http.StatusMultipleChoices {
		// A 300 applies no change; it is only defined for external ID paths.
		return sdkgo.NewMutationBranch(UpdateRecordBranchProviderRejected, UpdateRecordOutput{}, salesforceFailurePointer(sdkgo.FailureProtocol, updateRecordOperationID, "Salesforce returned multiple choices for a record ID"), client.receipt(call, input.RecordID, response.apiUsage))
	}
	return sdkgo.NewMutationBranch(UpdateRecordBranchUpdated, UpdateRecordOutput{ID: input.RecordID}, nil, client.receipt(call, input.RecordID, response.apiUsage))
}

// validateUpdateRecordInput returns the JSON body for valid input.
func validateUpdateRecordInput(input UpdateRecordInput) ([]byte, error) {
	if !isAPIName(input.SObjectType) {
		return nil, errors.New("sObjectType must be an object API name such as Opportunity")
	}
	if !isRecordID(input.RecordID) {
		return nil, errors.New("recordId must be a 15- or 18-character Salesforce record ID")
	}
	if len(input.Fields) == 0 {
		return nil, errors.New("fields must set at least one field")
	}
	return encodeRecordFields(input.Fields)
}
