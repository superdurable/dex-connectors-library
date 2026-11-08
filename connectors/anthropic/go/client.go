// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package anthropic exposes Anthropic model metadata as a typed Dex Query Step.
// See examples/model-inspection for a complete Flow and Worker.
package anthropic

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const anthropicAPIVersion = "2026-10-21"

var modelIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,199}$`)

// Option supplies a non-serializable client dependency.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies a transport; New copies it, preserves a nonzero timeout, and disables redirects.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable HTTP settings and resolves credentials for each call. It is safe for concurrent use.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// GetModelInput identifies a Claude model ID or alias. It never contains a credential.
type GetModelInput struct {
	// ModelID is an Anthropic model ID or alias, using letters, digits, periods, underscores, or hyphens.
	ModelID string `json:"modelId"`
}

// ModelInfo contains identity and display metadata for a Claude model.
type ModelInfo struct {
	// ID is the resolved model ID, which can differ from a requested alias.
	ID string `json:"id"`
	// DisplayName is the provider's human-readable model label.
	DisplayName string `json:"displayName"`
	// CreatedAt is the provider's RFC 3339 model creation timestamp.
	CreatedAt string `json:"createdAt"`
}

// GetModelOperation fetches one Claude model by ID or alias.
type GetModelOperation struct{ client *Client }

// New validates configuration without contacting Anthropic. Credentials are resolved for every call.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Anthropic credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Anthropic base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// GetModel returns the typed model lookup Query bound to this client.
func (client *Client) GetModel() GetModelOperation { return GetModelOperation{client} }

// Definition returns the manifest-generated model lookup contract.
func (GetModelOperation) Definition() sdkgo.QueryDefinition { return GetModelDefinition }

// Invoke validates the model ID, fetches model metadata, and classifies provider failures without exposing credentials.
func (operation GetModelOperation) Invoke(call sdkgo.Call, input GetModelInput) sdkgo.QueryAttempt[ModelInfo] {
	if !modelIDPattern.MatchString(input.ModelID) {
		return failedGetModel(GetModelBranchDefect, sdkgo.FailureValidation, "modelId has an invalid format")
	}
	credentials, err := operation.client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return failedGetModel(GetModelBranchDefect, sdkgo.FailureAuthentication, "connection credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, operation.client.baseURL+"/v1/models/"+input.ModelID, nil)
	if err != nil {
		return failedGetModel(GetModelBranchDefect, sdkgo.FailureLocalDefect, "model request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("anthropic-version", anthropicAPIVersion)
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return retryGetModel(sdkgo.FailureAvailability, "Anthropic model request failed", 0)
	}
	// Closing a read-only response cannot change its outcome.
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return retryGetModel(sdkgo.FailureRateLimit, "Anthropic rate limited the model request", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode >= http.StatusInternalServerError:
		return retryGetModel(sdkgo.FailureAvailability, "Anthropic model lookup is temporarily unavailable", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusNotFound:
		return failedGetModel(GetModelBranchNotFound, sdkgo.FailureNotFound, "Anthropic did not find the model")
	case response.StatusCode == http.StatusUnauthorized:
		return failedGetModel(GetModelBranchProviderRejected, sdkgo.FailureAuthentication, "Anthropic rejected the API key")
	case response.StatusCode == http.StatusForbidden:
		return failedGetModel(GetModelBranchProviderRejected, sdkgo.FailureAuthorization, "Anthropic denied access to the model")
	case response.StatusCode != http.StatusOK:
		return failedGetModel(GetModelBranchProviderRejected, sdkgo.FailureProviderRejection, "Anthropic rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return failedGetModel(GetModelBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Anthropic model response exceeded the configured limit")
	}
	if err != nil {
		return retryGetModel(sdkgo.FailureAvailability, "Anthropic model response was interrupted", 0)
	}
	var model struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		CreatedAt   string `json:"created_at"`
		Type        string `json:"type"`
	}
	if json.Unmarshal(body, &model) != nil || model.Type != "model" || model.ID == "" || model.DisplayName == "" {
		return failedGetModel(GetModelBranchInvalidResponse, sdkgo.FailureProtocol, "Anthropic returned unusable model data")
	}
	if _, err := time.Parse(time.RFC3339Nano, model.CreatedAt); err != nil {
		return failedGetModel(GetModelBranchInvalidResponse, sdkgo.FailureProtocol, "Anthropic returned an invalid model timestamp")
	}
	return sdkgo.NewQueryBranch(GetModelBranchFound, ModelInfo{ID: model.ID, DisplayName: model.DisplayName, CreatedAt: model.CreatedAt}, nil, sdkgo.Receipt{})
}

func failedGetModel(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[ModelInfo] {
	failure := sdkgo.Failure{Kind: kind, Provider: "anthropic", Operation: "getModel", Message: message}
	return sdkgo.NewQueryBranch(branch, ModelInfo{}, &failure, sdkgo.Receipt{})
}

func retryGetModel(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.QueryAttempt[ModelInfo] {
	failure := sdkgo.Failure{Kind: kind, Provider: "anthropic", Operation: "getModel", Message: message}
	return sdkgo.NewQueryRetry[ModelInfo](failure, retryAfter)
}
