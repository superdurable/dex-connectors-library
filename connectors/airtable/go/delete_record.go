// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// DeleteRecordInput identifies the record whose absence is requested.
type DeleteRecordInput struct {
	// BaseID is the Airtable base ID.
	BaseID string `json:"baseId"`
	// TableIDOrName identifies the table, preferably by stable ID.
	TableIDOrName string `json:"tableIdOrName"`
	// RecordID is the Airtable record ID.
	RecordID string `json:"recordId"`
}

// DeletedRecord confirms which record was removed.
type DeletedRecord struct {
	// RecordID is the requested record ID.
	RecordID string `json:"recordId"`
}

// DeleteRecordOperation implements the deleteRecord Mutation.
type DeleteRecordOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (DeleteRecordOperation) Definition() sdkgo.MutationDefinition { return DeleteRecordDefinition }

// IdempotencyKey correlates retries; Airtable does not accept the key.
func (DeleteRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes a record; repeating the request converges on absence.
func (operation DeleteRecordOperation) Invoke(call sdkgo.Call, input DeleteRecordInput) sdkgo.MutationAttempt[DeletedRecord] {
	if err := validateBaseAndTable(input.BaseID, input.TableIDOrName); err != nil {
		return sdkgo.NewMutationBranch(DeleteRecordBranchDefect, DeletedRecord{}, providerFailurePointer("deleteRecord", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	if !recordIDPattern.MatchString(input.RecordID) {
		return sdkgo.NewMutationBranch(DeleteRecordBranchDefect, DeletedRecord{}, providerFailurePointer("deleteRecord", sdkgo.FailureValidation, "recordId must be an Airtable record ID"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "deleteRecord", providerRequest{
		method: http.MethodDelete, path: recordPath(input.BaseID, input.TableIDOrName) + "/" + url.PathEscape(input.RecordID), baseID: input.BaseID,
	}, input.RecordID)
	if outcome != nil {
		if outcome.disposition == outcomeNotFound {
			return sdkgo.NewMutationBranch(DeleteRecordBranchAlreadyAbsent, DeletedRecord{RecordID: input.RecordID}, &outcome.failure, outcome.receipt)
		}
		return mutationAttemptForOutcome[DeletedRecord](*outcome, operationBranches{rejected: DeleteRecordBranchProviderRejected, invalidResponse: DeleteRecordBranchInvalidResponse, defect: DeleteRecordBranchDefect})
	}
	var deleted struct {
		ID      string `json:"id"`
		Deleted bool   `json:"deleted"`
	}
	if err := decodeResponse(response.body, &deleted); err != nil || deleted.ID != input.RecordID || !deleted.Deleted {
		return sdkgo.NewMutationBranch(DeleteRecordBranchInvalidResponse, DeletedRecord{}, providerFailurePointer("deleteRecord", sdkgo.FailureProtocol, errors.New("Airtable returned an invalid deletion confirmation").Error()), operation.client.receipt(call, input.RecordID))
	}
	return sdkgo.NewMutationBranch(DeleteRecordBranchDeleted, DeletedRecord{RecordID: input.RecordID}, nil, operation.client.receipt(call, input.RecordID))
}
