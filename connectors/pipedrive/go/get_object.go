// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive

import (
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetObjectInput identifies one record.
type GetObjectInput struct {
	// ObjectType is the record's object type.
	ObjectType ObjectType `json:"objectType"`
	// ObjectID is Pipedrive's positive decimal record ID.
	ObjectID string `json:"objectId"`
}

// GetObjectOperation implements the record read Query.
type GetObjectOperation struct{ client *Client }

var getBranches = operationBranches{
	notFound: GetObjectBranchNotFound, rejected: GetObjectBranchProviderRejected,
	invalidResponse: GetObjectBranchInvalidResponse, defect: GetObjectBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (GetObjectOperation) Definition() sdkgo.QueryDefinition { return GetObjectDefinition }

// Invoke reads one record with every standard and custom field.
func (operation GetObjectOperation) Invoke(call sdkgo.Call, input GetObjectInput) sdkgo.QueryAttempt[CRMObject] {
	const operationID = "getObject"
	if err := input.validate(); err != nil {
		return sdkgo.NewQueryBranch(GetObjectBranchDefect, CRMObject{}, providerFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := operation.client.startSession(call, operationID)
	if failure != nil {
		return queryAttemptForSession[CRMObject](failure, getBranches)
	}
	defer cancel()
	object, result := operation.client.readRecord(session, input.ObjectType, input.ObjectID)
	receipt := operation.client.receipt(call, result.response, input.ObjectID)
	if result.outcome != exchangeSucceeded {
		return queryAttemptForExchange[CRMObject](result, receipt, getBranches)
	}
	return sdkgo.NewQueryBranch(GetObjectBranchFound, object, nil, receipt)
}

func (input GetObjectInput) validate() error {
	if err := input.ObjectType.validate(); err != nil {
		return err
	}
	return validateRecordID("objectId", input.ObjectID)
}

func recordPath(objectType ObjectType, objectID string) string {
	return "/api/v2/" + string(objectType) + "/" + objectID
}

func collectionPath(objectType ObjectType) string {
	return "/api/v2/" + string(objectType)
}
