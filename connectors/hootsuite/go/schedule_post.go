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
	"sync/atomic"
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

// Invoke schedules at most once and returns uncertain when a submitted request has no confirmed ID.
func (operation SchedulePostOperation) Invoke(call sdkgo.Call, input SchedulePostInput) sdkgo.MutationAttempt[ScheduledPostReceipt] {
	fence, err := singledispatch.NewFence(call, SchedulePostDefinition.StepDefaults.ExecuteMethodTimeout)
	if err != nil {
		return scheduleFailure(SchedulePostBranchDefect, sdkgo.FailureLocalDefect, "Hootsuite dispatch fence is unavailable")
	}
	if fence.Decide().Kind != singledispatch.SendAllowed {
		return scheduleUncertain(sdkgo.FailureTransport, "Hootsuite schedule outcome cannot be confirmed; inspect Planner before resending")
	}
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
	credentials, err := sdkgo.ResolveCredential(call.Context, operation.client.credentials, call, operation.client.refreshDriver)
	if sdkgo.IsReauthorizationRequired(err) {
		return scheduleFailure(SchedulePostBranchDefect, sdkgo.FailureAuthentication, "connection credentials require reauthorization")
	}
	if err != nil {
		return scheduleRetry(sdkgo.FailureTransport, "connection refresh failed", 0)
	}
	if credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return scheduleFailure(SchedulePostBranchDefect, sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/v1/messages", bytes.NewReader(payload))
	if err != nil {
		return scheduleFailure(SchedulePostBranchDefect, sdkgo.FailureLocalDefect, "schedule request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json;charset=utf-8")
	request.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	request.Header.Set("User-Agent", "Dex Connector (https://superdurable.io)")
	if err := singledispatch.PrepareHTTPWrite(request); err != nil {
		return scheduleFailure(SchedulePostBranchDefect, sdkgo.FailureLocalDefect, "schedule request is invalid")
	}
	var didGetConnection atomic.Bool
	var response *http.Response
	err = fence.Dispatch(func(requestContext context.Context) error {
		trace := &httptrace.ClientTrace{GotConn: func(httptrace.GotConnInfo) { didGetConnection.Store(true) }}
		request = request.WithContext(httptrace.WithClientTrace(requestContext, trace))
		response, err = operation.client.httpClient.Do(request)
		return err
	})
	if err != nil {
		if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
			return scheduleRetry(sdkgo.FailureTransport, "Hootsuite dispatch did not start", 0)
		}
		if !didGetConnection.Load() {
			if checkpointErr := fence.RecordCanResend(); checkpointErr == nil {
				return scheduleRetry(sdkgo.FailureTransport, "Hootsuite could not be reached before sending", 0)
			}
		}
		return scheduleUncertain(sdkgo.FailureTransport, "Hootsuite schedule outcome is unknown")
	}
	// Closing the body cannot change the provider's completed decision.
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusTooManyRequests || response.StatusCode == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return scheduleRetry(sdkgo.FailureAvailability, "Hootsuite retry checkpoint is unavailable", 0)
		}
		failureKind := sdkgo.FailureAvailability
		if response.StatusCode == http.StatusTooManyRequests {
			failureKind = sdkgo.FailureRateLimit
		}
		return scheduleRetry(failureKind, "Hootsuite did not schedule the message", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode >= 500 || response.StatusCode == http.StatusRequestTimeout {
		return scheduleUncertain(sdkgo.FailureAvailability, "Hootsuite schedule outcome is unknown")
	}
	if response.StatusCode >= 400 && response.StatusCode < 500 {
		return scheduleFailure(SchedulePostBranchProviderRejected, sdkgo.FailureProviderRejection, "Hootsuite rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if response.StatusCode != http.StatusOK {
		return scheduleUncertain(sdkgo.FailureProtocol, "Hootsuite returned an unexpected schedule response")
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if err != nil {
		return scheduleUncertain(sdkgo.FailureTransport, "Hootsuite accepted the request but its response could not be read")
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
	if err := json.Unmarshal(body, &wire); err != nil || len(wire.Data) != 1 || wire.Data[0].ID == "" || wire.Data[0].State == "" {
		return scheduleUncertain(sdkgo.FailureProtocol, "Hootsuite accepted the request but returned no usable message ID")
	}
	if wire.Data[0].SocialProfile.ID != "" && wire.Data[0].SocialProfile.ID != input.SocialProfileID {
		return scheduleUncertain(sdkgo.FailureProtocol, "Hootsuite returned an unexpected social profile")
	}
	// A failed ID heartbeat does not permit a second dispatch.
	_ = fence.RecordProviderID(wire.Data[0].ID)
	return sdkgo.NewMutationBranch(SchedulePostBranchScheduled, ScheduledPostReceipt{ID: wire.Data[0].ID, State: wire.Data[0].State, SocialProfileID: input.SocialProfileID}, nil, sdkgo.Receipt{})
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
