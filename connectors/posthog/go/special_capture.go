// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package posthog

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// CreateAliasInput associates an alias with an existing distinct ID.
type CreateAliasInput struct {
	// DistinctID is the existing, stable person identity.
	DistinctID string `json:"distinctId"`
	// Alias is the new identity to associate with the person.
	Alias string `json:"alias"`
	// Timestamp is the fixed event time in RFC3339 format.
	Timestamp string `json:"timestamp"`
}

// IdentifyPersonInput associates non-secret properties with a distinct ID.
type IdentifyPersonInput struct {
	// DistinctID is the stable person identity.
	DistinctID string `json:"distinctId"`
	// Timestamp is the fixed event time in RFC3339 format.
	Timestamp string `json:"timestamp"`
	// Properties are non-secret values to set on the person profile.
	Properties map[string]string `json:"properties"`
}

// TrackPageViewInput captures one page view.
type TrackPageViewInput struct {
	// DistinctID is the stable person or anonymous identity.
	DistinctID string `json:"distinctId"`
	// URL is an absolute HTTPS page URL without credentials, query, or fragment.
	URL string `json:"url"`
	// Timestamp is the fixed event time in RFC3339 format.
	Timestamp string `json:"timestamp"`
}

// TrackScreenViewInput captures one named screen view.
type TrackScreenViewInput struct {
	// DistinctID is the stable person or anonymous identity.
	DistinctID string `json:"distinctId"`
	// Name is the screen name shown in PostHog paths.
	Name string `json:"name"`
	// Timestamp is the fixed event time in RFC3339 format.
	Timestamp string `json:"timestamp"`
}

// CreateAliasOperation associates a new alias with an existing person.
type CreateAliasOperation struct{ client *Client }

// CreateAlias binds the alias Mutation to this client.
func (client *Client) CreateAlias() CreateAliasOperation { return CreateAliasOperation{client} }

// Definition returns the manifest-generated alias contract.
func (CreateAliasOperation) Definition() sdkgo.MutationDefinition { return CreateAliasDefinition }

// IdempotencyKey derives PostHog's stable event UUID from the Dex call.
func (CreateAliasOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateAliasInput) sdkgo.IdempotencyKey {
	return CaptureEventOperation{}.IdempotencyKey(callID, CaptureEventInput{})
}

// Invoke sends one alias event with a stable UUID.
func (operation CreateAliasOperation) Invoke(call sdkgo.Call, input CreateAliasInput) sdkgo.MutationAttempt[CapturedEvent] {
	branches := captureBranches{accepted: CreateAliasBranchAccepted, quotaLimited: CreateAliasBranchQuotaLimited, rejected: CreateAliasBranchProviderRejected, defect: CreateAliasBranchDefect}
	if !validCaptureIdentity(input.Alias) || input.Alias == input.DistinctID {
		return specialCaptureBranch("createAlias", branches.defect, sdkgo.FailureValidation, "alias is invalid or equals the distinct ID")
	}
	return operation.client.captureSpecial(call, "createAlias", "$create_alias", input.DistinctID, input.Timestamp, map[string]any{"alias": input.Alias}, branches)
}

// IdentifyPersonOperation updates a person's non-secret properties.
type IdentifyPersonOperation struct{ client *Client }

// IdentifyPerson binds the identify Mutation to this client.
func (client *Client) IdentifyPerson() IdentifyPersonOperation {
	return IdentifyPersonOperation{client}
}

// Definition returns the manifest-generated identify contract.
func (IdentifyPersonOperation) Definition() sdkgo.MutationDefinition { return IdentifyPersonDefinition }

// IdempotencyKey derives PostHog's stable event UUID from the Dex call.
func (IdentifyPersonOperation) IdempotencyKey(callID sdkgo.CallID, _ IdentifyPersonInput) sdkgo.IdempotencyKey {
	return CaptureEventOperation{}.IdempotencyKey(callID, CaptureEventInput{})
}

// Invoke sends a person-property update through PostHog's capture API.
func (operation IdentifyPersonOperation) Invoke(call sdkgo.Call, input IdentifyPersonInput) sdkgo.MutationAttempt[CapturedEvent] {
	branches := captureBranches{accepted: IdentifyPersonBranchAccepted, quotaLimited: IdentifyPersonBranchQuotaLimited, rejected: IdentifyPersonBranchProviderRejected, defect: IdentifyPersonBranchDefect}
	if len(input.Properties) == 0 || len(input.Properties) > 100 || !validPublicProperties(input.Properties) {
		return specialCaptureBranch("identifyPerson", branches.defect, sdkgo.FailureValidation, "person properties are empty or invalid")
	}
	return operation.client.captureSpecial(call, "identifyPerson", "$identify", input.DistinctID, input.Timestamp, map[string]any{"$set": input.Properties}, branches)
}

// TrackPageViewOperation captures a page view for PostHog Paths.
type TrackPageViewOperation struct{ client *Client }

// TrackPageView binds the page-view Mutation to this client.
func (client *Client) TrackPageView() TrackPageViewOperation { return TrackPageViewOperation{client} }

// Definition returns the manifest-generated page-view contract.
func (TrackPageViewOperation) Definition() sdkgo.MutationDefinition { return TrackPageViewDefinition }

// IdempotencyKey derives PostHog's stable event UUID from the Dex call.
func (TrackPageViewOperation) IdempotencyKey(callID sdkgo.CallID, _ TrackPageViewInput) sdkgo.IdempotencyKey {
	return CaptureEventOperation{}.IdempotencyKey(callID, CaptureEventInput{})
}

// Invoke sends a page view with a URL that excludes query secrets.
func (operation TrackPageViewOperation) Invoke(call sdkgo.Call, input TrackPageViewInput) sdkgo.MutationAttempt[CapturedEvent] {
	branches := captureBranches{accepted: TrackPageViewBranchAccepted, quotaLimited: TrackPageViewBranchQuotaLimited, rejected: TrackPageViewBranchProviderRejected, defect: TrackPageViewBranchDefect}
	pageURL, err := url.Parse(input.URL)
	if err != nil || pageURL.Scheme != "https" || pageURL.Host == "" || pageURL.User != nil || pageURL.RawQuery != "" || pageURL.Fragment != "" || len(input.URL) > 2048 {
		return specialCaptureBranch("trackPageView", branches.defect, sdkgo.FailureValidation, "page URL must be HTTPS without credentials, query, or fragment")
	}
	return operation.client.captureSpecial(call, "trackPageView", "$pageview", input.DistinctID, input.Timestamp, map[string]any{"$current_url": input.URL}, branches)
}

// TrackScreenViewOperation captures a named screen view for PostHog Paths.
type TrackScreenViewOperation struct{ client *Client }

// TrackScreenView binds the screen-view Mutation to this client.
func (client *Client) TrackScreenView() TrackScreenViewOperation {
	return TrackScreenViewOperation{client}
}

// Definition returns the manifest-generated screen-view contract.
func (TrackScreenViewOperation) Definition() sdkgo.MutationDefinition {
	return TrackScreenViewDefinition
}

// IdempotencyKey derives PostHog's stable event UUID from the Dex call.
func (TrackScreenViewOperation) IdempotencyKey(callID sdkgo.CallID, _ TrackScreenViewInput) sdkgo.IdempotencyKey {
	return CaptureEventOperation{}.IdempotencyKey(callID, CaptureEventInput{})
}

// Invoke sends a named screen view with a stable UUID.
func (operation TrackScreenViewOperation) Invoke(call sdkgo.Call, input TrackScreenViewInput) sdkgo.MutationAttempt[CapturedEvent] {
	branches := captureBranches{accepted: TrackScreenViewBranchAccepted, quotaLimited: TrackScreenViewBranchQuotaLimited, rejected: TrackScreenViewBranchProviderRejected, defect: TrackScreenViewBranchDefect}
	if strings.TrimSpace(input.Name) == "" || !utf8.ValidString(input.Name) || len(input.Name) > 256 {
		return specialCaptureBranch("trackScreenView", branches.defect, sdkgo.FailureValidation, "screen name is invalid")
	}
	return operation.client.captureSpecial(call, "trackScreenView", "$screen", input.DistinctID, input.Timestamp, map[string]any{"$screen_name": input.Name}, branches)
}

type captureBranches struct {
	accepted     sdkgo.BranchID
	quotaLimited sdkgo.BranchID
	rejected     sdkgo.BranchID
	defect       sdkgo.BranchID
}

func (client *Client) captureSpecial(call sdkgo.Call, operationName, event, distinctID, timestampText string, properties map[string]any, branches captureBranches) sdkgo.MutationAttempt[CapturedEvent] {
	if !validCaptureIdentity(distinctID) || len(call.IdempotencyKey) != 36 {
		return specialCaptureBranch(operationName, branches.defect, sdkgo.FailureValidation, "distinct ID or stable call identity is invalid")
	}
	timestamp, err := time.Parse(time.RFC3339Nano, timestampText)
	if err != nil || timestamp.IsZero() {
		return specialCaptureBranch(operationName, branches.defect, sdkgo.FailureValidation, "timestamp must be an RFC3339 instant")
	}
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.ProjectToken.Reveal()) {
		return specialCaptureBranch(operationName, branches.defect, sdkgo.FailureAuthentication, "PostHog project token is unavailable or invalid")
	}
	payload, err := json.Marshal(struct {
		Token      string         `json:"token"`
		Event      string         `json:"event"`
		DistinctID string         `json:"distinct_id"`
		Timestamp  string         `json:"timestamp"`
		UUID       string         `json:"uuid"`
		Properties map[string]any `json:"properties"`
	}{credentials.ProjectToken.Reveal(), event, distinctID, timestamp.UTC().Format(time.RFC3339Nano), string(call.IdempotencyKey), properties})
	if err != nil || len(payload) > 1<<20 {
		return specialCaptureBranch(operationName, branches.defect, sdkgo.FailureValidation, "event payload exceeds the limit")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.captureURL+"/i/v0/e/", bytes.NewReader(payload))
	if err != nil {
		return specialCaptureBranch(operationName, branches.defect, sdkgo.FailureLocalDefect, "capture request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return specialCaptureRetry(operationName, sdkgo.FailureTransport, "PostHog capture response is unavailable", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return specialCaptureRetry(operationName, sdkgo.FailureRateLimit, "PostHog rate limited capture", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return specialCaptureRetry(operationName, sdkgo.FailureAvailability, "PostHog capture is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return specialCaptureBranch(operationName, branches.rejected, posthogRejectionKind(response.StatusCode), "PostHog rejected capture with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return specialCaptureRetry(operationName, sdkgo.FailureResponseTooLarge, "PostHog capture confirmation exceeded the limit", 0)
	}
	if err != nil {
		return specialCaptureRetry(operationName, sdkgo.FailureTransport, "PostHog capture confirmation was interrupted", 0)
	}
	var confirmation struct {
		Status       string          `json:"status"`
		QuotaLimited json.RawMessage `json:"quota_limited"`
	}
	if json.Unmarshal(body, &confirmation) != nil || confirmation.Status != "Ok" {
		return specialCaptureRetry(operationName, sdkgo.FailureProtocol, "PostHog capture confirmation is unusable", 0)
	}
	quota := bytes.TrimSpace(confirmation.QuotaLimited)
	if len(quota) > 0 && !bytes.Equal(quota, []byte("null")) && !bytes.Equal(quota, []byte("[]")) && !bytes.Equal(quota, []byte("{}")) {
		return specialCaptureBranch(operationName, branches.quotaLimited, sdkgo.FailureProviderRejection, "PostHog reported an event quota limit")
	}
	return sdkgo.NewMutationBranch(branches.accepted, CapturedEvent{EventID: string(call.IdempotencyKey), Accepted: true}, nil, sdkgo.Receipt{})
}

func validCaptureIdentity(identity string) bool {
	return strings.TrimSpace(identity) != "" && utf8.ValidString(identity) && len(identity) <= 512
}

func validPublicProperties(properties map[string]string) bool {
	for key, value := range properties {
		if strings.TrimSpace(key) == "" || !utf8.ValidString(key) || !utf8.ValidString(value) || len(key) > 128 || len(value) > 4096 || strings.HasPrefix(key, "$") || strings.EqualFold(key, "token") || strings.EqualFold(key, "api_key") || strings.EqualFold(key, "password") || strings.EqualFold(key, "secret") {
			return false
		}
	}
	return true
}

func specialCaptureBranch(operationName string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CapturedEvent] {
	failure := sdkgo.Failure{Kind: kind, Provider: "posthog", Operation: operationName, Message: message}
	return sdkgo.NewMutationBranch(branch, CapturedEvent{}, &failure, sdkgo.Receipt{})
}

func specialCaptureRetry(operationName string, kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.MutationAttempt[CapturedEvent] {
	failure := sdkgo.Failure{Kind: kind, Provider: "posthog", Operation: operationName, Message: message}
	return sdkgo.NewMutationRetry[CapturedEvent](failure, delay)
}
