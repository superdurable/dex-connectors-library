// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package close implements read-only Close API Queries.
package close

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable HTTP dependency.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport; New copies it and disables redirects.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client holds immutable HTTP configuration and resolves credentials per call.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates configuration without contacting Close.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Close credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("Close base URL: %w", err)
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

var (
	safeKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

// ListLeadsInput selects one Close lead page.
type ListLeadsInput struct {
	// Skip is the zero-based offset into the lead list.
	Skip int `json:"skip,omitempty"`
	// Limit caps leads returned; zero uses 100.
	Limit int `json:"limit,omitempty"`
}

// Lead identifies a Close lead and its display name.
type Lead struct {
	// ID is the provider-assigned resource identity.
	ID string `json:"id"`
	// Name is the resource display name.
	Name string `json:"name"`
}

// LeadPage contains one page and a continuation flag.
type LeadPage struct {
	// Leads contains the returned Close lead records.
	Leads []Lead `json:"leads"`
	// HasMore reports whether another lead page exists.
	HasMore bool `json:"hasMore"`
}

// GetLeadInput identifies one Close lead.
type GetLeadInput struct {
	// LeadID is the Close lead_ identifier.
	LeadID string `json:"leadId"`
}

// ListLeads returns the bound listLeads Query.
func (client *Client) ListLeads() ListLeadsOperation { return ListLeadsOperation{client} }

// ListLeadsOperation performs one Close read.
type ListLeadsOperation struct{ client *Client }

// Definition returns the manifest-generated operation contract.
func (ListLeadsOperation) Definition() sdkgo.QueryDefinition { return ListLeadsDefinition }

// Invoke validates input and classifies the HTTP result.
func (operation ListLeadsOperation) Invoke(call sdkgo.Call, input ListLeadsInput) sdkgo.QueryAttempt[LeadPage] {
	if input.Skip < 0 || input.Limit < 0 || input.Limit > 100 {
		return failedBranch[LeadPage]("listLeads", ListLeadsBranchDefect, sdkgo.FailureValidation, "skip or limit is invalid")
	}
	limit := input.Limit
	if limit == 0 {
		limit = 100
	}
	path := "/lead/?_skip=" + strconv.Itoa(input.Skip) + "&_limit=" + strconv.Itoa(limit)
	return invokeGet(operation.client, call, "listLeads", path, ListLeadsBranchListed, ListLeadsBranchProviderRejected, decodeListLeads)
}
func decodeListLeads(body []byte) (LeadPage, error) {
	var wire struct {
		Data []Lead `json:"data"`
		// HasMore reports whether another lead page exists.
		HasMore bool `json:"has_more"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return LeadPage{}, err
	}
	if wire.Data == nil {
		return LeadPage{}, fmt.Errorf("missing leads")
	}
	return LeadPage{Leads: wire.Data, HasMore: wire.HasMore}, nil
}

// GetLead returns the bound getLead Query.
func (client *Client) GetLead() GetLeadOperation { return GetLeadOperation{client} }

// GetLeadOperation performs one Close read.
type GetLeadOperation struct{ client *Client }

// Definition returns the manifest-generated operation contract.
func (GetLeadOperation) Definition() sdkgo.QueryDefinition { return GetLeadDefinition }

// Invoke validates input and classifies the HTTP result.
func (operation GetLeadOperation) Invoke(call sdkgo.Call, input GetLeadInput) sdkgo.QueryAttempt[Lead] {
	if !strings.HasPrefix(input.LeadID, "lead_") || !safeKey.MatchString(input.LeadID) {
		return failedBranch[Lead]("getLead", GetLeadBranchDefect, sdkgo.FailureValidation, "leadId must be a Close lead ID")
	}
	path := "/lead/" + input.LeadID + "/"
	return invokeGet(operation.client, call, "getLead", path, GetLeadBranchFound, GetLeadBranchNotFound, decodeGetLead)
}
func decodeGetLead(body []byte) (Lead, error) {
	var lead Lead
	if err := json.Unmarshal(body, &lead); err != nil {
		return Lead{}, err
	}
	if lead.ID == "" || lead.Name == "" {
		return Lead{}, fmt.Errorf("incomplete lead")
	}
	return lead, nil
}

func invokeGet[T any](client *Client, call sdkgo.Call, operationID, path string, successBranch, notFoundBranch sdkgo.BranchID, decode func([]byte) (T, error)) sdkgo.QueryAttempt[T] {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return failedBranch[T](operationID, "defect", sdkgo.FailureAuthentication, "connection credentials are unavailable")
	}
	if err := credentials.Validate(); err != nil || !providerhttp.IsHeaderSafeCredential(credentials.APIKey.Reveal()) {
		return failedBranch[T](operationID, "defect", sdkgo.FailureAuthentication, "connection credentials are invalid")
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return failedBranch[T](operationID, "defect", sdkgo.FailureLocalDefect, "request cannot be built")
	}
	request.Header.Set("Accept", "application/json")
	request.SetBasicAuth(credentials.APIKey.Reveal(), "")
	response, err := client.httpClient.Do(request)
	if err != nil {
		return retryBranch[T](operationID, sdkgo.FailureAvailability, "provider request failed", 0)
	}
	// Closing a read response cannot change its outcome.
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		switch {
		case response.StatusCode == http.StatusTooManyRequests:
			return retryBranch[T](operationID, sdkgo.FailureRateLimit, "provider rate limit", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
		case response.StatusCode == http.StatusRequestTimeout || response.StatusCode == http.StatusConflict || response.StatusCode >= http.StatusInternalServerError:
			return retryBranch[T](operationID, sdkgo.FailureAvailability, "provider temporarily unavailable", providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
		case response.StatusCode == http.StatusNotFound && notFoundBranch != "providerRejected":
			return failedBranch[T](operationID, notFoundBranch, sdkgo.FailureNotFound, "provider resource was not found")
		case response.StatusCode == http.StatusUnauthorized:
			return failedBranch[T](operationID, "providerRejected", sdkgo.FailureAuthentication, "provider rejected the API key")
		case response.StatusCode == http.StatusForbidden:
			return failedBranch[T](operationID, "providerRejected", sdkgo.FailureAuthorization, "provider denied the request")
		default:
			return failedBranch[T](operationID, "providerRejected", sdkgo.FailureProviderRejection, "provider rejected HTTP "+strconv.Itoa(response.StatusCode))
		}
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return failedBranch[T](operationID, "invalidResponse", sdkgo.FailureResponseTooLarge, "provider response exceeded configured limit")
	}
	if err != nil {
		return retryBranch[T](operationID, sdkgo.FailureAvailability, "provider response was interrupted", 0)
	}
	value, err := decode(body)
	if err != nil {
		return failedBranch[T](operationID, "invalidResponse", sdkgo.FailureProtocol, "provider returned unusable JSON")
	}
	return sdkgo.NewQueryBranch(successBranch, value, nil, sdkgo.Receipt{})
}
func failedBranch[T any](operationID string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	var zero T
	failure := sdkgo.Failure{Kind: kind, Provider: "close", Operation: operationID, Message: message}
	return sdkgo.NewQueryBranch(branch, zero, &failure, sdkgo.Receipt{})
}
func retryBranch[T any](operationID string, kind sdkgo.FailureKind, message string, after time.Duration) sdkgo.QueryAttempt[T] {
	failure := sdkgo.Failure{Kind: kind, Provider: "close", Operation: operationID, Message: message}
	return sdkgo.NewQueryRetry[T](failure, after)
}
