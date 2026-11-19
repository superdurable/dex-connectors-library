// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package greenhouse

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListCandidatesInput selects one page of candidate profiles.
type ListCandidatesInput struct {
	// PageSize is 1 to 500; zero selects Greenhouse's default page size.
	PageSize int `json:"pageSize,omitempty"`
	// Cursor is the opaque value returned by a preceding page; leave blank for the first page.
	Cursor string `json:"cursor,omitempty"`
}

// CandidateProfilesPage contains selected profile fields and an optional next cursor.
type CandidateProfilesPage struct {
	// Candidates contains only IDs and names, without contact information.
	Candidates []CandidateProfile `json:"candidates"`
	// NextCursor is blank when the last page has been read.
	NextCursor string `json:"nextCursor,omitempty"`
}

// ListCandidatesOperation reads one page through Harvest v3.
type ListCandidatesOperation struct{ client *Client }

// ListCandidates binds the Query to this client.
func (client *Client) ListCandidates() ListCandidatesOperation {
	return ListCandidatesOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (ListCandidatesOperation) Definition() sdkgo.QueryDefinition { return ListCandidatesDefinition }

// Invoke reads a page and classifies retryable and definitive outcomes.
func (operation ListCandidatesOperation) Invoke(call sdkgo.Call, input ListCandidatesInput) sdkgo.QueryAttempt[CandidateProfilesPage] {
	if input.PageSize < 0 || input.PageSize > 500 || input.Cursor != "" && (input.PageSize != 0 || len(input.Cursor) > 4096 || strings.TrimSpace(input.Cursor) == "") {
		return candidatePageBranch(ListCandidatesBranchDefect, sdkgo.FailureValidation, "page size or cursor is invalid")
	}
	query := url.Values{}
	if input.Cursor != "" {
		query.Set("cursor", input.Cursor)
	} else {
		if input.PageSize > 0 {
			query.Set("per_page", strconv.Itoa(input.PageSize))
		}
		query.Set("fields", "id,first_name,last_name")
	}
	response := operation.client.getCandidates(call, query)
	if errors.Is(response.err, errCredential) {
		return candidatePageBranch(ListCandidatesBranchDefect, sdkgo.FailureAuthentication, "Greenhouse access token is unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[CandidateProfilesPage](candidatePageFailure(sdkgo.FailureTransport, "Greenhouse request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[CandidateProfilesPage](candidatePageFailure(sdkgo.FailureRateLimit, "Greenhouse rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[CandidateProfilesPage](candidatePageFailure(sdkgo.FailureAvailability, "Greenhouse is temporarily unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return candidatePageBranch(ListCandidatesBranchProviderRejected, sdkgo.FailureProviderRejection, "Greenhouse rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return candidatePageBranch(ListCandidatesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Greenhouse response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[CandidateProfilesPage](candidatePageFailure(sdkgo.FailureTransport, "Greenhouse response was interrupted"), 0)
	}
	page, err := decodeCandidatePage(response.body, response.link, operation.client.baseURL)
	if err != nil {
		return candidatePageBranch(ListCandidatesBranchInvalidResponse, sdkgo.FailureProtocol, "Greenhouse returned an invalid candidate page")
	}
	return sdkgo.NewQueryBranch(ListCandidatesBranchListed, page, nil, sdkgo.Receipt{})
}

func decodeCandidatePage(body []byte, link string, baseURL string) (CandidateProfilesPage, error) {
	var records []struct {
		ID        int64   `json:"id"`
		FirstName *string `json:"first_name"`
		LastName  *string `json:"last_name"`
	}
	if err := json.Unmarshal(body, &records); err != nil || records == nil || len(records) > 500 {
		return CandidateProfilesPage{}, errors.New("invalid candidate list")
	}
	page := CandidateProfilesPage{Candidates: make([]CandidateProfile, 0, len(records))}
	for _, record := range records {
		if record.ID <= 0 {
			return CandidateProfilesPage{}, errors.New("invalid candidate ID")
		}
		profile := CandidateProfile{ID: record.ID}
		if record.FirstName != nil {
			profile.FirstName = *record.FirstName
		}
		if record.LastName != nil {
			profile.LastName = *record.LastName
		}
		page.Candidates = append(page.Candidates, profile)
	}
	if link == "" {
		return page, nil
	}
	for _, entry := range strings.Split(link, ",") {
		parts := strings.Split(strings.TrimSpace(entry), ";")
		if len(parts) != 2 || strings.TrimSpace(parts[1]) != `rel="next"` {
			continue
		}
		reference := strings.TrimSpace(parts[0])
		if !strings.HasPrefix(reference, "<") || !strings.HasSuffix(reference, ">") {
			return CandidateProfilesPage{}, errors.New("invalid next link")
		}
		nextURL, err := url.Parse(strings.TrimSuffix(strings.TrimPrefix(reference, "<"), ">"))
		if err != nil || nextURL.Scheme+"://"+nextURL.Host+nextURL.Path != baseURL+"/candidates" || len(nextURL.Query()) != 1 || nextURL.Query().Get("cursor") == "" {
			return CandidateProfilesPage{}, errors.New("invalid next cursor")
		}
		page.NextCursor = nextURL.Query().Get("cursor")
		return page, nil
	}
	return page, nil
}

func candidatePageFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "greenhouse", Operation: "listCandidates", Message: message}
}

func candidatePageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CandidateProfilesPage] {
	failure := candidatePageFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, CandidateProfilesPage{}, &failure, sdkgo.Receipt{})
}
