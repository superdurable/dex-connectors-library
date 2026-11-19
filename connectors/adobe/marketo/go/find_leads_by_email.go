// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package marketo

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// FindLeadsByEmailInput selects a page of Marketo leads with one exact email.
type FindLeadsByEmailInput struct {
	// Email is the address to match; it is sent as a query value and excluded from results.
	Email string `json:"email"`
	// NextPageToken is an opaque token returned by a previous page; blank starts a search.
	NextPageToken string `json:"nextPageToken,omitempty"`
}

// LeadProfilesPage contains matching IDs and names without email addresses.
type LeadProfilesPage struct {
	// Leads contains the matching lead profiles.
	Leads []LeadProfile `json:"leads"`
	// NextPageToken selects the next page, or is blank when no more results remain.
	NextPageToken string `json:"nextPageToken"`
}

// FindLeadsByEmailOperation searches lead profiles by exact email.
type FindLeadsByEmailOperation struct{ client *Client }

// FindLeadsByEmail binds the Query to this client.
func (client *Client) FindLeadsByEmail() FindLeadsByEmailOperation {
	return FindLeadsByEmailOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (FindLeadsByEmailOperation) Definition() sdkgo.QueryDefinition {
	return FindLeadsByEmailDefinition
}

// Invoke reads one matching page and omits the email from the result.
func (operation FindLeadsByEmailOperation) Invoke(call sdkgo.Call, input FindLeadsByEmailInput) sdkgo.QueryAttempt[LeadProfilesPage] {
	if !validLeadEmail(input.Email) || len(input.NextPageToken) > 2048 || strings.ContainsAny(input.NextPageToken, "\r\n") {
		return findLeadsBranch(FindLeadsByEmailBranchDefect, sdkgo.FailureValidation, "email or page token is invalid")
	}
	query := url.Values{"filterType": {"email"}, "filterValues": {input.Email}, "fields": {"id,firstName,lastName"}}
	if input.NextPageToken != "" {
		query.Set("nextPageToken", input.NextPageToken)
	}
	response := operation.client.requestLeadAPI(call, http.MethodGet, "/v1/leads.json?"+query.Encode(), nil)
	if errors.Is(response.err, errLeadReauthorization) {
		return findLeadsBranch(FindLeadsByEmailBranchDefect, sdkgo.FailureAuthentication, "Marketo connection needs authorization")
	}
	if errors.Is(response.err, errLeadCredentials) || errors.Is(response.err, errLeadTransport) {
		return findLeadsRetry(sdkgo.FailureTransport, "Marketo request or credential refresh failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return findLeadsRetry(sdkgo.FailureRateLimit, "Marketo rate limited the request", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return findLeadsRetry(sdkgo.FailureAvailability, "Marketo is temporarily unavailable", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return findLeadsBranch(FindLeadsByEmailBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return findLeadsBranch(FindLeadsByEmailBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Marketo response exceeded the configured limit")
	}
	if response.err != nil {
		return findLeadsRetry(sdkgo.FailureTransport, "Marketo response was interrupted", 0)
	}
	var envelope struct {
		Success       *bool          `json:"success"`
		Result        *[]LeadProfile `json:"result"`
		NextPageToken string         `json:"nextPageToken"`
		Errors        []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(response.body, &envelope); err != nil || envelope.Success == nil {
		return findLeadsBranch(FindLeadsByEmailBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned invalid lead search JSON")
	}
	if !*envelope.Success {
		return classifyFindLeadsError(envelope.Errors, response.retryAfter)
	}
	if envelope.Result == nil {
		return findLeadsBranch(FindLeadsByEmailBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo omitted the lead search result")
	}
	for _, lead := range *envelope.Result {
		if lead.ID <= 0 {
			return findLeadsBranch(FindLeadsByEmailBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned a lead without ID")
		}
	}
	return sdkgo.NewQueryBranch(FindLeadsByEmailBranchFound, LeadProfilesPage{Leads: *envelope.Result, NextPageToken: envelope.NextPageToken}, nil, sdkgo.Receipt{})
}

func validLeadEmail(email string) bool {
	if email == "" || strings.TrimSpace(email) != email {
		return false
	}
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email
}

func classifyFindLeadsError(errors []struct {
	Code string `json:"code"`
}, retryAfter time.Duration) sdkgo.QueryAttempt[LeadProfilesPage] {
	if len(errors) == 0 {
		return findLeadsBranch(FindLeadsByEmailBranchInvalidResponse, sdkgo.FailureProtocol, "Marketo returned an error without code")
	}
	switch errors[0].Code {
	case "602", "606", "607", "615":
		return findLeadsRetry(sdkgo.FailureRateLimit, "Marketo token or quota temporarily blocked the search", retryAfter)
	case "604", "608", "611", "614", "500", "502":
		return findLeadsRetry(sdkgo.FailureAvailability, "Marketo is temporarily unavailable", retryAfter)
	default:
		return findLeadsBranch(FindLeadsByEmailBranchProviderRejected, sdkgo.FailureProviderRejection, "Marketo rejected the lead search")
	}
}

func findLeadsFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "adobe-marketo", Operation: "findLeadsByEmail", Message: message}
}

func findLeadsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[LeadProfilesPage] {
	failure := findLeadsFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, LeadProfilesPage{}, &failure, sdkgo.Receipt{})
}

func findLeadsRetry(kind sdkgo.FailureKind, message string, delay time.Duration) sdkgo.QueryAttempt[LeadProfilesPage] {
	return sdkgo.NewQueryRetry[LeadProfilesPage](findLeadsFailure(kind, message), delay)
}
