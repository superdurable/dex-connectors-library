// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package klaviyo provides typed profile and event operations for Klaviyo.
package klaviyo

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const apiRevision = "2026-10-15"

// Option configures a client dependency at construction.
type Option interface{ applyClientOption(*Client) }

// Client holds immutable HTTP settings and resolves credentials for each call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// GetProfileInput identifies a Klaviyo profile.
type GetProfileInput struct {
	// ID is Klaviyo's opaque profile ID.
	ID string `json:"id"`
}

// Profile contains the identity fields returned for a Klaviyo profile.
type Profile struct {
	// ID is Klaviyo's opaque profile ID.
	ID string `json:"id"`
	// Email is the profile's email address, when present.
	Email string `json:"email,omitempty"`
}

// CreateEventInput associates one event with a profile email and metric.
type CreateEventInput struct {
	// Email identifies the profile. Klaviyo silently drops some test-domain events after accepting them.
	Email string `json:"email"`
	// MetricName names the event metric.
	MetricName string `json:"metricName"`
	// Properties contains optional non-secret string event properties.
	Properties map[string]string `json:"properties,omitempty"`
}

// EventAcceptance confirms submission, not completion of asynchronous processing.
type EventAcceptance struct {
	// Accepted means Klaviyo returned HTTP 202.
	Accepted bool `json:"accepted"`
}

// GetProfileOperation retrieves one profile.
type GetProfileOperation struct{ client *Client }

// CreateEventOperation submits a deduplicated event.
type CreateEventOperation struct{ client *Client }

// New validates settings and constructs a reusable Klaviyo client.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Klaviyo credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Klaviyo base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// GetProfile binds the profile lookup to this client.
func (client *Client) GetProfile() GetProfileOperation { return GetProfileOperation{client} }

// CreateEvent binds event submission to this client.
func (client *Client) CreateEvent() CreateEventOperation { return CreateEventOperation{client} }

// Definition returns the generated profile lookup contract.
func (GetProfileOperation) Definition() sdkgo.QueryDefinition { return GetProfileDefinition }

// Definition returns the generated event submission contract.
func (CreateEventOperation) Definition() sdkgo.MutationDefinition { return CreateEventDefinition }

// IdempotencyKey derives a 64-character unique_id from Dex's stable call identity.
func (CreateEventOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateEventInput) sdkgo.IdempotencyKey {
	digest := sha256.Sum256([]byte(callID))
	return sdkgo.IdempotencyKey(hex.EncodeToString(digest[:]))
}

// Invoke gets a profile by ID, selecting notFound for HTTP 404.
func (operation GetProfileOperation) Invoke(call sdkgo.Call, input GetProfileInput) sdkgo.QueryAttempt[Profile] {
	const name = "getProfile"
	if strings.TrimSpace(input.ID) == "" || strings.ContainsAny(input.ID, "/?&#") {
		return sdkgo.NewQueryBranch(GetProfileBranchDefect, Profile{}, klaviyoFailure(name, sdkgo.FailureValidation, "profile ID is invalid"), sdkgo.Receipt{})
	}
	request, failure := operation.client.newRequest(call, http.MethodGet, "/profiles/"+url.PathEscape(input.ID), nil, name)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetProfileBranchDefect, Profile{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[Profile](*klaviyoFailure(name, sdkgo.FailureTransport, "Klaviyo profile response is unavailable"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	if retryStatus(response.StatusCode) {
		return sdkgo.NewQueryRetry[Profile](*klaviyoFailure(name, sdkgo.FailureAvailability, "Klaviyo profile lookup is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetProfileBranchNotFound, Profile{}, klaviyoFailure(name, sdkgo.FailureNotFound, "Klaviyo profile was not found"), sdkgo.Receipt{})
	}
	if response.StatusCode != http.StatusOK {
		return sdkgo.NewQueryBranch(GetProfileBranchProviderRejected, Profile{}, klaviyoFailure(name, rejectionKind(response.StatusCode), "Klaviyo rejected profile lookup with HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewQueryBranch(GetProfileBranchInvalidResponse, Profile{}, klaviyoFailure(name, sdkgo.FailureResponseTooLarge, "Klaviyo profile response exceeded the limit"), sdkgo.Receipt{})
	}
	if err != nil {
		return sdkgo.NewQueryRetry[Profile](*klaviyoFailure(name, sdkgo.FailureAvailability, "Klaviyo profile response was interrupted"), 0)
	}
	var wire struct {
		Data struct {
			Type       string `json:"type"`
			ID         string `json:"id"`
			Attributes struct {
				Email string `json:"email"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Data.Type != "profile" || wire.Data.ID == "" {
		return sdkgo.NewQueryBranch(GetProfileBranchInvalidResponse, Profile{}, klaviyoFailure(name, sdkgo.FailureProtocol, "Klaviyo returned an invalid profile"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetProfileBranchFound, Profile{ID: wire.Data.ID, Email: wire.Data.Attributes.Email}, nil, sdkgo.Receipt{})
}

// Invoke submits an event with stable unique_id and returns acceptance only after HTTP 202.
func (operation CreateEventOperation) Invoke(call sdkgo.Call, input CreateEventInput) sdkgo.MutationAttempt[EventAcceptance] {
	const name = "createEvent"
	if !validEventEmail(input.Email) || strings.TrimSpace(input.MetricName) == "" || call.IdempotencyKey == "" {
		return sdkgo.NewMutationBranch(CreateEventBranchDefect, EventAcceptance{}, klaviyoFailure(name, sdkgo.FailureValidation, "email, metric name, and stable call identity are required"), sdkgo.Receipt{})
	}
	payload, err := json.Marshal(map[string]any{"data": map[string]any{"type": "event", "attributes": map[string]any{"properties": input.Properties, "metric": map[string]any{"data": map[string]any{"type": "metric", "attributes": map[string]string{"name": input.MetricName}}}, "profile": map[string]any{"data": map[string]any{"type": "profile", "attributes": map[string]string{"email": input.Email}}}, "unique_id": string(call.IdempotencyKey)}}})
	if err != nil || len(payload) > 1<<20 {
		return sdkgo.NewMutationBranch(CreateEventBranchDefect, EventAcceptance{}, klaviyoFailure(name, sdkgo.FailureValidation, "event payload is invalid or too large"), sdkgo.Receipt{})
	}
	request, failure := operation.client.newRequest(call, http.MethodPost, "/events", payload, name)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateEventBranchDefect, EventAcceptance{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[EventAcceptance](*klaviyoFailure(name, sdkgo.FailureTransport, "Klaviyo event response is unavailable"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	if retryStatus(response.StatusCode) {
		return sdkgo.NewMutationRetry[EventAcceptance](*klaviyoFailure(name, sdkgo.FailureAvailability, "Klaviyo event submission is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode != http.StatusAccepted {
		return sdkgo.NewMutationBranch(CreateEventBranchProviderRejected, EventAcceptance{}, klaviyoFailure(name, rejectionKind(response.StatusCode), "Klaviyo rejected event submission with HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	return sdkgo.NewMutationBranch(CreateEventBranchAccepted, EventAcceptance{Accepted: true}, nil, sdkgo.Receipt{})
}

func (client *Client) newRequest(call sdkgo.Call, method, path string, payload []byte, operation string) (*http.Request, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.PrivateAPIKey.Reveal()) {
		return nil, klaviyoFailure(operation, sdkgo.FailureAuthentication, "Klaviyo credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, klaviyoFailure(operation, sdkgo.FailureLocalDefect, "Klaviyo request could not be built")
	}
	request.Header.Set("Authorization", "Klaviyo-API-Key "+credentials.PrivateAPIKey.Reveal())
	request.Header.Set("Accept", "application/vnd.api+json")
	request.Header.Set("Content-Type", "application/vnd.api+json")
	request.Header.Set("revision", apiRevision)
	return request, nil
}

func validEventEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && strings.Contains(value, "@")
}
func retryStatus(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusConflict || status == http.StatusTooManyRequests || status >= 500
}
func rejectionKind(status int) sdkgo.FailureKind {
	if status == http.StatusUnauthorized {
		return sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		return sdkgo.FailureAuthorization
	}
	return sdkgo.FailureProviderRejection
}
func klaviyoFailure(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "klaviyo", Operation: operation, Message: message}
}
