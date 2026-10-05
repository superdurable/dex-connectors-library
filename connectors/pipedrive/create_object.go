// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive

import (
	"encoding/json"
	"net/http"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// CreateObjectInput is one new record. Fields use Pipedrive API v2 field
// names, such as title, person_id, org_id, stage_id, owner_id, and value for a
// deal, or name, org_id, and emails for a person; each value is exact JSON.
type CreateObjectInput struct {
	// ObjectType is the type of record to create.
	ObjectType ObjectType `json:"objectType"`
	// Fields maps standard field names to JSON values. A deal requires title;
	// a person or organization requires name.
	Fields map[string]json.RawMessage `json:"fields"`
	// CustomFields maps custom field keys, the 40-character keys shown in
	// Company settings > Data fields, to JSON values.
	CustomFields map[string]json.RawMessage `json:"customFields,omitempty"`
}

// CreateObjectOperation implements the single-dispatch create Mutation.
type CreateObjectOperation struct{ client *Client }

var createBranches = operationBranches{rejected: CreateObjectBranchProviderRejected, defect: CreateObjectBranchDefect}

// Definition returns the immutable connector operation definition.
func (CreateObjectOperation) Definition() sdkgo.MutationDefinition { return CreateObjectDefinition }

// IdempotencyKey uses the stable connector Call ID. Pipedrive documents no
// idempotency key, so the key only correlates the Receipt; a Dex heartbeat
// checkpoint narrows duplicate sends instead.
func (CreateObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateObjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one create at most once per Step execution, except that a
// Worker lost before Dex stored the dispatch checkpoint can send it twice. Only
// a burst-limit 429 or a connection that never opened is retried; any other
// unconfirmed outcome selects uncertain without sending the create again.
func (operation CreateObjectOperation) Invoke(call sdkgo.Call, input CreateObjectInput) sdkgo.MutationAttempt[CRMObject] {
	const operationID = "createObject"
	body, err := input.createRequestBody()
	if err != nil {
		return sdkgo.NewMutationBranch(CreateObjectBranchDefect, CRMObject{}, providerFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	// The checkpoint is read before credentials, so a later credential failure cannot report a sent create as rejected.
	if hasEarlierDispatch(call) {
		return sdkgo.NewMutationUncertain(CRMObject{}, providerFailure(operationID, sdkgo.FailureTransport,
			"an earlier attempt of this Step may have sent the create, so it is not sent again"), operation.client.receipt(call, providerResponse{}, ""))
	}
	session, cancel, failure := operation.client.startSession(call, operationID)
	if failure != nil {
		return mutationAttemptForSession[CRMObject](failure, createBranches)
	}
	defer cancel()
	if err := recordDispatch(call); err != nil {
		return dispatchCheckpointRetry[CRMObject](operationID)
	}
	result := operation.client.exchange(session, providerRequest{method: http.MethodPost, path: collectionPath(input.ObjectType), body: body})
	receipt := operation.client.receipt(call, result.response, "")
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeNotSent, exchangeRateLimited:
		releaseDispatch(call)
		return sdkgo.NewMutationRetry[CRMObject](result.failure, result.retryAfter)
	case exchangeUnavailable, exchangeInvalid:
		return sdkgo.NewMutationUncertain(CRMObject{}, result.failure, receipt)
	case exchangeDefect:
		releaseDispatch(call)
		return sdkgo.NewMutationBranch(CreateObjectBranchDefect, CRMObject{}, &result.failure, receipt)
	default:
		return sdkgo.NewMutationBranch(CreateObjectBranchProviderRejected, CRMObject{}, &result.failure, receipt)
	}
	object, err := decodeRecordBody(input.ObjectType, result.response.body)
	if err != nil {
		return sdkgo.NewMutationUncertain(CRMObject{}, providerFailure(operationID, sdkgo.FailureProtocol,
			"Pipedrive accepted the create but returned an invalid record"), receipt)
	}
	receipt.ProviderObjectID = object.ID
	return sdkgo.NewMutationBranch(CreateObjectBranchCreated, object, nil, receipt)
}

func (input CreateObjectInput) createRequestBody() ([]byte, error) {
	if err := input.ObjectType.validate(); err != nil {
		return nil, err
	}
	if err := validateWrittenFields(input.Fields, input.CustomFields); err != nil {
		return nil, err
	}
	if err := requireNameField(input.ObjectType, input.Fields); err != nil {
		return nil, err
	}
	return encodeWriteBody(input.Fields, input.CustomFields)
}
