// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
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

// Invoke posts one single-use link. A usable 2xx selects created; an unusable 2xx selects uncertain.
// A definite refusal (400, 401, 403, 404, 411, 413, 414, 415, or 422) of the first send selects
// providerRejected; 401 is not retried after token refresh. A 429, 503, or provably unsent request
// retries after a safe resend checkpoint. Any other outcome, such as a lost answer, a 5xx, or a 409,
// returns Retry while possible sends stay below the send cap and selects uncertain at the cap. A saved
// booking URL from an accepted earlier send selects created without another POST.
func (operation CreateSchedulingLinkOperation) Invoke(call sdkgo.Call, input CreateSchedulingLinkInput) sdkgo.MutationAttempt[SchedulingLink] {
	operationID := CreateSchedulingLinkDefinition.Operation.OperationID
	client := operation.client
	output := SchedulingLink{EventTypeURI: input.EventTypeURI}
	// Calendly can take most of its 25-second HTTP timeout; no provider read-back exists.
	fence, err := singledispatch.NewFence(call, CreateSchedulingLinkDefinition.StepDefaults.ExecuteMethodTimeout,
		singledispatch.WithWriteWindow(5.0/6.0), singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateSchedulingLinkBranchDefect, output,
			calendlyFailurePointer(operationID, sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationRetry[SchedulingLink](calendlyFailure(operationID, sdkgo.FailureAvailability,
			"the scheduling link dispatch checkpoint could not be read; nothing was sent"), 0)
	}
	if decision.Kind != singledispatch.SendAllowed && decision.Marker.CallID == call.ID && decision.Marker.ProviderID != "" {
		// Calendly returned this booking URL for an accepted earlier send, so it is never resent.
		if link, err := decodeSchedulingLinkBookingURL(decision.Marker.ProviderID, input.EventTypeURI); err == nil {
			return sdkgo.NewMutationBranch(CreateSchedulingLinkBranchCreated, link, nil, client.calendlyReceipt(link.BookingURL))
		}
		return uncertainSchedulingLink(client, operationID, output, sdkgo.FailureProtocol,
			"the saved scheduling link checkpoint is invalid")
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return uncertainSchedulingLink(client, operationID, output, sdkgo.FailureTransport,
			"earlier scheduling link requests up to the send cap may have been accepted, and Calendly offers no link read-back")
	}
	canResendUnknownOutcome := decision.MaximumPriorSends+1 < call.MaxSends()
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	if _, err := parseEventTypeURI(input.EventTypeURI); err != nil {
		return sdkgo.NewMutationBranch(CreateSchedulingLinkBranchDefect, output,
			calendlyFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, err := client.resolveCredentials(call)
	if err != nil && decision.Kind == singledispatch.ReadBackThenResendAllowed {
		// notSent would hide the earlier possible send, so nothing is recorded.
		if errors.Is(err, errCalendlyCredentialsUnavailable) {
			return sdkgo.NewMutationRetry[SchedulingLink](calendlyFailure(operationID, sdkgo.FailureAvailability, err.Error()), 0)
		}
		return uncertainSchedulingLink(client, operationID, output, sdkgo.FailureAuthentication,
			"an earlier scheduling link request may have been accepted, and the connection credentials are unavailable to resend it; reconnect the connection")
	}
	if err != nil {
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil {
			return sdkgo.NewMutationRetry[SchedulingLink](calendlyFailure(operationID, sdkgo.FailureAvailability,
				"Calendly credentials and a safe retry checkpoint are unavailable"), 0)
		}
		return credentialMutationAttempt(operationID, CreateSchedulingLinkBranchDefect, output, err)
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
		case isAcceptedCalendlyResponse(response.statusCode):
			return uncertainSchedulingLink(client, operationID, output, responseFailureKind(err),
				"Calendly accepted a scheduling link but its answer cannot be read")
		case errors.Is(err, errCalendlyRequestInvalid) && !hasPossibleEarlierSend:
			return sdkgo.NewMutationBranch(CreateSchedulingLinkBranchDefect, output,
				calendlyFailurePointer(operationID, sdkgo.FailureLocalDefect, errCalendlyRequestInvalid.Error()), sdkgo.Receipt{})
		case errors.Is(err, errCalendlyResponseTooLarge):
			return resendOrSelectUncertainSchedulingLink(client, canResendUnknownOutcome, operationID, output,
				calendlyFailure(operationID, sdkgo.FailureResponseTooLarge, "Calendly's scheduling link response exceeds maxResponseBytes"), 0)
		case attempt.observation.isProvablyUndispatched():
			if checkpointErr := fence.RecordCanResend(); checkpointErr != nil {
				return sdkgo.NewMutationRetry[SchedulingLink](calendlyFailure(operationID, sdkgo.FailureAvailability,
					"Calendly could not be reached and the retry checkpoint failed"), 0)
			}
			return sdkgo.NewMutationRetry[SchedulingLink](calendlyFailure(operationID, sdkgo.FailureAvailability,
				"Calendly could not be reached before a connection was obtained"), 0)
		default:
			return resendOrSelectUncertainSchedulingLink(client, canResendUnknownOutcome, operationID, output,
				calendlyFailure(operationID, sdkgo.FailureTransport, "Calendly's scheduling link answer was lost after the request may have been sent"), 0)
		}
	}
	if isAcceptedCalendlyResponse(response.statusCode) {
		link, err := decodeSchedulingLink(response.body, input.EventTypeURI)
		if err != nil {
			return uncertainSchedulingLink(client, operationID, output, sdkgo.FailureProtocol,
				"Calendly created a scheduling link but its answer is malformed")
		}
		_ = fence.RecordProviderID(link.BookingURL) // The accepted result is returned even if its advisory checkpoint fails.
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
	case isDefiniteRefusalStatus(response.statusCode):
		// An earlier send may have created a link, so a refusal of this resend proves nothing.
		if hasPossibleEarlierSend {
			return sdkgo.NewMutationUncertain(output, outcome.failure, client.calendlyReceipt(""))
		}
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[SchedulingLink](calendlyFailure(operationID, sdkgo.FailureAvailability,
				"Calendly refused the request but the refusal checkpoint failed"), 0)
		}
		return sdkgo.NewMutationBranch(CreateSchedulingLinkBranchProviderRejected, output, &outcome.failure, client.calendlyReceipt(""))
	default:
		// A 5xx, 409, 408, or other status does not prove whether a link was created.
		return resendOrSelectUncertainSchedulingLink(client, canResendUnknownOutcome, operationID, output, outcome.failure, outcome.retryAfter)
	}
}

// resendOrSelectUncertainSchedulingLink retries a possibly sent POST while the send cap allows another send.
func resendOrSelectUncertainSchedulingLink(client *Client, canResendUnknownOutcome bool, operationID string, output SchedulingLink,
	failure sdkgo.Failure, retryAfter time.Duration) sdkgo.MutationAttempt[SchedulingLink] {
	if canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return sdkgo.NewMutationRetry[SchedulingLink](failure, retryAfter)
	}
	return sdkgo.NewMutationUncertain(output, failure, client.calendlyReceipt(""))
}

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

func isAcceptedCalendlyResponse(statusCode int) bool { return statusCode >= 200 && statusCode < 300 }

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
	if decoded.Resource.Owner != "" && decoded.Resource.Owner != eventTypeURI {
		return SchedulingLink{}, errCalendlyResponseMalformed
	}
	return decodeSchedulingLinkBookingURL(decoded.Resource.BookingURL, eventTypeURI)
}

// decodeSchedulingLinkBookingURL requires an absolute HTTPS booking URL without user information.
func decodeSchedulingLinkBookingURL(rawBookingURL string, eventTypeURI string) (SchedulingLink, error) {
	bookingURL, err := url.Parse(rawBookingURL)
	if err != nil || bookingURL.Scheme != "https" || bookingURL.Hostname() == "" || bookingURL.User != nil {
		return SchedulingLink{}, errCalendlyResponseMalformed
	}
	return SchedulingLink{BookingURL: rawBookingURL, EventTypeURI: eventTypeURI}, nil
}
