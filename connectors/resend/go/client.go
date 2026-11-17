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

// ListEmailsInput selects a bounded sent-email page. Zero Limit uses Resend's default.
type ListEmailsInput struct {
	// Limit is the number of emails, from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive email ID cursor; blank starts at the newest email.
	After string `json:"after,omitempty"`
}

// EmailPage contains one sent-email page and its continuation indicator.
type EmailPage struct {
	// Data contains the sent email references.
	Data []Email `json:"data"`
	// HasMore reports whether another page is available.
	HasMore bool `json:"has_more"`
}

// GetContactInput selects one global contact by ID or email address.
type GetContactInput struct {
	// IDOrEmail is a contact ID or email address; blank is invalid.
	IDOrEmail string `json:"idOrEmail"`
}

// Contact contains selected global contact fields.
type Contact struct {
	// ID is the stable Resend contact ID.
	ID string `json:"id"`
	// Email is the contact's email address.
	Email string `json:"email"`
	// FirstName is the optional given name.
	FirstName string `json:"first_name"`
	// LastName is the optional family name.
	LastName string `json:"last_name"`
	// Unsubscribed reports the contact's subscription state.
	Unsubscribed bool `json:"unsubscribed"`
}

// ListContactsInput selects a bounded global-contact page.
type ListContactsInput struct {
	// Limit is the page size from 1 through 100; zero uses Resend's default.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive contact ID cursor; blank starts at the first page.
	After string `json:"after,omitempty"`
}

// ContactPage contains one contact page and its continuation indicator.
type ContactPage struct {
	// Data contains the contacts.
	Data []Contact `json:"data"`
	// HasMore reports whether another page is available.
	HasMore bool `json:"has_more"`
}

// GetDomainInput selects one sending domain by its Resend ID.
type GetDomainInput struct {
	// ID is the domain ID; blank is invalid.
	ID string `json:"id"`
}

// Domain contains selected sending-domain fields.
type Domain struct {
	// ID is the stable Resend domain ID.
	ID string `json:"id"`
	// Name is the DNS domain name.
	Name string `json:"name"`
	// Status is Resend's verification state.
	Status string `json:"status"`
}

// ListDomainsInput selects a bounded domain page.
type ListDomainsInput struct {
	// Limit is the page size from 1 through 100; zero retrieves all domains.
	Limit int `json:"limit,omitempty"`
	// After is the exclusive domain ID cursor; it requires a nonzero Limit.
	After string `json:"after,omitempty"`
}

// DomainPage contains one sending-domain page and its continuation indicator.
type DomainPage struct {
	// Data contains the domains.
	Data []Domain `json:"data"`
	// HasMore reports whether another page is available.
	HasMore bool `json:"has_more"`
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

// ListEmailsOperation invokes Resend's sent-email listing endpoint.
type ListEmailsOperation struct{ client *Client }

// Definition returns the generated listEmails contract.
func (ListEmailsOperation) Definition() sdkgo.QueryDefinition { return ListEmailsDefinition }

// GetContactOperation invokes Resend's global contact detail endpoint.
type GetContactOperation struct{ client *Client }

// Definition returns the generated getContact contract.
func (GetContactOperation) Definition() sdkgo.QueryDefinition { return GetContactDefinition }

// ListContactsOperation invokes Resend's global contact listing endpoint.
type ListContactsOperation struct{ client *Client }

// Definition returns the generated listContacts contract.
func (ListContactsOperation) Definition() sdkgo.QueryDefinition { return ListContactsDefinition }

// GetDomainOperation invokes Resend's domain detail endpoint.
type GetDomainOperation struct{ client *Client }

// Definition returns the generated getDomain contract.
func (GetDomainOperation) Definition() sdkgo.QueryDefinition { return GetDomainDefinition }

// ListDomainsOperation invokes Resend's domain listing endpoint.
type ListDomainsOperation struct{ client *Client }

// Definition returns the generated listDomains contract.
func (ListDomainsOperation) Definition() sdkgo.QueryDefinition { return ListDomainsDefinition }

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

// ListEmails binds listEmails to this client.
func (client *Client) ListEmails() ListEmailsOperation { return ListEmailsOperation{client} }

// GetContact binds getContact to this client.
func (client *Client) GetContact() GetContactOperation { return GetContactOperation{client} }

// ListContacts binds listContacts to this client.
func (client *Client) ListContacts() ListContactsOperation { return ListContactsOperation{client} }

// GetDomain binds getDomain to this client.
func (client *Client) GetDomain() GetDomainOperation { return GetDomainOperation{client} }

// ListDomains binds listDomains to this client.
func (client *Client) ListDomains() ListDomainsOperation { return ListDomainsOperation{client} }

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

// Invoke lists sent emails using a bounded Resend page.
func (operation ListEmailsOperation) Invoke(call sdkgo.Call, input ListEmailsInput) sdkgo.QueryAttempt[EmailPage] {
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[EmailPage]("listEmails", ListEmailsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listEmails", "/emails"+query, ListEmailsBranchListed, "", ListEmailsBranchProviderRejected, ListEmailsBranchInvalidResponse, ListEmailsBranchDefect, func(body []byte) (EmailPage, error) {
		var page EmailPage
		err := json.Unmarshal(body, &page)
		if err == nil && page.Data == nil {
			err = fmt.Errorf("email page data missing")
		}
		return page, err
	})
}

// Invoke retrieves one global contact by ID or email address.
func (operation GetContactOperation) Invoke(call sdkgo.Call, input GetContactInput) sdkgo.QueryAttempt[Contact] {
	if strings.TrimSpace(input.IDOrEmail) == "" {
		return queryFailure[Contact]("getContact", GetContactBranchDefect, sdkgo.FailureValidation, "contact ID or email is required")
	}
	return runQuery(operation.client, call, "getContact", "/contacts/"+url.PathEscape(input.IDOrEmail), GetContactBranchFound, GetContactBranchNotFound, GetContactBranchProviderRejected, GetContactBranchInvalidResponse, GetContactBranchDefect, decodeContact)
}

// Invoke lists global contacts using a bounded Resend page.
func (operation ListContactsOperation) Invoke(call sdkgo.Call, input ListContactsInput) sdkgo.QueryAttempt[ContactPage] {
	query, err := listQuery(input.Limit, input.After, false)
	if err != nil {
		return queryFailure[ContactPage]("listContacts", ListContactsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listContacts", "/contacts"+query, ListContactsBranchListed, "", ListContactsBranchProviderRejected, ListContactsBranchInvalidResponse, ListContactsBranchDefect, func(body []byte) (ContactPage, error) {
		var page ContactPage
		err := json.Unmarshal(body, &page)
		if err == nil && page.Data == nil {
			err = fmt.Errorf("contact page data missing")
		}
		return page, err
	})
}

// Invoke retrieves one sending domain by ID.
func (operation GetDomainOperation) Invoke(call sdkgo.Call, input GetDomainInput) sdkgo.QueryAttempt[Domain] {
	if strings.TrimSpace(input.ID) == "" {
		return queryFailure[Domain]("getDomain", GetDomainBranchDefect, sdkgo.FailureValidation, "domain ID is required")
	}
	return runQuery(operation.client, call, "getDomain", "/domains/"+url.PathEscape(input.ID), GetDomainBranchFound, GetDomainBranchNotFound, GetDomainBranchProviderRejected, GetDomainBranchInvalidResponse, GetDomainBranchDefect, decodeDomain)
}

// Invoke lists sending domains using a bounded Resend page.
func (operation ListDomainsOperation) Invoke(call sdkgo.Call, input ListDomainsInput) sdkgo.QueryAttempt[DomainPage] {
	query, err := listQuery(input.Limit, input.After, true)
	if err != nil {
		return queryFailure[DomainPage]("listDomains", ListDomainsBranchDefect, sdkgo.FailureValidation, err.Error())
	}
	return runQuery(operation.client, call, "listDomains", "/domains"+query, ListDomainsBranchListed, "", ListDomainsBranchProviderRejected, ListDomainsBranchInvalidResponse, ListDomainsBranchDefect, func(body []byte) (DomainPage, error) {
		var page DomainPage
		err := json.Unmarshal(body, &page)
		if err == nil && page.Data == nil {
			err = fmt.Errorf("domain page data missing")
		}
		return page, err
	})
}

func listQuery(limit int, after string, cursorNeedsLimit bool) (string, error) {
	if limit < 0 || limit > 100 || (cursorNeedsLimit && after != "" && limit == 0) {
		return "", fmt.Errorf("limit must be 1 through 100 when set and is required with a domain cursor")
	}
	query := url.Values{}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	if after != "" {
		query.Set("after", after)
	}
	if len(query) == 0 {
		return "", nil
	}
	return "?" + query.Encode(), nil
}

func decodeContact(body []byte) (Contact, error) {
	var contact Contact
	err := json.Unmarshal(body, &contact)
	if err == nil && (contact.ID == "" || contact.Email == "") {
		err = fmt.Errorf("contact ID or email missing")
	}
	return contact, err
}

func decodeDomain(body []byte) (Domain, error) {
	var domain Domain
	err := json.Unmarshal(body, &domain)
	if err == nil && (domain.ID == "" || domain.Name == "") {
		err = fmt.Errorf("domain ID or name missing")
	}
	return domain, err
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
		if extraHeader != "Idempotency-Key" {
			return mutationFailure[T](operation, rejected, sdkgo.FailureProviderRejection, "provider rejected the request with HTTP 409")
		}
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
