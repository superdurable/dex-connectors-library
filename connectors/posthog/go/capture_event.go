// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package posthog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// CaptureEventInput is one event with stable identity and time across retries.
type CaptureEventInput struct {
	// Event is a nonempty custom event name.
	Event string `json:"event"`
	// DistinctID is the stable person or anonymous identity.
	DistinctID string `json:"distinctId"`
	// Timestamp is the event time in RFC3339 format and must remain fixed across retries.
	Timestamp string `json:"timestamp"`
	// Properties contains non-secret string dimensions, excluding identity and person mutations.
	Properties map[string]string `json:"properties,omitempty"`
}

// CapturedEvent reports acceptance for processing, not confirmed ingestion.
type CapturedEvent struct {
	// EventID is the stable UUID sent to PostHog for retry deduplication.
	EventID string `json:"eventId"`
	// Accepted is true only when PostHog acknowledged the event without a quota limit.
	Accepted bool `json:"accepted"`
}

// CaptureEventOperation sends a deduplicated event to the public capture API.
type CaptureEventOperation struct{ client *Client }

// CaptureEvent binds event capture to this client.
func (client *Client) CaptureEvent() CaptureEventOperation { return CaptureEventOperation{client} }

// Definition returns the manifest-generated capture contract.
func (CaptureEventOperation) Definition() sdkgo.MutationDefinition { return CaptureEventDefinition }

// IdempotencyKey derives a stable UUID from Dex's call identity.
func (CaptureEventOperation) IdempotencyKey(callID sdkgo.CallID, _ CaptureEventInput) sdkgo.IdempotencyKey {
	digest := sha256.Sum256([]byte(callID))
	part := hex.EncodeToString(digest[:16])
	return sdkgo.IdempotencyKey(part[:8] + "-" + part[8:12] + "-" + part[12:16] + "-" + part[16:20] + "-" + part[20:])
}

// Invoke submits one event with a stable UUID and timestamp for safe retries.
func (operation CaptureEventOperation) Invoke(call sdkgo.Call, input CaptureEventInput) sdkgo.MutationAttempt[CapturedEvent] {
	if strings.TrimSpace(input.Event) == "" || strings.TrimSpace(input.DistinctID) == "" || !utf8.ValidString(input.Event) || !utf8.ValidString(input.DistinctID) || len(input.Event) > 256 || len(input.DistinctID) > 512 || len(call.IdempotencyKey) != 36 {
		return captureFailure(CaptureEventBranchDefect, sdkgo.FailureValidation, "event, distinct ID, or stable call identity is invalid")
	}
	timestamp, err := time.Parse(time.RFC3339Nano, input.Timestamp)
	if err != nil || timestamp.IsZero() {
		return captureFailure(CaptureEventBranchDefect, sdkgo.FailureValidation, "timestamp must be an RFC3339 instant")
	}
	properties := make(map[string]string, len(input.Properties))
	for key, value := range input.Properties {
		if strings.TrimSpace(key) == "" || !utf8.ValidString(key) || !utf8.ValidString(value) || len(key) > 128 || len(value) > 4096 || strings.HasPrefix(key, "$") || strings.EqualFold(key, "token") || strings.EqualFold(key, "api_key") || strings.EqualFold(key, "password") || strings.EqualFold(key, "secret") {
			return captureFailure(CaptureEventBranchDefect, sdkgo.FailureValidation, "event property is reserved or invalid")
		}
		properties[key] = value
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.ProjectToken.Reveal()) {
		return captureFailure(CaptureEventBranchDefect, sdkgo.FailureAuthentication, "PostHog project token is unavailable or invalid")
	}
	payload, err := json.Marshal(struct {
		Token      string            `json:"token"`
		Event      string            `json:"event"`
		DistinctID string            `json:"distinct_id"`
		Timestamp  string            `json:"timestamp"`
		UUID       string            `json:"uuid"`
		Properties map[string]string `json:"properties,omitempty"`
	}{credentials.ProjectToken.Reveal(), input.Event, input.DistinctID, timestamp.UTC().Format(time.RFC3339Nano), string(call.IdempotencyKey), properties})
	if err != nil || len(payload) > 1<<20 {
		return captureFailure(CaptureEventBranchDefect, sdkgo.FailureValidation, "event payload exceeds the limit")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.captureURL+"/i/v0/e/", bytes.NewReader(payload))
	if err != nil {
		return captureFailure(CaptureEventBranchDefect, sdkgo.FailureLocalDefect, "capture request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return captureRetry(sdkgo.FailureTransport, "PostHog capture response is unavailable", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return captureRetry(sdkgo.FailureRateLimit, "PostHog rate limited capture", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return captureRetry(sdkgo.FailureAvailability, "PostHog capture is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return captureFailure(CaptureEventBranchProviderRejected, posthogRejectionKind(response.StatusCode), "PostHog rejected capture with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return captureRetry(sdkgo.FailureResponseTooLarge, "PostHog capture confirmation exceeded the limit", 0)
	}
	if err != nil {
		return captureRetry(sdkgo.FailureTransport, "PostHog capture confirmation was interrupted", 0)
	}
	var wire struct {
		Status       string          `json:"status"`
		QuotaLimited json.RawMessage `json:"quota_limited"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Status != "Ok" {
		return captureRetry(sdkgo.FailureProtocol, "PostHog capture confirmation is unusable", 0)
	}
	quota := bytes.TrimSpace(wire.QuotaLimited)
	if len(quota) > 0 && !bytes.Equal(quota, []byte("null")) && !bytes.Equal(quota, []byte("[]")) && !bytes.Equal(quota, []byte("{}")) {
		return captureFailure(CaptureEventBranchQuotaLimited, sdkgo.FailureProviderRejection, "PostHog reported an event quota limit")
	}
	return sdkgo.NewMutationBranch(CaptureEventBranchAccepted, CapturedEvent{EventID: string(call.IdempotencyKey), Accepted: true}, nil, sdkgo.Receipt{})
}

func captureFailure(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CapturedEvent] {
	failure := sdkgo.Failure{Kind: kind, Provider: "posthog", Operation: "captureEvent", Message: message}
	return sdkgo.NewMutationBranch(branch, CapturedEvent{}, &failure, sdkgo.Receipt{})
}
func captureRetry(kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[CapturedEvent] {
	failure := sdkgo.Failure{Kind: kind, Provider: "posthog", Operation: "captureEvent", Message: message}
	return sdkgo.NewMutationRetry[CapturedEvent](failure, after)
}
