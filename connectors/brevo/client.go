// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package brevo exposes typed contact operations for Brevo's REST API.
package brevo

import (
	"bytes"
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

// Client holds immutable HTTP settings and resolves credentials for each call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// Option configures a client dependency at construction.
type Option interface{ applyClientOption(*Client) }

// CreateOrUpdateContactInput supplies an email and absolute attribute values.
type CreateOrUpdateContactInput struct {
	// Email identifies the contact and must be a plain email address.
	Email string `json:"email"`
	// Attributes sets existing Brevo contact attributes by their uppercase names.
	Attributes map[string]string `json:"attributes,omitempty"`
}

// ContactWriteResult confirms a contact create or update without exposing provider data.
type ContactWriteResult struct {
	// ID is available when Brevo created the contact.
	ID int64 `json:"id,omitempty"`
	// Email is the submitted contact email.
	Email string `json:"email"`
	// Created distinguishes a 201 create from a 204 update.
	Created bool `json:"created"`
}

// GetContactInput identifies a contact by email.
type GetContactInput struct {
	// Email is a plain email address.
	Email string `json:"email"`
}

// Contact contains the minimal non-secret contact identity returned by Brevo.
type Contact struct {
	// ID is Brevo's contact identifier.
	ID int64 `json:"id"`
	// Email is the contact email.
	Email string `json:"email"`
}

// CreateOrUpdateContactOperation performs a repeat-safe contact upsert.
type CreateOrUpdateContactOperation struct{ client *Client }

// GetContactOperation retrieves one contact.
type GetContactOperation struct{ client *Client }

// New validates the connection settings and builds a reusable client.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Brevo credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Brevo base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// CreateOrUpdateContact binds the contact upsert to this client.
func (client *Client) CreateOrUpdateContact() CreateOrUpdateContactOperation {
	return CreateOrUpdateContactOperation{client}
}

// GetContact binds the contact lookup to this client.
func (client *Client) GetContact() GetContactOperation { return GetContactOperation{client} }

// Definition returns the generated upsert contract.
func (CreateOrUpdateContactOperation) Definition() sdkgo.MutationDefinition {
	return CreateOrUpdateContactDefinition
}

// Definition returns the generated lookup contract.
func (GetContactOperation) Definition() sdkgo.QueryDefinition { return GetContactDefinition }

// IdempotencyKey uses Dex's stable call identity; setting absolute fields remains safe across retries.
func (CreateOrUpdateContactOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateOrUpdateContactInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends a repeat-safe contact upsert and classifies its response.
func (operation CreateOrUpdateContactOperation) Invoke(call sdkgo.Call, input CreateOrUpdateContactInput) sdkgo.MutationAttempt[ContactWriteResult] {
	const name = "createOrUpdateContact"
	if !validEmail(input.Email) {
		return sdkgo.NewMutationBranch(CreateOrUpdateContactBranchDefect, ContactWriteResult{}, brevoFailure(name, sdkgo.FailureValidation, "a plain email address is required"), sdkgo.Receipt{})
	}
	for key := range input.Attributes {
		if key == "" || strings.ToUpper(key) != key {
			return sdkgo.NewMutationBranch(CreateOrUpdateContactBranchDefect, ContactWriteResult{}, brevoFailure(name, sdkgo.FailureValidation, "attribute names must be uppercase"), sdkgo.Receipt{})
		}
	}
	payload, err := json.Marshal(struct {
		Email         string            `json:"email"`
		Attributes    map[string]string `json:"attributes,omitempty"`
		UpdateEnabled bool              `json:"updateEnabled"`
	}{input.Email, input.Attributes, true})
	if err != nil || len(payload) > 1<<20 {
		return sdkgo.NewMutationBranch(CreateOrUpdateContactBranchDefect, ContactWriteResult{}, brevoFailure(name, sdkgo.FailureValidation, "contact payload is invalid or too large"), sdkgo.Receipt{})
	}
	request, failure := operation.client.newRequest(call, http.MethodPost, "/contacts", payload, name)
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateOrUpdateContactBranchDefect, ContactWriteResult{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewMutationRetry[ContactWriteResult](*brevoFailure(name, sdkgo.FailureTransport, "Brevo contact response is unavailable"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	if isRetryable(response.StatusCode) {
		return sdkgo.NewMutationRetry[ContactWriteResult](*brevoFailure(name, sdkgo.FailureAvailability, "Brevo contact request is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode != http.StatusCreated && response.StatusCode != http.StatusNoContent {
		return sdkgo.NewMutationBranch(CreateOrUpdateContactBranchProviderRejected, ContactWriteResult{}, brevoFailure(name, failureKind(response.StatusCode), "Brevo rejected contact request with HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	result := ContactWriteResult{Email: input.Email, Created: response.StatusCode == http.StatusCreated}
	if result.Created {
		body, readError := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
		if errors.Is(readError, providerhttp.ErrBodyTooLarge) {
			return sdkgo.NewMutationBranch(CreateOrUpdateContactBranchInvalidResponse, ContactWriteResult{}, brevoFailure(name, sdkgo.FailureResponseTooLarge, "Brevo contact response exceeded the limit"), sdkgo.Receipt{})
		}
		if readError != nil {
			return sdkgo.NewMutationRetry[ContactWriteResult](*brevoFailure(name, sdkgo.FailureAvailability, "Brevo contact response could not be read"), 0)
		}
		var created struct {
			ID int64 `json:"id"`
		}
		if json.Unmarshal(body, &created) != nil || created.ID <= 0 {
			return sdkgo.NewMutationBranch(CreateOrUpdateContactBranchInvalidResponse, ContactWriteResult{}, brevoFailure(name, sdkgo.FailureProtocol, "Brevo returned an invalid contact ID"), sdkgo.Receipt{})
		}
		result.ID = created.ID
	}
	return sdkgo.NewMutationBranch(CreateOrUpdateContactBranchSaved, result, nil, sdkgo.Receipt{})
}

// Invoke retrieves a contact and returns a notFound branch for HTTP 404.
func (operation GetContactOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.QueryAttempt[Contact] {
	const name = "getContact"
	if !validEmail(input.Email) {
		return sdkgo.NewQueryBranch(GetContactBranchDefect, Contact{}, brevoFailure(name, sdkgo.FailureValidation, "a plain email address is required"), sdkgo.Receipt{})
	}
	request, failure := operation.client.newRequest(call, http.MethodGet, "/contacts/"+url.PathEscape(input.Email), nil, name)
	if failure != nil {
		return sdkgo.NewQueryBranch(GetContactBranchDefect, Contact{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[Contact](*brevoFailure(name, sdkgo.FailureTransport, "Brevo contact response is unavailable"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	if isRetryable(response.StatusCode) {
		return sdkgo.NewQueryRetry[Contact](*brevoFailure(name, sdkgo.FailureAvailability, "Brevo contact lookup is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	}
	if response.StatusCode == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetContactBranchNotFound, Contact{}, brevoFailure(name, sdkgo.FailureNotFound, "Brevo contact was not found"), sdkgo.Receipt{})
	}
	if response.StatusCode != http.StatusOK {
		return sdkgo.NewQueryBranch(GetContactBranchProviderRejected, Contact{}, brevoFailure(name, failureKind(response.StatusCode), "Brevo rejected contact lookup with HTTP "+strconv.Itoa(response.StatusCode)), sdkgo.Receipt{})
	}
	body, readError := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if errors.Is(readError, providerhttp.ErrBodyTooLarge) {
		return sdkgo.NewQueryBranch(GetContactBranchInvalidResponse, Contact{}, brevoFailure(name, sdkgo.FailureResponseTooLarge, "Brevo contact response exceeded the limit"), sdkgo.Receipt{})
	}
	if readError != nil {
		return sdkgo.NewQueryRetry[Contact](*brevoFailure(name, sdkgo.FailureAvailability, "Brevo contact response could not be read"), 0)
	}
	var contact Contact
	if json.Unmarshal(body, &contact) != nil || contact.ID <= 0 || !validEmail(contact.Email) {
		return sdkgo.NewQueryBranch(GetContactBranchInvalidResponse, Contact{}, brevoFailure(name, sdkgo.FailureProtocol, "Brevo returned an invalid contact"), sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(GetContactBranchFound, contact, nil, sdkgo.Receipt{})
}

func (client *Client) newRequest(call sdkgo.Call, method, path string, payload []byte, operation string) (*http.Request, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return nil, brevoFailure(operation, sdkgo.FailureAuthentication, "Brevo connection credentials are unavailable or invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, method, client.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, brevoFailure(operation, sdkgo.FailureLocalDefect, "Brevo request could not be built")
	}
	request.Header.Set("api-key", credentials.APIKey.Reveal())
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	return request, nil
}

func validEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && address.Address == value && strings.Contains(value, "@")
}

func isRetryable(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusConflict || status == http.StatusTooManyRequests || status >= 500
}

func failureKind(status int) sdkgo.FailureKind {
	if status == http.StatusUnauthorized {
		return sdkgo.FailureAuthentication
	}
	if status == http.StatusForbidden {
		return sdkgo.FailureAuthorization
	}
	return sdkgo.FailureProviderRejection
}

func brevoFailure(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: "brevo", Operation: operation, Message: message}
}
