// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar

import (
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// DeleteEventInput identifies an event to delete. Graph sends cancellation notices to attendees.
type DeleteEventInput struct {
	// EventID is the Graph event ID, including an occurrence ID.
	EventID string `json:"eventId"`
}

// DeleteEventOutput identifies the deleted or already absent event.
type DeleteEventOutput struct {
	// EventID is the event ID that is now absent.
	EventID string `json:"eventId"`
}

// DeleteEventOperation implements the deleteEvent mutation.
type DeleteEventOperation struct{ client *Client }

// Definition returns the mutation definition.
func (DeleteEventOperation) Definition() sdkgo.MutationDefinition { return DeleteEventDefinition }

// IdempotencyKey derives the receipt key from the stable call ID.
func (DeleteEventOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteEventInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes an event and treats an already absent event as completed.
func (operation DeleteEventOperation) Invoke(call sdkgo.Call, input DeleteEventInput) sdkgo.MutationAttempt[DeleteEventOutput] {
	const operationID = "deleteEvent"
	if validateExistingEventID(input.EventID) != nil {
		return sdkgo.NewMutationBranch(DeleteEventBranchDefect, DeleteEventOutput{}, failurePointer(sdkgo.FailureValidation, operationID, "eventId is invalid"), sdkgo.Receipt{})
	}
	credentials, mailboxRoot, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(DeleteEventBranchDefect, DeleteEventOutput{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.send(call, &credentials, graphRequest{method: http.MethodDelete, path: mailboxRoot + "/events/" + url.PathEscape(input.EventID)})
	if err != nil {
		requestFailure := asProviderRequestError(err)
		if requestFailure.kind == sdkgo.FailureLocalDefect {
			return sdkgo.NewMutationBranch(DeleteEventBranchDefect, DeleteEventOutput{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[DeleteEventOutput](calendarFailure(sdkgo.FailureTransport, operationID, "event deletion outcome is unknown"), 0)
	}
	receipt := operation.client.receipt(call, response, input.EventID)
	if response.statusCode == http.StatusNoContent || isMissingResourceStatus(response.statusCode) {
		return sdkgo.NewMutationBranch(DeleteEventBranchDeleted, DeleteEventOutput{EventID: input.EventID}, nil, receipt)
	}
	classification := classifyFailureStatus(response)
	if classification.isRetryable {
		return sdkgo.NewMutationRetry[DeleteEventOutput](calendarFailure(classification.kind, operationID, classification.message), classification.retryAfter)
	}
	return sdkgo.NewMutationBranch(DeleteEventBranchProviderRejected, DeleteEventOutput{}, failurePointer(classification.kind, operationID, classification.message), receipt)
}
