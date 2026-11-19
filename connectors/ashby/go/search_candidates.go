// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// SearchCandidatesInput selects candidate matches by name, email, or both.
type SearchCandidatesInput struct {
	// Name is an optional candidate name, up to 512 bytes; one of Name or Email is required.
	Name string `json:"name,omitempty"`
	// Email is an optional complete email address, up to 320 bytes.
	Email string `json:"email,omitempty"`
	// Limit caps the response at 100 candidates; zero uses Ashby's default of 100.
	Limit int `json:"limit,omitempty"`
}

// CandidateSearchResults contains identity-only search matches.
type CandidateSearchResults struct {
	// Candidates omits contact details and all other sensitive profile fields.
	Candidates []CandidateProfile `json:"candidates"`
}

// SearchCandidatesOperation reads matching candidate identities.
type SearchCandidatesOperation struct{ client *Client }

// SearchCandidates binds the Query to this client.
func (client *Client) SearchCandidates() SearchCandidatesOperation {
	return SearchCandidatesOperation{client}
}

// Definition returns the manifest-generated Query definition.
func (SearchCandidatesOperation) Definition() sdkgo.QueryDefinition {
	return SearchCandidatesDefinition
}

// Invoke searches Ashby and classifies definitive and retryable outcomes.
func (operation SearchCandidatesOperation) Invoke(call sdkgo.Call, input SearchCandidatesInput) sdkgo.QueryAttempt[CandidateSearchResults] {
	if strings.TrimSpace(input.Name) == "" && strings.TrimSpace(input.Email) == "" || len(input.Name) > 512 || len(input.Email) > 320 || input.Limit < 0 || input.Limit > 100 {
		return searchCandidatesBranch(SearchCandidatesBranchDefect, sdkgo.FailureValidation, "candidate search input is invalid")
	}
	if input.Email != "" {
		address, err := mail.ParseAddress(input.Email)
		if err != nil || address.Address != input.Email {
			return searchCandidatesBranch(SearchCandidatesBranchDefect, sdkgo.FailureValidation, "candidate search email is invalid")
		}
	}
	response := operation.client.postOperation(call, "/candidate.search", input)
	if errors.Is(response.err, errCredential) {
		return searchCandidatesBranch(SearchCandidatesBranchDefect, sdkgo.FailureAuthentication, "Ashby credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[CandidateSearchResults](searchCandidatesFailure(sdkgo.FailureTransport, "Ashby candidate search request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[CandidateSearchResults](searchCandidatesFailure(sdkgo.FailureRateLimit, "Ashby rate limited candidate search"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[CandidateSearchResults](searchCandidatesFailure(sdkgo.FailureAvailability, "Ashby candidate search is unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return searchCandidatesBranch(SearchCandidatesBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected candidate search with HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return searchCandidatesBranch(SearchCandidatesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Ashby candidate search response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[CandidateSearchResults](searchCandidatesFailure(sdkgo.FailureTransport, "Ashby candidate search response was interrupted"), 0)
	}
	var wire struct {
		Success   *bool              `json:"success"`
		Results   []CandidateProfile `json:"results"`
		ErrorInfo struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Success == nil {
		return searchCandidatesBranch(SearchCandidatesBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby returned invalid candidate search JSON")
	}
	if !*wire.Success {
		if strings.TrimSpace(wire.ErrorInfo.Code) == "" {
			return searchCandidatesBranch(SearchCandidatesBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby candidate search rejection omitted its code")
		}
		return searchCandidatesBranch(SearchCandidatesBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected candidate search")
	}
	if wire.Results == nil {
		return searchCandidatesBranch(SearchCandidatesBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby candidate search results are missing")
	}
	for _, candidate := range wire.Results {
		if !validCandidateID(candidate.ID) || strings.TrimSpace(candidate.Name) == "" || candidate.CreatedAt == "" || candidate.UpdatedAt == "" {
			return searchCandidatesBranch(SearchCandidatesBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby candidate search returned an invalid identity")
		}
	}
	return sdkgo.NewQueryBranch(SearchCandidatesBranchFound, CandidateSearchResults{Candidates: wire.Results}, nil, sdkgo.Receipt{})
}

func searchCandidatesFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "ashby", Operation: "searchCandidates", Message: message}
}

func searchCandidatesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CandidateSearchResults] {
	failure := searchCandidatesFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, CandidateSearchResults{}, &failure, sdkgo.Receipt{})
}
