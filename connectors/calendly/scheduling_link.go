// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

type schedulingLinkSendAttempt struct {
	client      *Client
	accessToken string
	body        []byte
	observation schedulingLinkDispatchObservation
	response    calendlyResponse
}

func (attempt *schedulingLinkSendAttempt) send(ctx context.Context) error {
	var err error
	attempt.response, err = attempt.client.sendCalendlyRequestOnce(ctx, attempt.accessToken,
		http.MethodPost, calendlyAPIBaseURL+"/scheduling_links", attempt.body, &attempt.observation)
	return err
}

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

// Invoke creates one single-use link. The fence permits another POST only after a proven unprocessed request.
func (operation CreateSchedulingLinkOperation) Invoke(call sdkgo.Call, input CreateSchedulingLinkInput) sdkgo.MutationAttempt[SchedulingLink] {
	operationID := CreateSchedulingLinkDefinition.Operation.OperationID
	client := operation.client
	output := SchedulingLink{EventTypeURI: input.EventTypeURI}
	// Calendly can take most of its 25-second HTTP timeout; no provider read-back exists.
	fence, err := singledispatch.NewFence(call, CreateSchedulingLinkDefinition.StepDefaults.ExecuteMethodTimeout,
		singledispatch.WithWriteWindow(5.0/6.0))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateSchedulingLinkBranchDefect, output,
			calendlyFailurePointer(operationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	}
	switch decision := fence.Decide(); decision.Kind {
	case singledispatch.FailClosed:
		return uncertainSchedulingLink(client, operationID, output, sdkgo.FailureProtocol,
			"the scheduling link dispatch checkpoint cannot be trusted")
	case singledispatch.ReadBackRequired:
		if decision.Marker.CallID != call.ID {
			return uncertainSchedulingLink(client, operationID, output, sdkgo.FailureProtocol,
				"the scheduling link checkpoint belongs to another call")
		}
		return uncertainSchedulingLink(client, operationID, output, sdkgo.FailureTransport,
			"a prior scheduling link request may have been accepted, and Calendly offers no link read-back")
	}
	if _, err := parseEventTypeURI(input.EventTypeURI); err != nil {
		return sdkgo.NewMutationBranch(CreateSchedulingLinkBranchDefect, output,
			calendlyFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil {
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[SchedulingLink](calendlyFailure(operationID, sdkgo.FailureAvailability,
				"Calendly credentials and a safe retry checkpoint are unavailable"), 0)
		}
		return sdkgo.NewMutationRetry[SchedulingLink](calendlyFailure(operationID, sdkgo.FailureAvailability,
			"Calendly credentials are unavailable before the scheduling link was sent"), 0)
	}
	body, err := json.Marshal(map[string]any{"max_event_count": 1, "owner": input.EventTypeURI, "owner_type": "EventType"})
	if err != nil {
		return sdkgo.NewMutationBranch(CreateSchedulingLinkBranchDefect, output,
			calendlyFailurePointer(operationID, sdkgo.FailureLocalDefect, errCalendlyRequestInvalid.Error()), sdkgo.Receipt{})
	}
	attempt := &schedulingLinkSendAttempt{client: client, accessToken: credentials.AccessToken.Reveal(), body: body}
	err = fence.Dispatch(attempt.send)
	response := attempt.response
	if err != nil {
		switch {
		case errors.Is(err, singledispatch.ErrCheckpoint), errors.Is(err, singledispatch.ErrInsufficientTime):
			return sdkgo.NewMutationRetry[SchedulingLink](calendlyFailure(operationID, sdkgo.FailureAvailability,
				"the scheduling link request was not sent before its safe deadline"), 0)
		case response.statusCode == http.StatusCreated || response.statusCode == http.StatusOK:
			return uncertainSchedulingLink(client, operationID, output, responseFailureKind(err),
				"Calendly accepted a scheduling link but its answer cannot be read")
		case errors.Is(err, errCalendlyRequestInvalid):
			return sdkgo.NewMutationBranch(CreateSchedulingLinkBranchDefect, output,
				calendlyFailurePointer(operationID, sdkgo.FailureLocalDefect, errCalendlyRequestInvalid.Error()), sdkgo.Receipt{})
		case errors.Is(err, errCalendlyResponseTooLarge):
			return uncertainSchedulingLink(client, operationID, output, sdkgo.FailureResponseTooLarge,
				"Calendly's scheduling link response exceeds maxResponseBytes")
		case attempt.observation.isProvablyUndispatched():
			if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
				return sdkgo.NewMutationRetry[SchedulingLink](calendlyFailure(operationID, sdkgo.FailureAvailability,
					"Calendly could not be reached and the retry checkpoint failed"), 0)
			}
			return sdkgo.NewMutationRetry[SchedulingLink](calendlyFailure(operationID, sdkgo.FailureAvailability,
				"Calendly could not be reached before a connection was obtained"), 0)
		default:
			return uncertainSchedulingLink(client, operationID, output, sdkgo.FailureTransport,
				"Calendly's scheduling link answer was lost after the request may have been sent")
		}
	}
	if response.statusCode == http.StatusCreated || response.statusCode == http.StatusOK {
		link, err := decodeSchedulingLink(response.body, input.EventTypeURI)
		if err != nil {
			return uncertainSchedulingLink(client, operationID, output, sdkgo.FailureProtocol,
				"Calendly created a scheduling link but its answer is malformed")
		}
		_ = fence.RecordProviderID(link.BookingURL)
		return sdkgo.NewMutationBranch(CreateSchedulingLinkBranchCreated, link, nil, client.calendlyReceipt(link.BookingURL))
	}
	outcome := client.classifyCalendlyFailure(operationID, response)
	switch {
	case response.statusCode == http.StatusTooManyRequests || response.statusCode == http.StatusServiceUnavailable:
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[SchedulingLink](calendlyFailure(operationID, sdkgo.FailureAvailability,
				"Calendly declined the request but the retry checkpoint failed"), 0)
		}
		return sdkgo.NewMutationRetry[SchedulingLink](outcome.failure, outcome.retryAfter)
	case response.statusCode == http.StatusConflict:
		return uncertainSchedulingLink(client, operationID, output, sdkgo.FailureConflict,
			"Calendly reported a link conflict without a way to identify the existing link")
	case response.statusCode >= 500:
		return uncertainSchedulingLink(client, operationID, output, sdkgo.FailureAvailability,
			"Calendly's response does not prove whether a scheduling link was created")
	default:
		return sdkgo.NewMutationBranch(CreateSchedulingLinkBranchProviderRejected, output, &outcome.failure, client.calendlyReceipt(""))
	}
}

func uncertainSchedulingLink(client *Client, operationID string, output SchedulingLink, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[SchedulingLink] {
	return sdkgo.NewMutationUncertain(output, calendlyFailure(operationID, kind, message), client.calendlyReceipt(""))
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
