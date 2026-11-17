// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hootsuite

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptrace"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// SchedulePostInput schedules one text message on one social profile.
type SchedulePostInput struct {
	// Text is the message body; Hootsuite applies network-specific length restrictions.
	Text string `json:"text"`
	// SocialProfileID is a stable ID returned by listSocialProfiles.
	SocialProfileID string `json:"socialProfileId"`
	// ScheduledSendTime is an ISO-8601 UTC instant ending in Z.
	ScheduledSendTime string `json:"scheduledSendTime"`
}

// ScheduledPostReceipt identifies a message accepted by Hootsuite.
type ScheduledPostReceipt struct {
	// ID is Hootsuite's stable message ID.
	ID string `json:"id"`
	// State is the state Hootsuite assigned to the message.
	State string `json:"state"`
	// SocialProfileID is the requested destination profile.
	SocialProfileID string `json:"socialProfileId"`
}

// SchedulePostOperation creates a scheduled message behind a durable single-dispatch fence.
type SchedulePostOperation struct{ client *Client }

// SchedulePost binds the scheduled message Mutation to this client.
func (client *Client) SchedulePost() SchedulePostOperation { return SchedulePostOperation{client} }

// Definition returns the manifest-generated schedule Mutation contract.
func (SchedulePostOperation) Definition() sdkgo.MutationDefinition { return SchedulePostDefinition }

// IdempotencyKey records the call identity; Hootsuite does not deduplicate it.
func (SchedulePostOperation) IdempotencyKey(callID sdkgo.CallID, _ SchedulePostInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke schedules under a single-dispatch fence. Hootsuite has no message idempotency key, so an
// unknown outcome resends while possible sends stay below call.MaxSends() and selects uncertain at
// the cap. A definite refusal is never resent.
func (operation SchedulePostOperation) Invoke(call sdkgo.Call, input SchedulePostInput) sdkgo.MutationAttempt[ScheduledPostReceipt] {
	if strings.TrimSpace(input.Text) == "" || !utf8.ValidString(input.Text) || len(input.Text) > 10000 ||
		!numericSocialProfileID(input.SocialProfileID) || !validScheduleTime(input.ScheduledSendTime) {
		return scheduleFailure(SchedulePostBranchDefect, sdkgo.FailureValidation, "scheduled message text, profile ID, or UTC time is invalid")
	}
	payload, err := json.Marshal(struct {
		Text              string   `json:"text"`
		SocialProfileIDs  []string `json:"socialProfileIds"`
		ScheduledSendTime string   `json:"scheduledSendTime"`
	}{Text: input.Text, SocialProfileIDs: []string{input.SocialProfileID}, ScheduledSendTime: input.ScheduledSendTime})
	if err != nil {
		return scheduleFailure(SchedulePostBranchDefect, sdkgo.FailureValidation, "scheduled message could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, SchedulePostDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return scheduleFailure(SchedulePostBranchDefect, sdkgo.FailureLocalDefect, "Hootsuite dispatch fence is unavailable")
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return scheduleRetry(sdkgo.FailureAvailability, "Hootsuite dispatch checkpoint is unreadable; no message was scheduled in this attempt", 0)
	case singledispatch.ReadBackRequired:
		return scheduleUncertain(sdkgo.FailureTransport, "an earlier attempt may have scheduled the Hootsuite message, and the send cap is reached; inspect Planner before resending")
	case singledispatch.ReadBackThenResendAllowed:
		// A saved message ID proves Hootsuite scheduled the message, so it is never resent.
		if decision.Marker.ProviderID != "" {
			return scheduleUncertain(sdkgo.FailureTransport, "an earlier attempt scheduled the Hootsuite message, but its confirmation was lost")
		}
	}
	// This attempt's send would be the last one the cap allows when no further send remains.
	canResendUnknownOutcome := decision.MaximumPriorSends+1 < call.MaxSends()
	// An earlier send may have scheduled the message, so a refusal of this resend cannot prove nothing changed.
	hasPossibleEarlierSend := decision.MaximumPriorSends > 0
	credentials, err := sdkgo.ResolveCredential(call.Context, operation.client.credentials, call, operation.client.refreshDriver)
	if sdkgo.IsReauthorizationRequired(err) {
		return localScheduleFailure(fence, hasPossibleEarlierSend, sdkgo.FailureAuthentication, "connection credentials require reauthorization")
	}
	if err != nil {
		// A notSent checkpoint keeps this pre-send retry out of the send count when no send preceded it.
		_ = fence.RecordNotSent()
		return scheduleRetry(sdkgo.FailureTransport, "connection refresh failed", 0)
	}
	if credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return localScheduleFailure(fence, hasPossibleEarlierSend, sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return localScheduleFailure(fence, hasPossibleEarlierSend, sdkgo.FailureLocalDefect, "schedule request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json;charset=utf-8")
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return localScheduleFailure(fence, hasPossibleEarlierSend, sdkgo.FailureLocalDefect, "schedule request is invalid")
	}
	observation := &dispatchObservation{}
	var response *http.Response
	err = fence.Dispatch(func(requestContext context.Context) error {
		request = request.WithContext(httptrace.WithClientTrace(requestContext, observation.clientTrace()))
		var sendErr error
		response, sendErr = operation.client.httpClient.Do(request)
		return sendErr
	})
	if err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return scheduleRetry(sdkgo.FailureTransport, "Hootsuite dispatch did not start", 0)
		}
		if observation.isProvablyUndispatched() {
			// A lost canResend checkpoint leaves the claim, which the next attempt counts as a send.
			_ = fence.RecordCanResend()
			return scheduleRetry(sdkgo.FailureTransport, "Hootsuite could not be reached before sending", 0)
		}
		return resendOrSelectUncertain(canResendUnknownOutcome, sdkgo.FailureTransport, "Hootsuite schedule outcome is unknown", 0)
	}
	// Closing the body cannot change the provider's completed decision.
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return scheduleRetry(sdkgo.FailureAvailability, "Hootsuite retry checkpoint is unavailable", 0)
		}
		failureKind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			failureKind = sdkgo.FailureRateLimit
		}
		return scheduleRetry(failureKind, "Hootsuite did not schedule the message", retryAfter)
	}
	isDefiniteRefusalStatus := isDefiniteRefusalStatus(response.StatusCode)
	isSuccessStatus := response.StatusCode >= 200 && response.StatusCode < 300
	if !isDefiniteRefusalStatus && !isSuccessStatus {
		return resendOrSelectUncertain(canResendUnknownOutcome, sdkgo.FailureAvailability, "Hootsuite schedule outcome is unknown after HTTP "+strconv.Itoa(response.StatusCode), retryAfter)
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return resendOrSelectUncertain(canResendUnknownOutcome, sdkgo.FailureTransport, "Hootsuite schedule response could not be read", 0)
	}
	var wire struct {
		Data []struct {
			ID            string `json:"id"`
			State         string `json:"state"`
			SocialProfile struct {
				ID string `json:"id"`
			} `json:"socialProfile"`
		} `json:"data"`
	}
	isWireDecoded := json.Unmarshal(body, &wire) == nil
	// A message ID in the body means Hootsuite scheduled the message.
	isAccepted := isWireDecoded && len(wire.Data) > 0 && wire.Data[0].ID != ""
	if isDefiniteRefusalStatus {
		if isAccepted {
			return scheduleUncertain(sdkgo.FailureProtocol, "Hootsuite returned a contradictory schedule response")
		}
		return rejectOrSelectUncertain(fence, hasPossibleEarlierSend, rejectionFailureKind(response.StatusCode), "Hootsuite rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if !isAccepted {
		return resendOrSelectUncertain(canResendUnknownOutcome, sdkgo.FailureProtocol, "Hootsuite schedule response is malformed", 0)
	}
	// A lost provider-ID checkpoint leaves the claim, which still counts this send.
	_ = fence.RecordProviderID(wire.Data[0].ID)
	if response.StatusCode != http.StatusOK || len(wire.Data) != 1 || wire.Data[0].State == "" {
		// Hootsuite reported a message ID, so a resend could duplicate the message.
		return scheduleUncertain(sdkgo.FailureProtocol, "Hootsuite accepted the request but returned an unexpected schedule response")
	}
	if wire.Data[0].SocialProfile.ID != "" && wire.Data[0].SocialProfile.ID != input.SocialProfileID {
		return scheduleUncertain(sdkgo.FailureProtocol, "Hootsuite returned an unexpected social profile")
	}
	return sdkgo.NewMutationBranch(SchedulePostBranchScheduled, ScheduledPostReceipt{ID: wire.Data[0].ID, State: wire.Data[0].State, SocialProfileID: input.SocialProfileID}, nil, sdkgo.Receipt{})
}

// isDefiniteRefusalStatus reports the HTTP statuses that prove Hootsuite refused the request.
func isDefiniteRefusalStatus(statusCode int) bool {
	switch statusCode {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound,
		http.StatusLengthRequired, http.StatusRequestEntityTooLarge, http.StatusRequestURITooLong,
		http.StatusUnsupportedMediaType, http.StatusUnprocessableEntity:
		return true
	}
	return false
}

func rejectionFailureKind(statusCode int) sdkgo.FailureKind {
	switch statusCode {
	case http.StatusUnauthorized:
		return sdkgo.FailureAuthentication
	case http.StatusForbidden:
		return sdkgo.FailureAuthorization
	}
	return sdkgo.FailureProviderRejection
}

// resendOrSelectUncertain retries a possibly scheduled message while the send cap allows another send.
func resendOrSelectUncertain(canResendUnknownOutcome bool, kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[ScheduledPostReceipt] {
	if canResendUnknownOutcome {
		// The claim stays the last marker, so the next attempt counts this send.
		return scheduleRetry(kind, message, retryAfter)
	}
	return scheduleUncertain(kind, message)
}

// rejectOrSelectUncertain selects providerRejected only when no earlier send of this call may have scheduled the message.
func rejectOrSelectUncertain(fence *singledispatch.Fence, hasPossibleEarlierSend bool, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ScheduledPostReceipt] {
	if hasPossibleEarlierSend {
		return scheduleUncertain(kind, message)
	}
	// The branch is terminal, so a lost canResend checkpoint cannot cause a resend.
	_ = fence.RecordCanResend()
	return scheduleFailure(SchedulePostBranchProviderRejected, kind, message)
}

// localScheduleFailure selects defect only when no earlier send may have scheduled the message.
func localScheduleFailure(fence *singledispatch.Fence, hasPossibleEarlierSend bool, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ScheduledPostReceipt] {
	if hasPossibleEarlierSend {
		// notSent would hide the earlier possible send, so nothing is recorded.
		return scheduleRetry(kind, message+"; no message was scheduled in this attempt", 0)
	}
	// The branch is terminal, so a lost notSent checkpoint cannot cause a resend.
	_ = fence.RecordNotSent()
	return scheduleFailure(SchedulePostBranchDefect, kind, message)
}

func numericSocialProfileID(id string) bool {
	if id == "" || len(id) > 40 {
		return false
	}
	for _, digit := range id {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func validScheduleTime(value string) bool {
	if len(value) == 0 || value[len(value)-1] != 'Z' {
		return false
	}
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}

func scheduleFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ScheduledPostReceipt] {
	failure := sdkgo.Failure{Kind: kind, Provider: "hootsuite", Operation: "schedulePost", Message: message}
	return sdkgo.NewMutationBranch(branch, ScheduledPostReceipt{}, &failure, sdkgo.Receipt{})
}

func scheduleRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[ScheduledPostReceipt] {
	failure := sdkgo.Failure{Kind: kind, Provider: "hootsuite", Operation: "schedulePost", Message: message}
	return sdkgo.NewMutationRetry[ScheduledPostReceipt](failure, retryAfter)
}

func scheduleUncertain(kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[ScheduledPostReceipt] {
	failure := sdkgo.Failure{Kind: kind, Provider: "hootsuite", Operation: "schedulePost", Message: message}
	return sdkgo.NewMutationUncertain(ScheduledPostReceipt{}, failure, sdkgo.Receipt{})
}
