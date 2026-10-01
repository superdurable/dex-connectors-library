// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	// schedulingLinkDeadlineMargin leaves time to return uncertain before the Execute deadline.
	schedulingLinkDeadlineMargin = 3 * time.Second
	// minimumSchedulingLinkRequestTime is the least request time worth dispatching the POST with.
	minimumSchedulingLinkRequestTime = 5 * time.Second
)

// CreateSchedulingLinkInput names the event type a single-use scheduling link books.
type CreateSchedulingLinkInput struct {
	// EventTypeURI is the event type's Calendly URI, such as the eventTypePicker unit's eventTypeUri.
	EventTypeURI string `json:"eventTypeUri"`
}

// SchedulingLink is one single-use Calendly booking link. It accepts exactly one booking.
type SchedulingLink struct {
	// BookingURL is the link to send to the invitee.
	BookingURL string `json:"bookingUrl"`
	// EventTypeURI is the event type the link books.
	EventTypeURI string `json:"eventTypeUri"`
}

// CreateSchedulingLinkOperation implements the createSchedulingLink Mutation. Build it with
// Client.CreateSchedulingLink.
type CreateSchedulingLinkOperation struct{ client *Client }

// Definition returns the immutable createSchedulingLink operation definition.
func (CreateSchedulingLinkOperation) Definition() sdkgo.MutationDefinition {
	return CreateSchedulingLinkDefinition
}

// IdempotencyKey returns the call ID. Calendly accepts no idempotency key for scheduling links, which is
// why the operation runs with sync durability and reports an unknown outcome as uncertain.
func (CreateSchedulingLinkOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateSchedulingLinkInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke posts POST /scheduling_links once with max_event_count 1. A 201 selects created and a 4xx other
// than 429 selects providerRejected. A 429, or a request that never left this process, returns Retry. A 5xx,
// timeout, lost connection, or unreadable 201 after the request was written selects uncertain, because
// Calendly may have created a link that a repeated call would duplicate. The POST must answer at least three
// seconds before the Execute timeout, so a slow Calendly selects uncertain instead of a Dex retry, and a 401
// is not repeated after a token refresh.
func (operation CreateSchedulingLinkOperation) Invoke(call sdkgo.Call, input CreateSchedulingLinkInput) sdkgo.MutationAttempt[SchedulingLink] {
	attemptStart := time.Now()
	operationID := CreateSchedulingLinkDefinition.Operation.OperationID
	client := operation.client
	output := SchedulingLink{EventTypeURI: input.EventTypeURI}
	if _, err := parseEventTypeURI(input.EventTypeURI); err != nil {
		return sdkgo.NewMutationBranch(CreateSchedulingLinkBranchDefect, output,
			calendlyFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		return credentialMutationAttempt(operationID, CreateSchedulingLinkBranchDefect, output, err)
	}
	body, err := json.Marshal(map[string]any{"max_event_count": 1, "owner": input.EventTypeURI, "owner_type": "EventType"})
	if err != nil {
		return sdkgo.NewMutationBranch(CreateSchedulingLinkBranchDefect, output,
			calendlyFailurePointer(operationID, sdkgo.FailureLocalDefect, errCalendlyRequestInvalid.Error()), sdkgo.Receipt{})
	}
	requestContext, cancel, isStartable := newSchedulingLinkRequestContext(call.Context, attemptStart)
	defer cancel()
	if !isStartable {
		return sdkgo.NewMutationRetry[SchedulingLink](calendlyFailure(operationID, sdkgo.FailureAvailability,
			"too little of the Execute timeout remained to send the scheduling link request"), 0)
	}
	var isDispatched atomic.Bool
	response, err := client.sendCalendlyRequestOnce(requestContext, credentials.AccessToken.Reveal(), http.MethodPost,
		calendlyAPIBaseURL+"/scheduling_links", body, &isDispatched)
	if err != nil {
		switch {
		case errors.Is(err, errCalendlyRequestInvalid):
			return sdkgo.NewMutationBranch(CreateSchedulingLinkBranchDefect, output,
				calendlyFailurePointer(operationID, sdkgo.FailureLocalDefect, errCalendlyRequestInvalid.Error()), sdkgo.Receipt{})
		case errors.Is(err, errCalendlyResponseTooLarge):
			return sdkgo.NewMutationUncertain(output, calendlyFailure(operationID, sdkgo.FailureResponseTooLarge,
				"Calendly created a scheduling link but its answer exceeds maxResponseBytes"), client.calendlyReceipt(""))
		case !isDispatched.Load():
			return sdkgo.NewMutationRetry[SchedulingLink](calendlyFailure(operationID, sdkgo.FailureAvailability, "Calendly could not be reached"), 0)
		default:
			return sdkgo.NewMutationUncertain(output, calendlyFailure(operationID, sdkgo.FailureTransport,
				"the scheduling link request was sent but Calendly's answer was lost; a link may exist"), client.calendlyReceipt(""))
		}
	}
	if response.statusCode == http.StatusCreated || response.statusCode == http.StatusOK {
		link, err := decodeSchedulingLink(response.body, input.EventTypeURI)
		if err != nil {
			return sdkgo.NewMutationUncertain(output, calendlyFailure(operationID, sdkgo.FailureProtocol,
				"Calendly created a scheduling link but its answer is malformed"), client.calendlyReceipt(""))
		}
		return sdkgo.NewMutationBranch(CreateSchedulingLinkBranchCreated, link, nil, client.calendlyReceipt(link.BookingURL))
	}
	outcome := client.classifyCalendlyFailure(operationID, response)
	switch {
	case response.statusCode >= 500:
		return sdkgo.NewMutationUncertain(output, calendlyFailure(operationID, sdkgo.FailureAvailability,
			"Calendly answered a server error after the scheduling link request was sent; a link may exist"), client.calendlyReceipt(""))
	case outcome.isRetry:
		return sdkgo.NewMutationRetry[SchedulingLink](outcome.failure, outcome.retryAfter)
	default:
		return sdkgo.NewMutationBranch(CreateSchedulingLinkBranchProviderRejected, output, &outcome.failure, client.calendlyReceipt(""))
	}
}

// newSchedulingLinkRequestContext ends the POST before the attempt's deadline, or before the default Execute
// timeout from attemptStart when the context has none; false means too little time is left.
func newSchedulingLinkRequestContext(stepContext context.Context, attemptStart time.Time) (context.Context, context.CancelFunc, bool) {
	if stepContext == nil {
		stepContext = context.Background()
	}
	deadline := attemptStart.Add(CreateSchedulingLinkDefinition.StepDefaults.ExecuteMethodTimeout)
	if contextDeadline, hasDeadline := stepContext.Deadline(); hasDeadline && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	requestDeadline := deadline.Add(-schedulingLinkDeadlineMargin)
	if time.Until(requestDeadline) < minimumSchedulingLinkRequestTime {
		return stepContext, func() {}, false
	}
	requestContext, cancel := context.WithDeadline(stepContext, requestDeadline)
	return requestContext, cancel, true
}

// decodeSchedulingLink requires an absolute HTTPS booking URL for the requested event type.
func decodeSchedulingLink(body []byte, eventTypeURI string) (SchedulingLink, error) {
	var decoded struct {
		Resource struct {
			BookingURL string `json:"booking_url"`
			Owner      string `json:"owner"`
		} `json:"resource"`
	}
	if err := decodeCalendlyJSON(body, &decoded); err != nil {
		return SchedulingLink{}, err
	}
	bookingURL, err := url.Parse(decoded.Resource.BookingURL)
	if err != nil || bookingURL.Scheme != "https" || bookingURL.Hostname() == "" || bookingURL.User != nil {
		return SchedulingLink{}, errCalendlyResponseMalformed
	}
	if decoded.Resource.Owner != "" && decoded.Resource.Owner != eventTypeURI {
		return SchedulingLink{}, errCalendlyResponseMalformed
	}
	return SchedulingLink{BookingURL: decoded.Resource.BookingURL, EventTypeURI: eventTypeURI}, nil
}
