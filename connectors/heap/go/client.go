// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package heap writes selected user properties through Heap's server-side API.
package heap

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option configures an HTTP dependency when constructing a Client.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ value *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.value }

// WithHTTPClient supplies a transport for provider requests; the caller retains its ownership.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{value: httpClient} }

// Client holds immutable Heap settings and is safe for concurrent Dex Steps.
type Client struct {
	baseURL          string
	appID            string
	maxResponseBytes int64
	httpClient       *http.Client
}

// SetUserPropertiesInput identifies one user and a bounded set of non-secret string properties.
type SetUserPropertiesInput struct {
	// Identity is a stable, unique identity already used by Heap; it is case-sensitive.
	Identity string `json:"identity"`
	// Properties are absolute replacement values. Do not pass secrets.
	Properties map[string]string `json:"properties"`
}

// UserPropertiesResult confirms Heap accepted the requested property values.
type UserPropertiesResult struct {
	// Accepted is true only after Heap returns HTTP 200.
	Accepted bool `json:"accepted"`
}

// SetUserPropertiesOperation overwrites selected properties for one Heap identity.
type SetUserPropertiesOperation struct{ client *Client }

// New validates settings and creates a Heap client. The operation requires no credentials.
func New(config Config, _ sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if !validAppID(config.AppID) {
		return nil, fmt.Errorf("Heap appId must contain decimal digits")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Heap baseUrl: %w", err)
	}
	client := &Client{baseURL: baseURL, appID: config.AppID, maxResponseBytes: config.MaxResponseBytes}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("Heap client option is nil")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// SetUserProperties binds the property update Mutation to this client.
func (client *Client) SetUserProperties() SetUserPropertiesOperation {
	return SetUserPropertiesOperation{client: client}
}

// Definition returns the manifest-generated Mutation contract.
func (SetUserPropertiesOperation) Definition() sdkgo.MutationDefinition {
	return SetUserPropertiesDefinition
}

// IdempotencyKey derives a stable call key; Heap retries are safe because properties overwrite values.
func (SetUserPropertiesOperation) IdempotencyKey(callID sdkgo.CallID, _ SetUserPropertiesInput) sdkgo.IdempotencyKey {
	digest := sha256.Sum256([]byte(callID))
	return sdkgo.IdempotencyKey(hex.EncodeToString(digest[:]))
}

// Invoke sends one bounded property update and classifies provider outcomes.
func (operation SetUserPropertiesOperation) Invoke(call sdkgo.Call, input SetUserPropertiesInput) sdkgo.MutationAttempt[UserPropertiesResult] {
	if !validInput(input) {
		return propertiesBranch(SetUserPropertiesBranchDefect, sdkgo.FailureValidation, "identity or properties are invalid")
	}
	payload, err := json.Marshal(struct {
		AppID      string            `json:"app_id"`
		Identity   string            `json:"identity"`
		Properties map[string]string `json:"properties"`
	}{AppID: operation.client.appID, Identity: input.Identity, Properties: input.Properties})
	if err != nil || len(payload) > 65536 {
		return propertiesBranch(SetUserPropertiesBranchDefect, sdkgo.FailureValidation, "property payload exceeds the limit")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, operation.client.baseURL+"/api/add_user_properties", bytes.NewReader(payload))
	if err != nil {
		return propertiesBranch(SetUserPropertiesBranchDefect, sdkgo.FailureLocalDefect, "Heap request could not be built")
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return propertiesRetry(sdkgo.FailureTransport, "Heap response is unavailable", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	if response.StatusCode == http.StatusTooManyRequests {
		return propertiesRetry(sdkgo.FailureRateLimit, "Heap rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return propertiesRetry(sdkgo.FailureAvailability, "Heap is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return propertiesBranch(SetUserPropertiesBranchProviderRejected, sdkgo.FailureProviderRejection, "Heap rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if _, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes); err != nil {
		if errors.Is(err, providerhttp.ErrBodyTooLarge) {
			return propertiesBranch(SetUserPropertiesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Heap confirmation exceeded the limit")
		}
		return propertiesRetry(sdkgo.FailureTransport, "Heap confirmation was interrupted", 0)
	}
	return sdkgo.NewMutationBranch(SetUserPropertiesBranchAccepted, UserPropertiesResult{Accepted: true}, nil, sdkgo.Receipt{})
}

func validAppID(appID string) bool {
	if len(appID) == 0 || len(appID) > 32 {
		return false
	}
	for _, character := range appID {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func validInput(input SetUserPropertiesInput) bool {
	if strings.TrimSpace(input.Identity) == "" || !utf8.ValidString(input.Identity) || len(input.Identity) > 255 || len(input.Properties) == 0 || len(input.Properties) > 50 {
		return false
	}
	for key, value := range input.Properties {
		if strings.TrimSpace(key) == "" || strings.EqualFold(key, "id") || !utf8.ValidString(key) || !utf8.ValidString(value) || len(key) >= 1024 || len(value) >= 1024 {
			return false
		}
	}
	return true
}

func propertiesFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "heap", Operation: "setUserProperties", Message: message}
}

func propertiesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[UserPropertiesResult] {
	failure := propertiesFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, UserPropertiesResult{}, &failure, sdkgo.Receipt{})
}

func propertiesRetry(kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.MutationAttempt[UserPropertiesResult] {
	return sdkgo.NewMutationRetry[UserPropertiesResult](propertiesFailure(kind, message), after)
}
