// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const deleteRecordOperationID = "deleteRecord"

// DeleteRecordInput identifies a Salesforce record to remove.
type DeleteRecordInput struct {
	// SObjectType is the object API name, such as Contact.
	SObjectType string `json:"sObjectType"`
	// RecordID is the 15- or 18-character Salesforce record ID.
	RecordID string `json:"recordId"`
}

// DeleteRecordOutput identifies the deleted record.
type DeleteRecordOutput struct {
	// ID is the deleted Salesforce record ID.
	ID string `json:"id,omitempty"`
	// ProviderErrors contains only safe Salesforce error codes and field names.
	ProviderErrors []ProviderError `json:"providerErrors,omitempty"`
}

// DeleteRecordOperation implements the deleteRecord connector operation.
type DeleteRecordOperation struct{ client *Client }

var deleteRecordBranches = operationBranches{
	operationID: deleteRecordOperationID, notFound: DeleteRecordBranchNotFound,
	requestRejected: DeleteRecordBranchRecordRejected, providerRejected: DeleteRecordBranchProviderRejected,
	defect: DeleteRecordBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (DeleteRecordOperation) Definition() sdkgo.MutationDefinition { return DeleteRecordDefinition }

// IdempotencyKey derives the stable receipt key from the connector call ID.
func (DeleteRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes one record. A repeated delete converges on absence.
func (operation DeleteRecordOperation) Invoke(call sdkgo.Call, input DeleteRecordInput) sdkgo.MutationAttempt[DeleteRecordOutput] {
	client := operation.client
	if !isAPIName(input.SObjectType) || !isRecordID(input.RecordID) {
		return sdkgo.NewMutationBranch(DeleteRecordBranchDefect, DeleteRecordOutput{}, salesforceFailurePointer(sdkgo.FailureValidation, deleteRecordOperationID, "sObjectType or recordId is invalid"), sdkgo.Receipt{})
	}
	credential, outcome := client.resolveCredential(call, deleteRecordBranches)
	if outcome != nil {
		return mutationAttemptFromOutcome(client, call, deleteRecordBranches, outcome, DeleteRecordOutput{})
	}
	response, outcome := client.send(call, &credential, deleteRecordBranches, salesforceRequest{
		method: http.MethodDelete, path: client.dataPath("sobjects", input.SObjectType, input.RecordID), responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return mutationAttemptFromOutcome(client, call, deleteRecordBranches, outcome, DeleteRecordOutput{ProviderErrors: outcome.providerErrors})
	}
	if response.status != http.StatusNoContent {
		return sdkgo.NewMutationBranch(DeleteRecordBranchDefect, DeleteRecordOutput{}, salesforceFailurePointer(sdkgo.FailureProtocol, deleteRecordOperationID, "Salesforce returned an unexpected delete response"), client.receipt(call, input.RecordID, response.apiUsage))
	}
	return sdkgo.NewMutationBranch(DeleteRecordBranchDeleted, DeleteRecordOutput{ID: input.RecordID}, nil, client.receipt(call, input.RecordID, response.apiUsage))
}
