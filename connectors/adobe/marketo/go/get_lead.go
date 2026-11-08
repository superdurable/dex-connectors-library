// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"encoding/json"
	"errors"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// GetLeadInput selects one non-secret positive Marketo lead ID.
type GetLeadInput struct {
	// LeadID is the ID shown on a lead detail page or returned by a lead query.
	LeadID int64 `json:"leadId"`
}

// LeadProfile contains the selected ID and optional names, excluding email and other fields.
type LeadProfile struct {
	// ID is the Marketo lead ID.
	ID int64 `json:"id"`
	// FirstName is the lead's first name, or empty when absent.
	FirstName string `json:"firstName"`
	// LastName is the lead's last name, or empty when absent.
	LastName string `json:"lastName"`
}

// GetLeadOperation reads selected fields from one Marketo lead.
type GetLeadOperation struct{ client *Client }

// GetLead binds the Query to this client.
func (client *Client) GetLead() GetLeadOperation { return GetLeadOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetLeadOperation) Definition() sdkgo.QueryDefinition { return GetLeadDefinition }

// Invoke classifies Marketo transport, HTTP, and response-level outcomes.
func (operation GetLeadOperation) Invoke(call sdkgo.Call, input GetLeadInput) sdkgo.QueryAttempt[LeadProfile] {
	if input.LeadID <= 0 {
		return leadBranch(GetLeadBranchDefect, sdkgo.FailureValidation, "Marketo lead ID must be positive")
	}
	credentials, err := sdkgo.ResolveCredential(call.Context, operation.client.credentials, call, operation.client.refreshDriver)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return leadBranch(GetLeadBranchDefect, sdkgo.FailureAuthentication, "Marketo connection needs authorization")
		}
		return leadRetry(sdkgo.FailureTransport, "Marketo credential refresh failed", 0)
	}
	token := credentials.AccessToken.Reveal()
	if !providerhttp.IsHeaderSafeCredential(token) {
		return leadBranch(GetLeadBranchDefect, sdkgo.FailureAuthentication, "Marketo access token is unavailable")
	}
	target := operation.client.restEndpoint + "/v1/lead/" + strconv.FormatInt(input.LeadID, 10) + ".json?" + url.Values{"fields": {"id,firstName,lastName"}}.Encode()
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, target, nil)
	if err != nil {
		return leadBranch(GetLeadBranchDefect, sdkgo.FailureValidation, "Marketo request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := operation.client.httpClient.Do(request)
	if err != nil {
		return leadRetry(sdkgo.FailureTransport, "Marketo request failed", 0)
	}
	defer func() { _ = response.Body.Close() }()
	retryAfter := providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now())
	body, readErr := providerhttp.ReadBoundedBody(response.Body, operation.client.maxResponseBytes)
	if response.StatusCode == http.StatusTooManyRequests {
		return leadRetry(sdkgo.FailureRateLimit, "Marketo rate limited the request", retryAfter)
	}
	if response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500 {
		return leadRetry(sdkgo.FailureAvailability, "Marketo is temporarily unavailable", retryAfter)
	}
	if response.StatusCode != http.StatusOK {
		return leadBranch(GetLeadBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected HTTP "+strconv.Itoa(response.StatusCode))
	}
	if errors.Is(readErr, providerhttp.ErrBodyTooLarge) {
		return leadBranch(GetLeadBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Marketo response exceeded the configured limit")
	}
	if readErr != nil {
		return leadRetry(sdkgo.FailureTransport, "Marketo response was interrupted", 0)
	}
	return decodeLeadResponse(body, input.LeadID, retryAfter)
}

func decodeLeadResponse(body []byte, requestedID int64, retryAfter time.Duration) sdkgo.QueryAttempt[LeadProfile] {
	var envelope struct {
		Success *bool `json:"success"`
		Result  *[]struct {
			ID        int64   `json:"id"`
			FirstName *string `json:"firstName"`
			LastName  *string `json:"lastName"`
		} `json:"result"`
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Success == nil {
		return leadBranch(GetLeadBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned invalid lead JSON")
	}
	if !*envelope.Success {
		if len(envelope.Errors) == 0 {
			return leadBranch(GetLeadBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned an error without a code")
		}
		switch envelope.Errors[0].Code {
		case "601", "602":
			return leadBranch(GetLeadBranchProviderRejected, sdkgo.FailureAuthentication, "Marketo access token was rejected")
		case "606", "607", "615":
			return leadRetry(sdkgo.FailureRateLimit, "Marketo quota or concurrency limit was reached", retryAfter)
		case "604", "608", "611", "614", "500", "502":
			return leadRetry(sdkgo.FailureAvailability, "Marketo is temporarily unavailable", retryAfter)
		case "702":
			return leadBranch(GetLeadBranchNotFound, sdkgo.FailureNotFound, "Marketo lead was not found")
		default:
			return leadBranch(GetLeadBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the lead lookup")
		}
	}
	if envelope.Result == nil || len(*envelope.Result) > 1 {
		return leadBranch(GetLeadBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned an invalid lead result")
	}
	if len(*envelope.Result) == 0 {
		return leadBranch(GetLeadBranchNotFound, sdkgo.FailureNotFound, "Marketo lead was not found")
	}
	record := (*envelope.Result)[0]
	if record.ID != requestedID {
		return leadBranch(GetLeadBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo lead ID did not match the request")
	}
	profile := LeadProfile{ID: record.ID}
	if record.FirstName != nil {
		profile.FirstName = *record.FirstName
	}
	if record.LastName != nil {
		profile.LastName = *record.LastName
	}
	return sdkgo.NewQueryBranch(GetLeadBranchFound, profile, nil, sdkgo.Receipt{})
}
func leadFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: "getLead", Message: message}
}
func leadBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[LeadProfile] {
	failure := leadFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, LeadProfile{}, &failure, sdkgo.Receipt{})
}
func leadRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[LeadProfile] {
	return sdkgo.NewQueryRetry[LeadProfile](leadFailure(kind, message), delay)
}
