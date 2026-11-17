// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const createRecordOperationID = "createRecord"

// CreateRecordInput provides the object and field values for one new record.
type CreateRecordInput struct {
	// SObjectType is the object API name, such as Contact.
	SObjectType string `json:"sObjectType"`
	// Fields maps field API names to exact JSON values.
	Fields map[string]json.RawMessage `json:"fields"`
}

// CreateRecordOutput identifies the created Salesforce record.
type CreateRecordOutput struct {
	// ID is the record ID returned by Salesforce.
	ID string `json:"id,omitempty"`
	// ProviderErrors contains only safe Salesforce error codes and field names.
	ProviderErrors []ProviderError `json:"providerErrors,omitempty"`
}

// CreateRecordOperation implements the createRecord connector operation.
type CreateRecordOperation struct{ client *Client }

var createRecordBranches = operationBranches{
	operationID:     createRecordOperationID,
	requestRejected: CreateRecordBranchRecordRejected, providerRejected: CreateRecordBranchProviderRejected,
	defect: CreateRecordBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (CreateRecordOperation) Definition() sdkgo.MutationDefinition { return CreateRecordDefinition }

// IdempotencyKey derives the stable receipt key from the connector call ID.
func (CreateRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke bounds possible creates to the Step's send cap and reads back a saved record ID.
func (operation CreateRecordOperation) Invoke(call sdkgo.Call, input CreateRecordInput) sdkgo.MutationAttempt[CreateRecordOutput] {
	client := operation.client
	if !isAPIName(input.SObjectType) || len(input.Fields) == 0 {
		return sdkgo.NewMutationBranch(CreateRecordBranchDefect, CreateRecordOutput{}, salesforceFailurePointer(sdkgo.FailureValidation, createRecordOperationID, "sObjectType and nonempty fields are required"), sdkgo.Receipt{})
	}
	body, err := encodeRecordFields(input.Fields)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateRecordBranchDefect, CreateRecordOutput{}, salesforceFailurePointer(sdkgo.FailureValidation, createRecordOperationID, err.Error()), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateRecordDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateRecordBranchDefect, CreateRecordOutput{}, salesforceFailurePointer(sdkgo.FailureLocalDefect, createRecordOperationID, "Salesforce dispatch fence could not be built"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return client.uncertainCreate(call, "Salesforce create checkpoint could not be trusted")
	}
	credential, outcome := client.resolveCredential(call, createRecordBranches)
	if outcome != nil {
		if decision.MaximumPriorSends > 0 {
			return client.uncertainCreate(call, "an earlier Salesforce create may have succeeded; credentials are unavailable")
		}
		if decision.Kind == singledispatch.SendAllowed && fence.RecordNotSent() != nil {
			return sdkgo.NewMutationRetry[CreateRecordOutput](salesforceFailure(sdkgo.FailureAvailability, createRecordOperationID, "Salesforce no-send checkpoint could not be recorded"), 0)
		}
		return mutationAttemptFromOutcome(client, call, createRecordBranches, outcome, CreateRecordOutput{})
	}
	if decision.Marker.ProviderID != "" {
		return client.readBackCreatedRecord(call, &credential, input.SObjectType, decision.Marker.ProviderID)
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return client.uncertainCreate(call, "an earlier Salesforce create may have succeeded")
	}
	var response salesforceResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		var sendErr error
		response, sendErr = client.sendRequest(requestContext, call, &credential, salesforceRequest{
			method: http.MethodPost, path: client.dataPath("sobjects", input.SObjectType), body: body,
			responseLimit: client.maxResponseBytes,
		})
		return sendErr
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return sdkgo.NewMutationRetry[CreateRecordOutput](salesforceFailure(sdkgo.FailureAvailability, createRecordOperationID, "Salesforce create was not sent before its deadline"), 0)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[CreateRecordOutput](salesforceFailure(sdkgo.FailureAvailability, createRecordOperationID, "Salesforce resend checkpoint could not be recorded"), 0)
		}
		outcome := client.classifyFailedResponse(createRecordBranches, response)
		return sdkgo.NewMutationRetry[CreateRecordOutput](outcome.failure, outcome.retryAfter)
	}
	if err != nil || response.status >= 500 {
		if decision.MaximumPriorSends+1 < call.MaxSends() {
			return sdkgo.NewMutationRetry[CreateRecordOutput](salesforceFailure(sdkgo.FailureTransport, createRecordOperationID, "Salesforce create outcome is unknown"), 0)
		}
		return client.uncertainCreate(call, "Salesforce create outcome could not be confirmed after bounded sends")
	}
	if response.status < 200 || response.status >= 300 {
		outcome := client.classifyFailedResponse(createRecordBranches, response)
		if decision.MaximumPriorSends > 0 {
			return client.uncertainCreate(call, "an earlier Salesforce create may have succeeded")
		}
		return mutationAttemptFromOutcome(client, call, createRecordBranches, &outcome, CreateRecordOutput{ProviderErrors: outcome.providerErrors})
	}
	var created struct {
		ID      string `json:"id"`
		Success bool   `json:"success"`
	}
	if response.status != http.StatusCreated || json.Unmarshal(response.body, &created) != nil || !created.Success || !isRecordID(created.ID) {
		return client.uncertainCreate(call, "Salesforce returned an invalid create response")
	}
	if fence.RecordProviderID(created.ID) != nil {
		return sdkgo.NewMutationRetry[CreateRecordOutput](salesforceFailure(sdkgo.FailureAvailability, createRecordOperationID, "Salesforce created a record but its checkpoint was unavailable"), 0)
	}
	return sdkgo.NewMutationBranch(CreateRecordBranchCreated, CreateRecordOutput{ID: created.ID}, nil, client.receipt(call, created.ID, response.apiUsage))
}

func (client *Client) readBackCreatedRecord(call sdkgo.Call, credential *Credentials, sObjectType string, recordID string) sdkgo.MutationAttempt[CreateRecordOutput] {
	response, err := client.sendRequest(call.Context, call, credential, salesforceRequest{
		method: http.MethodGet, path: client.dataPath("sobjects", sObjectType, recordID),
		query: map[string][]string{"fields": {"Id"}}, responseLimit: client.maxResponseBytes,
	})
	if err != nil || response.status != http.StatusOK {
		return client.uncertainCreate(call, "Salesforce could not confirm the previously created record")
	}
	record, decodeErr := decodeRecord(response.body)
	if decodeErr != nil || !isSameRecordID(record.ID, recordID) {
		return client.uncertainCreate(call, "Salesforce returned an invalid created record")
	}
	return sdkgo.NewMutationBranch(CreateRecordBranchCreated, CreateRecordOutput{ID: recordID}, nil, client.receipt(call, recordID, response.apiUsage))
}

func (client *Client) uncertainCreate(call sdkgo.Call, message string) sdkgo.MutationAttempt[CreateRecordOutput] {
	return sdkgo.NewMutationUncertain(CreateRecordOutput{}, salesforceFailure(sdkgo.FailureTransport, createRecordOperationID, message), client.receipt(call, "", ""))
}
