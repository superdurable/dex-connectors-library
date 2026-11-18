// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateEventInput describes one event to create. Every attempt of one Step execution
// uses the same idempotency key: TransactionID when set, otherwise the Step's Call ID.
// The key is sent as Graph's transactionId and stamped on the event as a connector-owned
// extended property, so a retried Step finds and returns its own event.
type CreateEventInput struct {
	// CalendarID is a writable calendar ID from the calendar picker; blank uses the mailbox's default calendar.
	CalendarID string `json:"calendarId,omitempty"`
	// TransactionID optionally fixes the idempotency key for business-level deduplication across Step
	// executions: 1 to 64 letters, digits, dots, hyphens, or underscores. Blank uses the Call ID.
	TransactionID string `json:"transactionId,omitempty"`
	// Subject is the required event title.
	Subject string `json:"subject"`
	// Body is the optional plain-text event body.
	Body string `json:"body,omitempty"`
	// Location is the optional location display name.
	Location string `json:"location,omitempty"`
	// Start is the inclusive start; see EventDateTime and ValidateEventTimes.
	Start EventDateTime `json:"start"`
	// End is the exclusive end; see EventDateTime and ValidateEventTimes.
	End EventDateTime `json:"end"`
	// IsAllDay creates an all-day event; Start and End must then be midnights in one zone.
	IsAllDay bool `json:"isAllDay,omitempty"`
	// Attendees lists unique bare attendee addresses. Graph sends every attendee an invitation
	// when the event is created, and that cannot be turned off; empty creates an event without attendees.
	Attendees []EventAttendeeInput `json:"attendees,omitempty"`
	// RequestOnlineMeeting asks Graph to attach a Microsoft Teams meeting to the event.
	RequestOnlineMeeting bool `json:"requestOnlineMeeting,omitempty"`
}

// CreateEventOutput is the event that carries the Step's idempotency key.
type CreateEventOutput struct {
	// Event is the created or previously created event.
	Event Event `json:"event"`
	// WasAlreadyCreated reports that an earlier attempt had already created this event, so no POST was sent.
	WasAlreadyCreated bool `json:"wasAlreadyCreated"`
	// DuplicateEventIDs lists further events with the same idempotency key. Graph's transactionId is meant
	// to prevent them, so a non-empty list means two concurrent attempts both created an event.
	DuplicateEventIDs []string `json:"duplicateEventIds,omitempty"`
}

// CreateEventOperation implements the createEvent connector Mutation.
type CreateEventOperation struct{ client *Client }

// Definition returns the immutable connector operation definition.
func (CreateEventOperation) Definition() sdkgo.MutationDefinition { return CreateEventDefinition }

// IdempotencyKey returns input.TransactionID when set, otherwise the Call ID, a UUID.
func (CreateEventOperation) IdempotencyKey(callID sdkgo.CallID, input CreateEventInput) sdkgo.IdempotencyKey {
	if input.TransactionID != "" {
		return sdkgo.IdempotencyKey(input.TransactionID)
	}
	return sdkgo.IdempotencyKey(callID)
}

// acceptedEventProviderID marks a 2xx whose event ID could not be read, so no later attempt POSTs again.
const acceptedEventProviderID = "accepted"

// Invoke runs under a single-dispatch Fence capped at the call's send limit. Every attempt looks
// the idempotency key up first through the extended-property marker and returns an event that
// carries it. Otherwise it POSTs with the key as transactionId and marker. An unknown outcome
// resends below the cap and selects uncertain at it; a 2xx is never resent; only a definite
// refusal of a first send selects providerRejected.
func (operation CreateEventOperation) Invoke(call sdkgo.Call, input CreateEventInput) sdkgo.MutationAttempt[CreateEventOutput] {
	idempotencyKey := string(call.IdempotencyKey)
	display, err := validateCreateEventInput(input, idempotencyKey)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateEventBranchDefect, CreateEventOutput{}, failurePointer(sdkgo.FailureValidation, createEventOperationID, err.Error()), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateEventDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateEventBranchDefect, CreateEventOutput{}, failurePointer(sdkgo.FailureLocalDefect, createEventOperationID, "createEvent dispatch fence could not be constructed"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		// An unreadable checkpoint is an unknown outcome; the retry budget bounds these attempts.
		return sdkgo.NewMutationRetry[CreateEventOutput](calendarFailure(sdkgo.FailureTransport, createEventOperationID, "an earlier attempt may have created the event, and its dispatch checkpoint is unreadable"), 0)
	}
	attempt := &createEventAttempt{
		client: operation.client, call: call, fence: fence, input: input, idempotencyKey: idempotencyKey, display: display,
		hasPossibleEarlierSend:  decision.Kind != singledispatch.SendAllowed || decision.MaximumPriorSends > 0 || decision.Marker.ProviderID != "",
		canResendUnknownOutcome: decision.MaximumPriorSends+1 < call.MaxSends(),
		isAccepted:              decision.Marker.ProviderID != "",
		isSendForbidden:         decision.Kind == singledispatch.ReadBackRequired || decision.Marker.ProviderID != "",
	}
	return attempt.run()
}

const createEventOperationID = "createEvent"

// createEventAttempt carries one createEvent attempt's Fence decision and resolved connection.
type createEventAttempt struct {
	client         *Client
	call           sdkgo.Call
	fence          *singledispatch.Fence
	input          CreateEventInput
	idempotencyKey string
	display        eventDisplay
	credentials    Credentials
	mailboxRoot    string
	// hasPossibleEarlierSend reports an earlier attempt whose POST may have created the event.
	hasPossibleEarlierSend bool
	// canResendUnknownOutcome reports that this attempt's POST would leave a further send under the cap.
	canResendUnknownOutcome bool
	// isAccepted reports that an earlier attempt received a 2xx, so the event exists.
	isAccepted bool
	// isSendForbidden reports that this attempt may only read back.
	isSendForbidden bool
}

func (attempt *createEventAttempt) run() sdkgo.MutationAttempt[CreateEventOutput] {
	credentials, mailboxRoot, failure, isRetryable := attempt.client.resolveCredentials(attempt.call, createEventOperationID)
	if failure != nil {
		return attempt.credentialFailure(*failure, isRetryable)
	}
	attempt.credentials, attempt.mailboxRoot = credentials, mailboxRoot
	if lookup, isDecided := attempt.lookUp(); isDecided {
		return lookup
	}
	if attempt.isSendForbidden {
		return attempt.uncertain(calendarFailure(sdkgo.FailureTransport, createEventOperationID, "an earlier attempt may have created the event, and the marker lookup cannot confirm it"), graphResponse{})
	}
	return attempt.send()
}

// credentialFailure keeps notSent and defect for a first send only; after a possible send it retries or selects uncertain.
func (attempt *createEventAttempt) credentialFailure(failure sdkgo.Failure, isRetryable bool) sdkgo.MutationAttempt[CreateEventOutput] {
	if attempt.hasPossibleEarlierSend {
		if isRetryable {
			return sdkgo.NewMutationRetry[CreateEventOutput](failure, 0)
		}
		return attempt.uncertain(calendarFailure(failure.Kind, createEventOperationID, "an earlier attempt may have created the event, and the connection credentials are unavailable to confirm it"), graphResponse{})
	}
	if err := attempt.fence.RecordNotSent(); err != nil {
		return sdkgo.NewMutationRetry[CreateEventOutput](calendarFailure(sdkgo.FailureAvailability, createEventOperationID, "credentials and the no-send checkpoint are unavailable"), 0)
	}
	if isRetryable {
		return sdkgo.NewMutationRetry[CreateEventOutput](failure, 0)
	}
	return sdkgo.NewMutationBranch(CreateEventBranchDefect, CreateEventOutput{}, &failure, sdkgo.Receipt{})
}

// lookUp finds the event by its marker; false means the marker is on no event yet.
func (attempt *createEventAttempt) lookUp() (sdkgo.MutationAttempt[CreateEventOutput], bool) {
	read := attempt.client.findEventByIdempotencyKey(attempt.call, &attempt.credentials, createEventOperationID, attempt.mailboxRoot, attempt.idempotencyKey, attempt.input.CalendarID, attempt.display)
	switch read.outcome {
	case eventReadMissing:
		return sdkgo.MutationAttempt[CreateEventOutput]{}, false
	case eventReadFound:
		// The found event is authoritative even if its checkpoint is lost.
		_ = attempt.fence.RecordProviderID(read.event.ID)
		return sdkgo.NewMutationBranch(CreateEventBranchCreated, CreateEventOutput{
			Event: read.event, WasAlreadyCreated: true, DuplicateEventIDs: read.duplicateEventIDs,
		}, nil, attempt.client.receipt(attempt.call, read.response, read.event.ID)), true
	case eventReadRejected:
		if isDefiniteGraphRefusal(read.response) {
			if attempt.hasPossibleEarlierSend {
				return attempt.uncertain(read.failure, read.response), true
			}
			// Nothing was sent; a lost checkpoint leaves no claim to misreport.
			_ = attempt.fence.RecordNotSent()
			return sdkgo.NewMutationBranch(CreateEventBranchProviderRejected, CreateEventOutput{}, &read.failure, attempt.client.receipt(attempt.call, read.response, "")), true
		}
	case eventReadInvalid:
		if !attempt.hasPossibleEarlierSend {
			// Nothing was sent yet, so an unusable lookup answer can end the Step without a duplicate.
			_ = attempt.fence.RecordNotSent()
			return sdkgo.NewMutationBranch(CreateEventBranchInvalidResponse, CreateEventOutput{}, &read.failure, attempt.client.receipt(attempt.call, read.response, "")), true
		}
	case eventReadDefect:
		if !attempt.hasPossibleEarlierSend {
			_ = attempt.fence.RecordNotSent()
			return sdkgo.NewMutationBranch(CreateEventBranchDefect, CreateEventOutput{}, &read.failure, sdkgo.Receipt{}), true
		}
		return attempt.uncertain(read.failure, graphResponse{}), true
	}
	// A transient, unlisted, or unreadable lookup answer leaves the outcome unknown.
	return sdkgo.NewMutationRetry[CreateEventOutput](read.failure, read.retryAfter), true
}

// send POSTs the event once under the Fence and classifies the answer.
func (attempt *createEventAttempt) send() sdkgo.MutationAttempt[CreateEventOutput] {
	var response graphResponse
	var sendErr error
	dispatchErr := attempt.fence.Dispatch(func(requestContext context.Context) error {
		response, sendErr = attempt.client.send(attempt.call, &attempt.credentials, graphRequest{
			method: http.MethodPost, path: calendarPath(attempt.mailboxRoot, attempt.input.CalendarID) + "/events",
			payload: buildCreateEventPayload(attempt.input, attempt.idempotencyKey), requestContext: requestContext,
		})
		return sendErr
	})
	if errors.Is(dispatchErr, singledispatch.ErrCheckpoint) || errors.Is(dispatchErr, singledispatch.ErrInsufficientTime) || errors.Is(dispatchErr, singledispatch.ErrDispatchNotAllowed) {
		return sdkgo.NewMutationRetry[CreateEventOutput](calendarFailure(sdkgo.FailureAvailability, createEventOperationID, "the event POST was not sent in this attempt"), 0)
	}
	if sendErr != nil {
		requestFailure := asProviderRequestError(sendErr)
		switch {
		case requestFailure.kind == sdkgo.FailureResponseTooLarge && isSuccessStatus(response.statusCode):
			return attempt.acceptedWithoutEvent(response)
		case requestFailure.kind == sdkgo.FailureLocalDefect && !attempt.hasPossibleEarlierSend:
			return sdkgo.NewMutationBranch(CreateEventBranchDefect, CreateEventOutput{}, failurePointer(requestFailure.kind, createEventOperationID, requestFailure.message), sdkgo.Receipt{})
		}
		return attempt.resendOrUncertain(calendarFailure(sdkgo.FailureTransport, createEventOperationID, "create outcome is unknown; the retry looks the idempotency key up first"), 0, response)
	}
	if response.statusCode == http.StatusTooManyRequests || response.statusCode == http.StatusServiceUnavailable {
		classification := classifyFailureStatus(response)
		// Graph did not process the POST, so it does not count against the cap.
		_ = attempt.fence.RecordCanResend()
		return sdkgo.NewMutationRetry[CreateEventOutput](calendarFailure(classification.kind, createEventOperationID, classification.message), classification.retryAfter)
	}
	if isSuccessStatus(response.statusCode) {
		event, err := decodeEvent(response.body, attempt.input.CalendarID, attempt.display)
		if err != nil || (event.TransactionID != "" && event.TransactionID != attempt.idempotencyKey) {
			return attempt.acceptedWithoutEvent(response)
		}
		// The accepted event is known even if its checkpoint is lost.
		_ = attempt.fence.RecordProviderID(event.ID)
		return sdkgo.NewMutationBranch(CreateEventBranchCreated, CreateEventOutput{Event: event}, nil, attempt.client.receipt(attempt.call, response, event.ID))
	}
	classification := classifyFailureStatus(response)
	failure := calendarFailure(classification.kind, createEventOperationID, classification.message)
	if response.statusCode == http.StatusBadRequest || response.statusCode == http.StatusConflict {
		read := attempt.client.findEventByIdempotencyKey(attempt.call, &attempt.credentials, createEventOperationID, attempt.mailboxRoot, attempt.idempotencyKey, attempt.input.CalendarID, attempt.display)
		if read.outcome == eventReadFound {
			_ = attempt.fence.RecordProviderID(read.event.ID)
			return sdkgo.NewMutationBranch(CreateEventBranchCreated, CreateEventOutput{
				Event: read.event, WasAlreadyCreated: true, DuplicateEventIDs: read.duplicateEventIDs,
			}, nil, attempt.client.receipt(attempt.call, read.response, read.event.ID))
		}
		if read.outcome != eventReadMissing {
			return attempt.resendOrUncertain(failure, classification.retryAfter, response)
		}
	}
	if !isDefiniteGraphRefusal(response) {
		// Only an allowlisted refusal proves nothing was created; any other answer is unknown.
		return attempt.resendOrUncertain(failure, classification.retryAfter, response)
	}
	if attempt.hasPossibleEarlierSend {
		return attempt.uncertain(failure, response)
	}
	// A lost checkpoint leaves the claim, which agent hosts conservatively report as unknown.
	_ = attempt.fence.RecordCanResend()
	return sdkgo.NewMutationBranch(CreateEventBranchProviderRejected, CreateEventOutput{}, &failure, attempt.client.receipt(attempt.call, response, ""))
}

// acceptedWithoutEvent never resends after a 2xx: it records acceptance and reads the marker back.
func (attempt *createEventAttempt) acceptedWithoutEvent(response graphResponse) sdkgo.MutationAttempt[CreateEventOutput] {
	failure := calendarFailure(sdkgo.FailureProtocol, createEventOperationID, "Graph accepted the event, but its response is unusable")
	if err := attempt.fence.RecordProviderID(acceptedEventProviderID); err != nil {
		return attempt.uncertain(failure, response)
	}
	read := attempt.client.findEventByIdempotencyKey(attempt.call, &attempt.credentials, createEventOperationID, attempt.mailboxRoot, attempt.idempotencyKey, attempt.input.CalendarID, attempt.display)
	if read.outcome == eventReadFound {
		_ = attempt.fence.RecordProviderID(read.event.ID)
		return sdkgo.NewMutationBranch(CreateEventBranchCreated, CreateEventOutput{Event: read.event, DuplicateEventIDs: read.duplicateEventIDs}, nil, attempt.client.receipt(attempt.call, read.response, read.event.ID))
	}
	// The marker index may lag the create; the next attempt only reads back.
	return sdkgo.NewMutationRetry[CreateEventOutput](failure, 0)
}

// resendOrUncertain retries an unknown outcome while the send cap allows another POST.
func (attempt *createEventAttempt) resendOrUncertain(failure sdkgo.Failure, retryAfter time.Duration, response graphResponse) sdkgo.MutationAttempt[CreateEventOutput] {
	if attempt.canResendUnknownOutcome {
		return sdkgo.NewMutationRetry[CreateEventOutput](failure, retryAfter)
	}
	return attempt.uncertain(failure, response)
}

func (attempt *createEventAttempt) uncertain(failure sdkgo.Failure, response graphResponse) sdkgo.MutationAttempt[CreateEventOutput] {
	return sdkgo.NewMutationUncertain(CreateEventOutput{}, failure, attempt.client.receipt(attempt.call, response, ""))
}

// isDefiniteGraphRefusal reports an allowlisted refusal status whose body carries no created item ID.
func isDefiniteGraphRefusal(response graphResponse) bool {
	switch response.statusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
	default:
		return false
	}
	var body struct {
		ID json.RawMessage `json:"id"`
	}
	return json.Unmarshal(response.body, &body) != nil || len(body.ID) == 0 || string(body.ID) == "null" || string(body.ID) == `""`
}

func validateCreateEventInput(input CreateEventInput, idempotencyKey string) (eventDisplay, error) {
	if err := validateCalendarID(input.CalendarID); err != nil {
		return eventDisplay{}, err
	}
	if err := validateTransactionID(idempotencyKey); err != nil {
		return eventDisplay{}, err
	}
	if strings.TrimSpace(input.Subject) == "" {
		return eventDisplay{}, errors.New("subject is required")
	}
	if err := ValidateEventTimes(input.Start, input.End, input.IsAllDay); err != nil {
		return eventDisplay{}, err
	}
	if err := validateAttendeeInputs(input.Attendees); err != nil {
		return eventDisplay{}, err
	}
	location, zone, err := loadDisplayLocation(input.Start.TimeZone)
	if err != nil {
		return eventDisplay{}, err
	}
	return eventDisplay{location: location, zone: zone}, nil
}

func buildCreateEventPayload(input CreateEventInput, idempotencyKey string) map[string]any {
	payload := map[string]any{
		"subject": input.Subject, "start": encodeGraphBoundary(input.Start), "end": encodeGraphBoundary(input.End),
		"transactionId":                 idempotencyKey,
		"singleValueExtendedProperties": []map[string]string{{"id": idempotencyMarkerPropertyID, "value": idempotencyKey}},
	}
	if input.IsAllDay {
		payload["isAllDay"] = true
	}
	if input.Body != "" {
		payload["body"] = graphItemBody{ContentType: "text", Content: input.Body}
	}
	if input.Location != "" {
		payload["location"] = graphLocation{DisplayName: input.Location}
	}
	if len(input.Attendees) > 0 {
		payload["attendees"] = encodeAttendees(input.Attendees)
	}
	if input.RequestOnlineMeeting {
		payload["isOnlineMeeting"] = true
		payload["onlineMeetingProvider"] = teamsOnlineMeetingProvider
	}
	return payload
}
