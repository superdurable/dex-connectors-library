// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const getEnvelopeOperationID = "getEnvelope"

// GetEnvelopeInput names the envelope to read.
type GetEnvelopeInput struct {
	// EnvelopeID is the envelope's GUID, such as a createEnvelopeFromTemplate Result's envelopeId.
	EnvelopeID string `json:"envelopeId"`
}

// GetEnvelopeOperation implements the getEnvelope Query.
type GetEnvelopeOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (GetEnvelopeOperation) Definition() sdkgo.QueryDefinition { return GetEnvelopeDefinition }

// Invoke reads one envelope with its text custom fields. A 429 or 5xx returns Retry.
func (operation GetEnvelopeOperation) Invoke(call sdkgo.Call, input GetEnvelopeInput) sdkgo.QueryAttempt[Envelope] {
	client := operation.client
	if err := validateEnvelopeID(input.EnvelopeID); err != nil {
		return sdkgo.NewQueryBranch(GetEnvelopeBranchDefect, Envelope{}, docusignFailurePointer(getEnvelopeOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, sessionFailure := client.openSession(call, getEnvelopeOperationID)
	if sessionFailure != nil {
		return querySessionFailure[Envelope](sessionFailure, GetEnvelopeBranchProviderRejected, GetEnvelopeBranchInvalidResponse, GetEnvelopeBranchDefect)
	}
	envelope, outcome, err := client.readEnvelope(call, &session, getEnvelopeOperationID, input.EnvelopeID)
	switch {
	case err != nil:
		return sdkgo.NewQueryBranch(GetEnvelopeBranchInvalidResponse, Envelope{}, docusignFailurePointer(getEnvelopeOperationID, responseFailureKind(err), responseFailureMessage(err)), client.receipt(input.EnvelopeID, ""))
	case outcome == nil:
		return sdkgo.NewQueryBranch(GetEnvelopeBranchFound, envelope, nil, client.receipt(envelope.EnvelopeID, ""))
	case outcome.isRetry:
		return sdkgo.NewQueryRetry[Envelope](outcome.failure, outcome.retryAfter)
	case outcome.isNotFound:
		return sdkgo.NewQueryBranch(GetEnvelopeBranchNotFound, Envelope{}, &outcome.failure, client.receipt(input.EnvelopeID, outcome.errorCode))
	default:
		return sdkgo.NewQueryBranch(GetEnvelopeBranchProviderRejected, Envelope{}, &outcome.failure, client.receipt(input.EnvelopeID, outcome.errorCode))
	}
}

// readEnvelope returns an outcome for a refusal or transport failure, an error for a bad body.
func (client *Client) readEnvelope(
	call sdkgo.Call, session *docusignSession, operationID string, envelopeID string,
) (Envelope, *docusignOutcome, error) {
	response, err := client.sendAPIRequest(call, session, docusignRequest{
		method: http.MethodGet, path: "/envelopes/" + url.PathEscape(envelopeID), query: url.Values{"include": {"custom_fields"}},
	}, nil)
	if errors.Is(err, errDocuSignResponseTooLarge) {
		return Envelope{}, nil, err
	}
	if err != nil {
		return Envelope{}, &docusignOutcome{isRetry: true, failure: docusignFailure(operationID, sdkgo.FailureTransport, "DocuSign could not be reached to read the envelope")}, nil
	}
	if response.statusCode != http.StatusOK {
		outcome := client.classifyDocuSignFailure(operationID, response)
		return Envelope{}, &outcome, nil
	}
	envelope, err := decodeEnvelope(response.body)
	if err != nil {
		return Envelope{}, nil, err
	}
	if envelope.EnvelopeID != lowercaseGUID(envelopeID) {
		return Envelope{}, nil, errDocuSignResponseMalformed
	}
	return envelope, nil, nil
}

// querySessionFailure reports a call that never reached the eSignature API.
func querySessionFailure[T any](failure *sessionFailure, rejected sdkgo.BranchID, invalidResponse sdkgo.BranchID, defect sdkgo.BranchID) sdkgo.QueryAttempt[T] {
	var zero T
	switch failure.kind {
	case sessionFailureRetry:
		return sdkgo.NewQueryRetry[T](failure.failure, failure.retryAfter)
	case sessionFailureRejected:
		return sdkgo.NewQueryBranch(rejected, zero, &failure.failure, sdkgo.Receipt{})
	case sessionFailureInvalidResponse:
		return sdkgo.NewQueryBranch(invalidResponse, zero, &failure.failure, sdkgo.Receipt{})
	default:
		return sdkgo.NewQueryBranch(defect, zero, &failure.failure, sdkgo.Receipt{})
	}
}
