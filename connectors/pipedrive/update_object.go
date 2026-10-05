// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive

import (
	"encoding/json"
	"errors"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// UpdateObjectInput sets named fields on one record. Fields that are not named keep their values.
type UpdateObjectInput struct {
	// ObjectType is the record's object type.
	ObjectType ObjectType `json:"objectType"`
	// ObjectID is Pipedrive's positive decimal record ID.
	ObjectID string `json:"objectId"`
	// Fields maps standard field names, such as stage_id or owner_id, to JSON
	// values; JSON null clears a field that Pipedrive allows to be empty.
	Fields map[string]json.RawMessage `json:"fields,omitempty"`
	// CustomFields maps 40-character custom field keys to JSON values; JSON null clears a value.
	CustomFields map[string]json.RawMessage `json:"customFields,omitempty"`
}

// UpdateObjectOperation implements the field update Mutation.
type UpdateObjectOperation struct{ client *Client }

var updateBranches = operationBranches{
	notFound: UpdateObjectBranchNotFound, rejected: UpdateObjectBranchProviderRejected,
	invalidResponse: UpdateObjectBranchInvalidResponse, defect: UpdateObjectBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (UpdateObjectOperation) Definition() sdkgo.MutationDefinition { return UpdateObjectDefinition }

// IdempotencyKey derives the receipt key from the stable Call ID. Pipedrive has
// no idempotency key; setting the same values again leaves the record unchanged.
func (UpdateObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateObjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sets the named fields with PATCH. Repeating the same update converges
// on the same values, so an unconfirmed outcome is retried.
func (operation UpdateObjectOperation) Invoke(call sdkgo.Call, input UpdateObjectInput) sdkgo.MutationAttempt[CRMObject] {
	const operationID = "updateObject"
	body, err := input.updateRequestBody()
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateObjectBranchDefect, CRMObject{}, providerFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := operation.client.startSession(call, operationID)
	if failure != nil {
		return mutationAttemptForSession[CRMObject](failure, updateBranches)
	}
	defer cancel()
	object, result := operation.client.patchRecord(session, input.ObjectType, input.ObjectID, body)
	receipt := operation.client.receipt(call, result.response, input.ObjectID)
	if result.outcome != exchangeSucceeded {
		return repeatableMutationAttemptForExchange[CRMObject](result, receipt, updateBranches)
	}
	return sdkgo.NewMutationBranch(UpdateObjectBranchUpdated, object, nil, receipt)
}

func (input UpdateObjectInput) updateRequestBody() ([]byte, error) {
	if err := input.ObjectType.validate(); err != nil {
		return nil, err
	}
	if err := validateRecordID("objectId", input.ObjectID); err != nil {
		return nil, err
	}
	if len(input.Fields)+len(input.CustomFields) == 0 {
		return nil, errors.New("at least one field or custom field value is required")
	}
	if err := validateWrittenFields(input.Fields, input.CustomFields); err != nil {
		return nil, err
	}
	return encodeWriteBody(input.Fields, input.CustomFields)
}
