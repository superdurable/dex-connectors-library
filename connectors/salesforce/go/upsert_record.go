// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	upsertRecordByExternalIDOperationID = "upsertRecordByExternalId"
	// maxExternalIDValueCharacters is Salesforce's longest Text external ID field.
	maxExternalIDValueCharacters = 255
)

// UpsertRecordByExternalIDInput identifies one record by an External ID field
// value and the fields to write to it.
//
// Salesforce creates the record when no record carries ExternalIDValue in
// ExternalIDField and updates it when exactly one does. Repeating the same
// input therefore converges on one record, which is what makes a retried Step
// safe; mark the field Unique as well as External ID so that concurrent
// attempts cannot create two records.
type UpsertRecordByExternalIDInput struct {
	// SObjectType is the object API name, such as Contact or Invoice__c.
	SObjectType string `json:"sObjectType"`
	// ExternalIDField is the API name of a field marked External ID, such as ERP_Id__c.
	ExternalIDField string `json:"externalIdField"`
	// ExternalIDValue is the correlation key from the other system, at most 255 characters.
	ExternalIDValue string `json:"externalIdValue"`
	// Fields maps field API names to exact JSON values, such as "LastName": "Raman".
	// It must not set Id or ExternalIDField, which the request path names.
	Fields map[string]json.RawMessage `json:"fields"`
}

// UpsertRecordByExternalIDOutput reports the upserted record or why Salesforce refused it.
type UpsertRecordByExternalIDOutput struct {
	// ID is the 18-character ID of the created or updated record on upserted.
	ID string `json:"id,omitempty"`
	// IsCreated reports that this request created the record. A retried Step
	// can report false after an earlier attempt created it.
	IsCreated bool `json:"isCreated,omitempty"`
	// MatchingRecordIDs lists the records that share the external ID value on multipleMatches.
	MatchingRecordIDs []string `json:"matchingRecordIds,omitempty"`
	// ProviderErrors lists Salesforce's error codes and fields on recordRejected and providerRejected.
	ProviderErrors []ProviderError `json:"providerErrors,omitempty"`
}

// UpsertRecordByExternalIDOperation implements the upsertRecordByExternalId connector operation.
type UpsertRecordByExternalIDOperation struct{ client *Client }

type upsertResponse struct {
	ID      string `json:"id"`
	Success bool   `json:"success"`
	Created *bool  `json:"created"`
}

// A 404 names an unknown sObject or external ID field; an oversized success is retried.
var upsertRecordByExternalIDBranches = operationBranches{
	operationID: upsertRecordByExternalIDOperationID, notFound: UpsertRecordByExternalIDBranchProviderRejected,
	requestRejected: UpsertRecordByExternalIDBranchRecordRejected, providerRejected: UpsertRecordByExternalIDBranchProviderRejected,
	defect: UpsertRecordByExternalIDBranchDefect, isDuplicateValueRetryable: true,
}

// Definition returns the immutable connector operation definition.
func (UpsertRecordByExternalIDOperation) Definition() sdkgo.MutationDefinition {
	return UpsertRecordByExternalIDDefinition
}

// IdempotencyKey derives the receipt key from the stable connector call ID.
// Salesforce deduplicates by the external ID value itself, so the key is not sent.
func (UpsertRecordByExternalIDOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertRecordByExternalIDInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one PATCH to /sobjects/{type}/{externalIdField}/{value} and
// classifies its attempt. Because repeating it is safe, a lost response,
// timeout, 5xx, unreadable success, or DUPLICATE_VALUE from a concurrent
// attempt returns Retry instead of an uncertain outcome.
func (operation UpsertRecordByExternalIDOperation) Invoke(
	call sdkgo.Call,
	input UpsertRecordByExternalIDInput,
) sdkgo.MutationAttempt[UpsertRecordByExternalIDOutput] {
	client := operation.client
	branches := upsertRecordByExternalIDBranches
	body, err := validateUpsertRecordByExternalIDInput(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpsertRecordByExternalIDBranchDefect, UpsertRecordByExternalIDOutput{}, salesforceFailurePointer(sdkgo.FailureValidation, branches.operationID, err.Error()), sdkgo.Receipt{})
	}
	credential, outcome := client.resolveCredential(call, branches)
	if outcome != nil {
		return mutationAttemptFromOutcome(client, call, branches, outcome, UpsertRecordByExternalIDOutput{})
	}
	response, outcome := client.send(call, &credential, branches, salesforceRequest{
		method: http.MethodPatch, path: client.dataPath("sobjects", input.SObjectType, input.ExternalIDField, input.ExternalIDValue),
		body: body, responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return mutationAttemptFromOutcome(client, call, branches, outcome, UpsertRecordByExternalIDOutput{ProviderErrors: outcome.providerErrors})
	}
	receipt := client.receipt(call, "", response.apiUsage)
	if response.status == http.StatusMultipleChoices {
		failure := salesforceFailure(sdkgo.FailureConflict, branches.operationID, "more than one record carries the external ID value")
		return sdkgo.NewMutationBranch(UpsertRecordByExternalIDBranchMultipleMatches, UpsertRecordByExternalIDOutput{
			MatchingRecordIDs: parseMatchingRecordIDs(response.body),
		}, &failure, receipt)
	}
	var decoded upsertResponse
	if err := json.Unmarshal(response.body, &decoded); err != nil || !decoded.Success || decoded.Created == nil || !isRecordID(decoded.ID) {
		return sdkgo.NewMutationRetry[UpsertRecordByExternalIDOutput](salesforceFailure(sdkgo.FailureProtocol, branches.operationID, "Salesforce returned an unreadable upsert result"), 0)
	}
	receipt.ProviderObjectID = decoded.ID
	return sdkgo.NewMutationBranch(UpsertRecordByExternalIDBranchUpserted, UpsertRecordByExternalIDOutput{ID: decoded.ID, IsCreated: *decoded.Created}, nil, receipt)
}

// validateUpsertRecordByExternalIDInput returns the JSON body for valid input.
func validateUpsertRecordByExternalIDInput(input UpsertRecordByExternalIDInput) ([]byte, error) {
	if !isAPIName(input.SObjectType) {
		return nil, errors.New("sObjectType must be an object API name such as Contact")
	}
	if !isFieldAPIName(input.ExternalIDField) || strings.EqualFold(input.ExternalIDField, "Id") {
		return nil, errors.New("externalIdField must be the API name of an External ID field such as ERP_Id__c")
	}
	if !isExternalIDValue(input.ExternalIDValue) {
		return nil, errors.New("externalIdValue must be 1 to 255 characters without surrounding spaces or control characters")
	}
	return encodeRecordFields(input.Fields, input.ExternalIDField)
}

func isExternalIDValue(value string) bool {
	if value == "" || strings.TrimSpace(value) != value || !utf8.ValidString(value) ||
		utf8.RuneCountInString(value) > maxExternalIDValueCharacters || value == "." || value == ".." {
		return false
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return false
		}
	}
	return true
}

// parseMatchingRecordIDs reads the record URLs Salesforce lists in a 300 upsert response.
func parseMatchingRecordIDs(body []byte) []string {
	var recordURLs []string
	if json.Unmarshal(body, &recordURLs) != nil {
		return nil
	}
	recordIDs := make([]string, 0, len(recordURLs))
	for _, recordURL := range recordURLs {
		if recordID := path.Base(recordURL); isRecordID(recordID) {
			recordIDs = append(recordIDs, recordID)
		}
	}
	return recordIDs
}
