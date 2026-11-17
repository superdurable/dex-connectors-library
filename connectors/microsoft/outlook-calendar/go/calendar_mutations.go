// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateCalendarInput names a calendar in the connection's mailbox.
type CreateCalendarInput struct {
	// Name is the new calendar display name, 1 to 255 UTF-8 bytes.
	Name string `json:"name"`
}

// UpdateCalendarInput sets a calendar's name.
type UpdateCalendarInput struct {
	// CalendarID is a non-default Graph calendar ID.
	CalendarID string `json:"calendarId"`
	// Name is the desired display name, 1 to 255 UTF-8 bytes.
	Name string `json:"name"`
}

// DeleteCalendarInput identifies a non-default calendar.
type DeleteCalendarInput struct {
	// CalendarID is the Graph calendar ID; the default calendar cannot be deleted.
	CalendarID string `json:"calendarId"`
}

// DeleteCalendarOutput identifies the absent calendar.
type DeleteCalendarOutput struct {
	// CalendarID is the deleted or already absent calendar ID.
	CalendarID string `json:"calendarId"`
}

// CreateCalendarOperation implements the createCalendar mutation.
type CreateCalendarOperation struct{ client *Client }

// UpdateCalendarOperation implements the updateCalendar mutation.
type UpdateCalendarOperation struct{ client *Client }

// DeleteCalendarOperation implements the deleteCalendar mutation.
type DeleteCalendarOperation struct{ client *Client }

// Definition returns the create mutation definition.
func (CreateCalendarOperation) Definition() sdkgo.MutationDefinition { return CreateCalendarDefinition }

// Definition returns the update mutation definition.
func (UpdateCalendarOperation) Definition() sdkgo.MutationDefinition { return UpdateCalendarDefinition }

// Definition returns the delete mutation definition.
func (DeleteCalendarOperation) Definition() sdkgo.MutationDefinition { return DeleteCalendarDefinition }

// IdempotencyKey derives the receipt key from the stable call ID.
func (CreateCalendarOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCalendarInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the receipt key from the stable call ID.
func (UpdateCalendarOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCalendarInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// IdempotencyKey derives the receipt key from the stable call ID.
func (DeleteCalendarOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteCalendarInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a calendar with a bounded send cap. Calendar names are not unique,
// so an unknown POST cannot be identified by name and remains uncertain at the cap.
func (operation CreateCalendarOperation) Invoke(call sdkgo.Call, input CreateCalendarInput) sdkgo.MutationAttempt[Calendar] {
	const operationID = "createCalendar"
	if !validCalendarName(input.Name) {
		return sdkgo.NewMutationBranch(CreateCalendarBranchDefect, Calendar{}, failurePointer(sdkgo.FailureValidation, operationID, "name must be 1 to 255 UTF-8 bytes"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateCalendarDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateCalendarBranchDefect, Calendar{}, failurePointer(sdkgo.FailureLocalDefect, operationID, "calendar dispatch checkpoint is invalid"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationBranch(CreateCalendarBranchDefect, Calendar{}, failurePointer(sdkgo.FailureLocalDefect, operationID, "calendar dispatch checkpoint could not be read"), sdkgo.Receipt{})
	}
	credentials, mailboxRoot, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateCalendarBranchDefect, Calendar{}, failure, sdkgo.Receipt{})
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		// Calendar names are not unique; read-back can show a candidate but cannot prove which POST created it.
		_, _ = operation.client.send(call, &credentials, graphRequest{method: http.MethodGet, path: mailboxRoot + "/calendars", query: url.Values{"$select": {"id,name"}, "$filter": {"name eq '" + strings.ReplaceAll(input.Name, "'", "''") + "'"}}})
		return sdkgo.NewMutationUncertain(Calendar{}, calendarFailure(sdkgo.FailureTransport, operationID, "calendar create send cap was reached without confirmation"), sdkgo.Receipt{})
	}
	var response graphResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		var sendErr error
		response, sendErr = operation.client.send(call, &credentials, graphRequest{method: http.MethodPost, path: mailboxRoot + "/calendars", payload: map[string]string{"name": input.Name}, requestContext: requestContext})
		return sendErr
	})
	if err != nil {
		return sdkgo.NewMutationRetry[Calendar](calendarFailure(sdkgo.FailureTransport, operationID, "calendar creation outcome is unknown"), 0)
	}
	receipt := operation.client.receipt(call, response, "")
	if response.statusCode == http.StatusCreated {
		var item Calendar
		if json.Unmarshal(response.body, &item) != nil || item.ID == "" || item.Name != input.Name {
			return sdkgo.NewMutationBranch(CreateCalendarBranchInvalidResponse, Calendar{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned invalid calendar metadata"), receipt)
		}
		receipt.ProviderObjectID = item.ID
		return sdkgo.NewMutationBranch(CreateCalendarBranchCreated, item, nil, receipt)
	}
	classification := classifyFailureStatus(response)
	if response.statusCode == http.StatusTooManyRequests || response.statusCode == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[Calendar](calendarFailure(sdkgo.FailureAvailability, operationID, "calendar rejection checkpoint could not be recorded"), 0)
		}
		return sdkgo.NewMutationRetry[Calendar](calendarFailure(classification.kind, operationID, classification.message), classification.retryAfter)
	}
	if classification.isRetryable {
		return sdkgo.NewMutationRetry[Calendar](calendarFailure(classification.kind, operationID, "calendar creation outcome is unknown"), classification.retryAfter)
	}
	return sdkgo.NewMutationBranch(CreateCalendarBranchProviderRejected, Calendar{}, failurePointer(classification.kind, operationID, classification.message), receipt)
}

// Invoke sets a calendar name absolutely, so a retry repeats the same state change.
func (operation UpdateCalendarOperation) Invoke(call sdkgo.Call, input UpdateCalendarInput) sdkgo.MutationAttempt[Calendar] {
	const operationID = "updateCalendar"
	if validateExistingEventID(input.CalendarID) != nil || !validCalendarName(input.Name) {
		return sdkgo.NewMutationBranch(UpdateCalendarBranchDefect, Calendar{}, failurePointer(sdkgo.FailureValidation, operationID, "calendarId or name is invalid"), sdkgo.Receipt{})
	}
	credentials, mailboxRoot, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(UpdateCalendarBranchDefect, Calendar{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.send(call, &credentials, graphRequest{method: http.MethodPatch, path: calendarPath(mailboxRoot, input.CalendarID), payload: map[string]string{"name": input.Name}})
	if err != nil {
		requestFailure := asProviderRequestError(err)
		if requestFailure.kind == sdkgo.FailureLocalDefect {
			return sdkgo.NewMutationBranch(UpdateCalendarBranchDefect, Calendar{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[Calendar](calendarFailure(sdkgo.FailureTransport, operationID, "calendar update outcome is unknown"), 0)
	}
	receipt := operation.client.receipt(call, response, input.CalendarID)
	if isMissingResourceStatus(response.statusCode) {
		return sdkgo.NewMutationBranch(UpdateCalendarBranchNotFound, Calendar{}, failurePointer(sdkgo.FailureNotFound, operationID, "calendar was not found"), receipt)
	}
	if !isSuccessStatus(response.statusCode) {
		classification := classifyFailureStatus(response)
		if classification.isRetryable {
			return sdkgo.NewMutationRetry[Calendar](calendarFailure(classification.kind, operationID, classification.message), classification.retryAfter)
		}
		return sdkgo.NewMutationBranch(UpdateCalendarBranchProviderRejected, Calendar{}, failurePointer(classification.kind, operationID, classification.message), receipt)
	}
	var item Calendar
	if json.Unmarshal(response.body, &item) != nil || item.ID != input.CalendarID || item.Name != input.Name {
		return sdkgo.NewMutationBranch(UpdateCalendarBranchInvalidResponse, Calendar{}, failurePointer(sdkgo.FailureProtocol, operationID, "provider returned invalid calendar metadata"), receipt)
	}
	return sdkgo.NewMutationBranch(UpdateCalendarBranchUpdated, item, nil, receipt)
}

// Invoke deletes a non-default calendar and treats an already absent one as completed.
func (operation DeleteCalendarOperation) Invoke(call sdkgo.Call, input DeleteCalendarInput) sdkgo.MutationAttempt[DeleteCalendarOutput] {
	const operationID = "deleteCalendar"
	if validateExistingEventID(input.CalendarID) != nil {
		return sdkgo.NewMutationBranch(DeleteCalendarBranchDefect, DeleteCalendarOutput{}, failurePointer(sdkgo.FailureValidation, operationID, "calendarId is invalid"), sdkgo.Receipt{})
	}
	credentials, mailboxRoot, failure := operation.client.resolveCredentials(call, operationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(DeleteCalendarBranchDefect, DeleteCalendarOutput{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.send(call, &credentials, graphRequest{method: http.MethodDelete, path: calendarPath(mailboxRoot, input.CalendarID)})
	if err != nil {
		requestFailure := asProviderRequestError(err)
		if requestFailure.kind == sdkgo.FailureLocalDefect {
			return sdkgo.NewMutationBranch(DeleteCalendarBranchDefect, DeleteCalendarOutput{}, failurePointer(requestFailure.kind, operationID, requestFailure.message), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[DeleteCalendarOutput](calendarFailure(sdkgo.FailureTransport, operationID, "calendar deletion outcome is unknown"), 0)
	}
	receipt := operation.client.receipt(call, response, input.CalendarID)
	if response.statusCode == http.StatusNoContent || isMissingResourceStatus(response.statusCode) {
		return sdkgo.NewMutationBranch(DeleteCalendarBranchDeleted, DeleteCalendarOutput{CalendarID: input.CalendarID}, nil, receipt)
	}
	classification := classifyFailureStatus(response)
	if classification.isRetryable {
		return sdkgo.NewMutationRetry[DeleteCalendarOutput](calendarFailure(classification.kind, operationID, classification.message), classification.retryAfter)
	}
	return sdkgo.NewMutationBranch(DeleteCalendarBranchProviderRejected, DeleteCalendarOutput{}, failurePointer(classification.kind, operationID, classification.message), receipt)
}

func validCalendarName(name string) bool {
	return utf8.ValidString(name) && strings.TrimSpace(name) == name && len(name) >= 1 && len(name) <= 255
}
