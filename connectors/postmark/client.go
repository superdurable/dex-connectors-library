// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package postmark implements the Postmark JSON API as typed Dex connector Steps.
package postmark

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a client dependency during construction.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport; New copies it and disables redirects.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client holds immutable HTTP settings and resolves credentials for each call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// ListOutboundMessagesInput chooses a bounded outbound search page.
type ListOutboundMessagesInput struct {
	// Count is the number of messages requested, up to 500.
	Count int `json:"count"`
	// Offset is the zero-based page offset.
	Offset int `json:"offset"`
}

// GetMessageDetailsInput identifies an outbound message.
type GetMessageDetailsInput struct {
	// MessageID is Postmark's outbound message identifier.
	MessageID string `json:"messageId"`
}

// MessageDetails contains non-secret outbound message metadata.
type MessageDetails struct {
	// MessageID is Postmark's outbound message identifier.
	MessageID string `json:"MessageID"`
	// Subject is the email subject.
	Subject string `json:"Subject"`
	// Status is the provider's current status.
	Status string `json:"Status"`
}

// MessagePage is one outbound message search page.
type MessagePage struct {
	// TotalCount is the count of matching provider records.
	TotalCount int `json:"TotalCount"`
	// Messages contains the current outbound page.
	Messages []MessageDetails `json:"Messages"`
}

// ListOutboundMessagesOperation invokes Postmark's listOutboundMessages endpoint.
type ListOutboundMessagesOperation struct {
	client *Client
}

// Definition returns the generated listOutboundMessages contract.
func (ListOutboundMessagesOperation) Definition() sdkgo.QueryDefinition {
	return ListOutboundMessagesDefinition
}

// GetMessageDetailsOperation invokes Postmark's getMessageDetails endpoint.
type GetMessageDetailsOperation struct {
	client *Client
}

// Definition returns the generated getMessageDetails contract.
func (GetMessageDetailsOperation) Definition() sdkgo.QueryDefinition {
	return GetMessageDetailsDefinition
}

// New validates configuration without a network call. Zero settings use manifest defaults.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Postmark credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Postmark base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// ListOutboundMessages binds listOutboundMessages to this client.
func (client *Client) ListOutboundMessages() ListOutboundMessagesOperation {
	return ListOutboundMessagesOperation{client}
}

// GetMessageDetails binds getMessageDetails to this client.
func (client *Client) GetMessageDetails() GetMessageDetailsOperation {
	return GetMessageDetailsOperation{client}
}

// Invoke runs this provider operation.
func (operation ListOutboundMessagesOperation) Invoke(call sdkgo.Call, input ListOutboundMessagesInput) sdkgo.QueryAttempt[MessagePage] {
	if input.Count == 0 {
		input.Count = 50
	}
	if input.Count < 1 || input.Count > 500 || input.Offset < 0 || input.Count+input.Offset > 10000 {
		return queryFailure[MessagePage]("listOutboundMessages", ListOutboundMessagesBranchDefect, sdkgo.FailureValidation, "count and offset must fit Postmark's 10,000-message search window")
	}
	query := url.Values{"count": {strconv.Itoa(input.Count)}, "offset": {strconv.Itoa(input.Offset)}}
	return runQuery(operation.client, call, "listOutboundMessages", "/messages/outbound?"+query.Encode(), ListOutboundMessagesBranchListed, "", ListOutboundMessagesBranchProviderRejected, ListOutboundMessagesBranchInvalidResponse, ListOutboundMessagesBranchDefect, func(body []byte) (MessagePage, error) {
		var page MessagePage
		err := json.Unmarshal(body, &page)
		if err == nil && page.Messages == nil {
			err = fmt.Errorf("messages missing")
		}
		return page, err
	})
}

// Invoke runs this provider operation.
func (operation GetMessageDetailsOperation) Invoke(call sdkgo.Call, input GetMessageDetailsInput) sdkgo.QueryAttempt[MessageDetails] {
	if input.MessageID == "" {
		return queryFailure[MessageDetails]("getMessageDetails", GetMessageDetailsBranchDefect, sdkgo.FailureValidation, "message ID is required")
	}
	return runQuery(operation.client, call, "getMessageDetails", "/messages/outbound/"+url.PathEscape(input.MessageID)+"/details", GetMessageDetailsBranchFound, GetMessageDetailsBranchNotFound, GetMessageDetailsBranchProviderRejected, GetMessageDetailsBranchInvalidResponse, GetMessageDetailsBranchDefect, func(body []byte) (MessageDetails, error) {
		var message MessageDetails
		err := json.Unmarshal(body, &message)
		if err == nil && message.MessageID == "" {
			err = fmt.Errorf("message ID missing")
		}
		return message, err
	})
}

func (client *Client) newRequest(call sdkgo.Call, method, path string, body []byte) (*http.Request, error) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return nil, fmt.Errorf("credentials unavailable")
	}
	if err := credentials.Validate(); err != nil || strings.TrimSpace(credentials.APIKey.Reveal()) == "" || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return nil, fmt.Errorf("credentials invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("request invalid")
	}
	request.Header.Set("X-Postmark-Server-Token", ""+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("User-Agent", "superdurable-postmark/0.1")
	return request, nil
}

func runQuery[T any](client *Client, call sdkgo.Call, operation, path string, success, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	request, err := client.newRequest(call, http.MethodGet, path, nil)
	if err != nil {
		return queryFailure[T](operation, defect, sdkgo.FailureAuthentication, "connection credentials or request are invalid")
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider request failed"), 0)
	}
	// A read-only response close cannot affect the provider outcome.
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[T](newFailure(operation, sdkgo.FailureRateLimit, "provider rate limited the request"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewQueryRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case operation == "getMessageDetails" && response.StatusCode == http.StatusUnprocessableEntity:
		if response.Header.Get("X-PM-ApiErrorCode") == "701" {
			return queryFailure[T](operation, notFound, sdkgo.FailureNotFound, "Postmark message was not found")
		}
		errorBody, _ := providerhttp.ReadBoundedBody(response.Body, providerhttp.MaxErrorBodyBytes)
		var providerError struct {
			ErrorCode int `json:"ErrorCode"`
		}
		if err := json.Unmarshal(errorBody, &providerError); err == nil && providerError.ErrorCode == 701 {
			return queryFailure[T](operation, notFound, sdkgo.FailureNotFound, "Postmark message was not found")
		}
		return queryFailure[T](operation, rejected, sdkgo.FailureProviderRejection, "Postmark rejected the message request")

	case response.StatusCode == http.StatusNotFound && notFound != "":
		return queryFailure[T](operation, notFound, sdkgo.FailureNotFound, "provider resource was not found")
	case response.StatusCode != http.StatusOK:
		return queryFailure[T](operation, rejected, sdkgo.FailureProviderRejection, "provider rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return queryFailure[T](operation, invalid, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider response was interrupted"), 0)
	}
	value, err := decode(content)
	if err != nil {
		return queryFailure[T](operation, invalid, sdkgo.FailureProtocol, "provider response is incomplete or malformed")
	}
	return sdkgo.NewQueryBranch(success, value, nil, sdkgo.Receipt{})
}

func runMutation[T any](client *Client, call sdkgo.Call, operation, method, path string, body []byte, extraHeader, extraValue string, success, notFound, rejected, invalid, defect sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.MutationAttempt[T] {
	request, err := client.newRequest(call, method, path, body)
	if err != nil {
		return mutationFailure[T](operation, defect, sdkgo.FailureAuthentication, "connection credentials or request are invalid")
	}
	if extraHeader != "" {
		request.Header.Set(extraHeader, extraValue)
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider request failed"), 0)
	}
	// Closing the response cannot change a completed provider write.
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureRateLimit, "provider rate limited the request"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusConflict:
		conflict, _ := providerhttp.ReadBoundedBody(response.Body, providerhttp.MaxErrorBodyBytes)
		names := providerhttp.ReadErrorTokens(conflict, []string{"/name"})
		if len(names) == 1 && names[0] == "invalid_idempotent_request" {
			return mutationFailure[T](operation, rejected, sdkgo.FailureProviderRejection, "idempotency key was used for a different request")
		}
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider is processing the same request"), 0)
	case response.StatusCode == http.StatusNotFound && notFound != "":
		return mutationFailure[T](operation, notFound, sdkgo.FailureNotFound, "provider resource was not found")
	case response.StatusCode != http.StatusOK && response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusAccepted:
		return mutationFailure[T](operation, rejected, sdkgo.FailureProviderRejection, "provider rejected the request with HTTP "+strconv.Itoa(response.StatusCode))
	}
	content, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return mutationFailure[T](operation, invalid, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewMutationRetry[T](newFailure(operation, sdkgo.FailureAvailability, "provider response was interrupted"), 0)
	}
	value, err := decode(content)
	if err != nil {
		return mutationFailure[T](operation, invalid, sdkgo.FailureProtocol, "provider response is incomplete or malformed")
	}
	return sdkgo.NewMutationBranch(success, value, nil, sdkgo.Receipt{})
}

func queryFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	var zero T
	failure := newFailure(operation, kind, message)
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}
func mutationFailure[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[T] {
	var zero T
	failure := newFailure(operation, kind, message)
	return sdkgo.NewMutationBranch(branch, zero, &failure, sdkgo.Receipt{})
}
func newFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "postmark", Operation: operation, Message: message}
}
