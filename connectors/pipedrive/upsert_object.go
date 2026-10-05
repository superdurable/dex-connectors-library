// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package pipedrive

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// IdentityPropertyEmail identifies a person by one of its email addresses.
	IdentityPropertyEmail = "email"
	// IdentityPropertyName identifies an organization by its exact name.
	IdentityPropertyName = "name"
	// maximumIdentityMatches is the search page that must hold every exact match of an identity value.
	maximumIdentityMatches = 50
	maximumEmailLength     = 254
)

// UpsertObjectInput creates or updates one person identified by email, or one
// organization identified by name. Pipedrive does not enforce either value as
// unique, so more than one match selects multipleMatches and writes nothing.
type UpsertObjectInput struct {
	// ObjectType is persons or organizations; deals have no identity value, so use createObject.
	ObjectType ObjectType `json:"objectType"`
	// IDProperty is email for persons or name for organizations.
	IDProperty string `json:"idProperty"`
	// IDValue is the email address or the organization name. Pipedrive's exact
	// search ignores case, and so does the connector's match.
	IDValue string `json:"idValue"`
	// Fields maps standard field names to JSON values written on create and on
	// update. A person requires name; neither may set the identity field
	// itself, which the connector writes on create.
	Fields map[string]json.RawMessage `json:"fields,omitempty"`
	// CustomFields maps 40-character custom field keys to JSON values written on create and on update.
	CustomFields map[string]json.RawMessage `json:"customFields,omitempty"`
}

// UpsertedObject is the record an upsert created or updated.
type UpsertedObject struct {
	// Object is the record with the values Pipedrive returned.
	Object CRMObject `json:"object"`
	// Created reports whether this attempt created the record. After a retry
	// that found the record an earlier attempt created, it is false.
	Created bool `json:"created"`
	// MatchingIDs lists the records that carry the identity value on multipleMatches.
	MatchingIDs []string `json:"matchingIds,omitempty"`
}

// UpsertObjectOperation implements the identity upsert Mutation.
type UpsertObjectOperation struct{ client *Client }

// upsertRequests holds the encoded create and update bodies; a nil update body reads the match instead.
type upsertRequests struct {
	identityField SearchField
	createBody    []byte
	updateBody    []byte
}

var upsertBranches = operationBranches{
	rejected: UpsertObjectBranchProviderRejected, invalidResponse: UpsertObjectBranchInvalidResponse, defect: UpsertObjectBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (UpsertObjectOperation) Definition() sdkgo.MutationDefinition { return UpsertObjectDefinition }

// IdempotencyKey derives the receipt key from the stable Call ID. Pipedrive has
// no idempotency key; the identity search before every create provides convergence.
func (UpsertObjectOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertObjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke searches the identity value, updates the one match, or creates the
// record. A create is preceded by a Dex heartbeat checkpoint; an attempt that
// finds the checkpoint selects uncertain instead of creating again when it finds
// no match or its search or update fails without a retry.
func (operation UpsertObjectOperation) Invoke(call sdkgo.Call, input UpsertObjectInput) sdkgo.MutationAttempt[UpsertedObject] {
	const operationID = "upsertObject"
	requests, err := input.upsertRequests()
	if err != nil {
		return sdkgo.NewMutationBranch(UpsertObjectBranchDefect, UpsertedObject{}, providerFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	isEarlierCreatePossible := hasEarlierDispatch(call)
	session, cancel, failure := operation.client.startSession(call, operationID)
	if failure != nil {
		if isEarlierCreatePossible && failure.route != sessionRetry {
			return unconfirmedCreateAttempt(failure.failure, sdkgo.Receipt{})
		}
		return mutationAttemptForSession[UpsertedObject](failure, upsertBranches)
	}
	defer cancel()
	matchingIDs, hasMoreMatches, result := operation.findIdentityMatches(session, input, requests.identityField)
	receipt := operation.client.receipt(call, result.response, "")
	switch {
	case result.outcome != exchangeSucceeded && isEarlierCreatePossible && !isRetryableExchange(result.outcome):
		return unconfirmedCreateAttempt(result.failure, receipt)
	case result.outcome != exchangeSucceeded:
		return repeatableMutationAttemptForExchange[UpsertedObject](result, receipt, upsertBranches)
	case len(matchingIDs) > 1 || hasMoreMatches:
		return sdkgo.NewMutationBranch(UpsertObjectBranchMultipleMatches, UpsertedObject{MatchingIDs: matchingIDs}, providerFailurePointer(operationID, sdkgo.FailureConflict,
			fmt.Sprintf("%d or more Pipedrive %s carry the identity value, so nothing was written", max(len(matchingIDs), 2), input.ObjectType)), receipt)
	case len(matchingIDs) == 1:
		return operation.updateMatch(session, input.ObjectType, matchingIDs[0], requests.updateBody, isEarlierCreatePossible)
	case isEarlierCreatePossible:
		return unconfirmedCreateAttempt(providerFailure(operationID, sdkgo.FailureTransport, "Pipedrive search finds no record with the identity value"), receipt)
	}
	return operation.createRecord(session, input.ObjectType, requests.createBody)
}

// findIdentityMatches runs Pipedrive's exact search and keeps results whose identity value really matches.
func (operation UpsertObjectOperation) findIdentityMatches(session *operationSession, input UpsertObjectInput, field SearchField) ([]string, bool, pipedriveExchange) {
	query := url.Values{
		"term": {input.IDValue}, "fields": {string(field)}, "exact_match": {"true"}, "limit": {strconv.Itoa(maximumIdentityMatches)},
	}
	result := operation.client.exchange(session, providerRequest{method: http.MethodGet, path: searchPath(input.ObjectType), query: query})
	if result.outcome != exchangeSucceeded {
		return nil, false, result
	}
	page, err := decodeSearchPage(input.ObjectType, result.response.body)
	if err != nil {
		result.outcome, result.failure = exchangeInvalid, providerFailure(session.operation, sdkgo.FailureProtocol, err.Error())
		return nil, false, result
	}
	var matchingIDs []string
	for _, object := range page.Objects {
		if input.isIdentityOf(object) {
			matchingIDs = append(matchingIDs, object.ID)
		}
	}
	return matchingIDs, page.NextCursor != "", result
}

// updateMatch writes the fields to the one match, or reads it when there is nothing to write.
func (operation UpsertObjectOperation) updateMatch(
	session *operationSession, objectType ObjectType, objectID string, updateBody []byte, isEarlierCreatePossible bool,
) sdkgo.MutationAttempt[UpsertedObject] {
	var object CRMObject
	var result pipedriveExchange
	if updateBody == nil {
		object, result = operation.client.readRecord(session, objectType, objectID)
	} else {
		object, result = operation.client.patchRecord(session, objectType, objectID, updateBody)
	}
	receipt := operation.client.receipt(session.call, result.response, objectID)
	if result.outcome != exchangeSucceeded && isEarlierCreatePossible && !isRetryableExchange(result.outcome) {
		return unconfirmedCreateAttempt(result.failure, receipt)
	}
	if result.outcome != exchangeSucceeded {
		return repeatableMutationAttemptForExchange[UpsertedObject](result, receipt, upsertBranches)
	}
	return sdkgo.NewMutationBranch(UpsertObjectBranchUpserted, UpsertedObject{Object: object}, nil, receipt)
}

// createRecord keeps the checkpoint after an unconfirmed create, so the next attempt finds the record by identity.
func (operation UpsertObjectOperation) createRecord(session *operationSession, objectType ObjectType, createBody []byte) sdkgo.MutationAttempt[UpsertedObject] {
	call := session.call
	if err := recordDispatch(call); err != nil {
		return dispatchCheckpointRetry[UpsertedObject](session.operation)
	}
	result := operation.client.exchange(session, providerRequest{method: http.MethodPost, path: collectionPath(objectType), body: createBody})
	receipt := operation.client.receipt(call, result.response, "")
	switch result.outcome {
	case exchangeSucceeded:
	case exchangeNotSent, exchangeRateLimited:
		releaseDispatch(call)
		return sdkgo.NewMutationRetry[UpsertedObject](result.failure, result.retryAfter)
	case exchangeUnavailable, exchangeInvalid:
		return sdkgo.NewMutationRetry[UpsertedObject](result.failure, result.retryAfter)
	case exchangeDefect:
		releaseDispatch(call)
		return sdkgo.NewMutationBranch(UpsertObjectBranchDefect, UpsertedObject{}, &result.failure, receipt)
	default:
		releaseDispatch(call)
		return sdkgo.NewMutationBranch(UpsertObjectBranchProviderRejected, UpsertedObject{}, &result.failure, receipt)
	}
	object, err := decodeRecordBody(objectType, result.response.body)
	if err != nil {
		return sdkgo.NewMutationRetry[UpsertedObject](providerFailure(session.operation, sdkgo.FailureProtocol,
			"Pipedrive accepted the create but returned an invalid record; the next attempt reads it back by its identity value"), 0)
	}
	receipt.ProviderObjectID = object.ID
	return sdkgo.NewMutationBranch(UpsertObjectBranchUpserted, UpsertedObject{Object: object, Created: true}, nil, receipt)
}

// unconfirmedCreateAttempt selects uncertain, because the earlier attempt's create may exist.
func unconfirmedCreateAttempt(failure sdkgo.Failure, receipt sdkgo.Receipt) sdkgo.MutationAttempt[UpsertedObject] {
	failure.Message = "an earlier attempt of this Step sent a create that was never confirmed, and " + failure.Message
	return sdkgo.NewMutationUncertain(UpsertedObject{}, failure, receipt)
}

func (input UpsertObjectInput) upsertRequests() (upsertRequests, error) {
	if err := validateWrittenFields(input.Fields, input.CustomFields); err != nil {
		return upsertRequests{}, err
	}
	createFields := maps.Clone(input.Fields)
	if createFields == nil {
		createFields = map[string]json.RawMessage{}
	}
	var requests upsertRequests
	switch input.ObjectType {
	case ObjectTypePersons:
		if input.IDProperty != IdentityPropertyEmail {
			return upsertRequests{}, errors.New("a persons upsert is identified by idProperty email")
		}
		address, err := mail.ParseAddress(input.IDValue)
		if err != nil || address.Address != input.IDValue || len(input.IDValue) > maximumEmailLength {
			return upsertRequests{}, errors.New("idValue must be one plain email address")
		}
		if _, hasEmails := input.Fields["emails"]; hasEmails {
			return upsertRequests{}, errors.New("fields cannot set emails; the connector writes idValue as the primary email")
		}
		if err := requireNameField(ObjectTypePersons, input.Fields); err != nil {
			return upsertRequests{}, err
		}
		createFields["emails"] = primaryEmailValue(input.IDValue)
		requests.identityField = SearchFieldEmail
	case ObjectTypeOrganizations:
		if input.IDProperty != IdentityPropertyName {
			return upsertRequests{}, errors.New("an organizations upsert is identified by idProperty name")
		}
		if strings.TrimSpace(input.IDValue) != input.IDValue || input.IDValue == "" || utf8.RuneCountInString(input.IDValue) > MaximumSearchTermCharacters {
			return upsertRequests{}, fmt.Errorf("idValue must be a name of 1 to %d characters without surrounding spaces", MaximumSearchTermCharacters)
		}
		if _, hasName := input.Fields["name"]; hasName {
			return upsertRequests{}, errors.New("fields cannot set name; the connector writes idValue as the organization name")
		}
		createFields["name"] = StringValue(input.IDValue)
		requests.identityField = SearchFieldName
	default:
		return upsertRequests{}, errors.New("upsert supports persons by email and organizations by name; create deals with createObject")
	}
	var err error
	if requests.createBody, err = encodeWriteBody(createFields, input.CustomFields); err != nil {
		return upsertRequests{}, err
	}
	if len(input.Fields)+len(input.CustomFields) > 0 {
		if requests.updateBody, err = encodeWriteBody(input.Fields, input.CustomFields); err != nil {
			return upsertRequests{}, err
		}
	}
	return requests, nil
}

// primaryEmailValue is a person's emails field holding one primary work address.
func primaryEmailValue(address string) json.RawMessage {
	return json.RawMessage(`[{"value":` + string(StringValue(address)) + `,"primary":true,"label":"work"}]`)
}

// isIdentityOf reports whether a search result carries the identity value, ignoring case as Pipedrive does.
func (input UpsertObjectInput) isIdentityOf(object CRMObject) bool {
	if input.ObjectType == ObjectTypeOrganizations {
		return strings.EqualFold(strings.TrimSpace(object.Name), input.IDValue)
	}
	for _, email := range object.Emails {
		if strings.EqualFold(strings.TrimSpace(email), input.IDValue) {
			return true
		}
	}
	return false
}
