// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package resend implements the Resend JSON API as typed Dex connector Steps.
package resend

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

// SendEmailInput is the exact payload sent once per keyed email operation.
type SendEmailInput struct {
	// From is the verified sender email address.
	From string `json:"from"`
	// To contains recipient email addresses.
	To []string `json:"to"`
	// Subject is the email subject.
	Subject string `json:"subject"`
	// HTML is the HTML email body.
	HTML string `json:"html"`
}

// SentEmail identifies a queued email.
type SentEmail struct {
	// ID is the provider's stable resource identifier.
	ID string `json:"id"`
}

// GetEmailInput identifies one sent email.
type GetEmailInput struct {
	// ID is the provider's stable resource identifier.
	ID string `json:"id"`
}

// Email contains selected sent email fields.
type Email struct {
	// ID is the provider's stable resource identifier.
	ID string `json:"id"`
	// From is the verified sender email address.
	From string `json:"from"`
	// To contains recipient email addresses.
	To []string `json:"to"`
	// Subject is the email subject.
	Subject string `json:"subject"`
	// CreatedAt is the provider's creation timestamp.
	CreatedAt string `json:"created_at"`
}

// SendEmailOperation invokes Resend's sendEmail endpoint.
type SendEmailOperation struct {
	client *Client
}

// Definition returns the generated sendEmail contract.
func (SendEmailOperation) Definition() sdkgo.MutationDefinition { return SendEmailDefinition }

// GetEmailOperation invokes Resend's getEmail endpoint.
type GetEmailOperation struct {
	client *Client
}

// Definition returns the generated getEmail contract.
func (GetEmailOperation) Definition() sdkgo.QueryDefinition { return GetEmailDefinition }

// New validates configuration without a network call. Zero settings use manifest defaults.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Resend credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Resend base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// SendEmail binds sendEmail to this client.
func (client *Client) SendEmail() SendEmailOperation { return SendEmailOperation{client} }

// GetEmail binds getEmail to this client.
func (client *Client) GetEmail() GetEmailOperation { return GetEmailOperation{client} }

// IdempotencyKey derives a stable provider-safe call key.
func (operation SendEmailOperation) IdempotencyKey(callID sdkgo.CallID, _ SendEmailInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke runs this provider operation.
func (operation SendEmailOperation) Invoke(call sdkgo.Call, input SendEmailInput) sdkgo.MutationAttempt[SentEmail] {
	if input.From == "" || len(input.To) == 0 || input.Subject == "" || input.HTML == "" {
		return mutationFailure[SentEmail]("sendEmail", SendEmailBranchDefect, sdkgo.FailureValidation, "from, to, subject, and html are required")
	}
	for _, recipient := range input.To {
		if recipient == "" {
			return mutationFailure[SentEmail]("sendEmail", SendEmailBranchDefect, sdkgo.FailureValidation, "recipient cannot be blank")
		}
	}
	if call.IdempotencyKey == "" || len(call.IdempotencyKey) > 256 {
		return mutationFailure[SentEmail]("sendEmail", SendEmailBranchDefect, sdkgo.FailureValidation, "idempotency key is invalid")
	}
	body, err := json.Marshal(input)
	if err != nil {
		return mutationFailure[SentEmail]("sendEmail", SendEmailBranchDefect, sdkgo.FailureValidation, "email cannot be encoded")
	}
	return runMutation(operation.client, call, "sendEmail", http.MethodPost, "/emails", body, "Idempotency-Key", string(call.IdempotencyKey), SendEmailBranchSent, "", SendEmailBranchProviderRejected, SendEmailBranchInvalidResponse, SendEmailBranchDefect, func(body []byte) (SentEmail, error) {
		var email SentEmail
		err := json.Unmarshal(body, &email)
		if err == nil && email.ID == "" {
			err = fmt.Errorf("email ID missing")
		}
		return email, err
	})
}

// Invoke runs this provider operation.
func (operation GetEmailOperation) Invoke(call sdkgo.Call, input GetEmailInput) sdkgo.QueryAttempt[Email] {
	if input.ID == "" {
		return queryFailure[Email]("getEmail", GetEmailBranchDefect, sdkgo.FailureValidation, "email ID is required")
	}
	return runQuery(operation.client, call, "getEmail", "/emails/"+url.PathEscape(input.ID), GetEmailBranchFound, GetEmailBranchNotFound, GetEmailBranchProviderRejected, GetEmailBranchInvalidResponse, GetEmailBranchDefect, func(body []byte) (Email, error) {
		var email Email
		err := json.Unmarshal(body, &email)
		if err == nil && email.ID == "" {
			err = fmt.Errorf("email ID missing")
		}
		return email, err
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
	request.Header.Set("Authorization", "Bearer "+credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	request.Header.Set("User-Agent", "superdurable-resend/0.1")
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
	return sdkgo.Failure{Kind: kind, Provider: "resend", Operation: operation, Message: message}
}
