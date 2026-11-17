// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// ListCandidatesInput selects one page of Ashby candidates.
type ListCandidatesInput struct {
	// Cursor is "start" for the first page or the previous nextCursor; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
	// Limit caps the page at 100 candidates; zero uses Ashby's default.
	Limit int `json:"limit,omitempty"`
}

// CandidatePage exposes candidate identities and the cursor for the next page.
type CandidatePage struct {
	// Candidates omits contact details and other sensitive profile fields.
	Candidates []CandidateProfile `json:"candidates"`
	// NextCursor is the cursor to pass to a later call when moreDataAvailable is true.
	NextCursor string `json:"nextCursor,omitempty"`
	// MoreDataAvailable reports whether another page exists.
	MoreDataAvailable bool `json:"moreDataAvailable"`
	// SyncToken is supplied by Ashby on the last page for later incremental synchronization.
	SyncToken string `json:"syncToken,omitempty"`
}

// ListCandidatesOperation reads one bounded candidate page.
type ListCandidatesOperation struct{ client *Client }

// ListCandidates binds the Query to this client.
func (client *Client) ListCandidates() ListCandidatesOperation {
	return ListCandidatesOperation{client}
}

// Definition returns the manifest-generated Query definition.
func (ListCandidatesOperation) Definition() sdkgo.QueryDefinition { return ListCandidatesDefinition }

// Invoke reads a page and classifies retryable and definitive provider outcomes.
func (operation ListCandidatesOperation) Invoke(call sdkgo.Call, input ListCandidatesInput) sdkgo.QueryAttempt[CandidatePage] {
	if input.Limit < 0 || input.Limit > 100 || len(input.Cursor) > 1024 {
		return listCandidatesBranch(ListCandidatesBranchDefect, sdkgo.FailureValidation, "candidate page input is invalid")
	}
	requestBody := map[string]any{}
	if input.Cursor != "" {
		requestBody["cursor"] = input.Cursor
	}
	if input.Limit != 0 {
		requestBody["limit"] = input.Limit
	}
	response := operation.client.postOperation(call, "/candidate.list", requestBody)
	if errors.Is(response.err, errCredential) {
		return listCandidatesBranch(ListCandidatesBranchDefect, sdkgo.FailureAuthentication, "Ashby credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[CandidatePage](listCandidatesFailure(sdkgo.FailureTransport, "Ashby candidate list request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[CandidatePage](listCandidatesFailure(sdkgo.FailureRateLimit, "Ashby rate limited candidate listing"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[CandidatePage](listCandidatesFailure(sdkgo.FailureAvailability, "Ashby candidate listing is unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return listCandidatesBranch(ListCandidatesBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected candidate listing with HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return listCandidatesBranch(ListCandidatesBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Ashby candidate page exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[CandidatePage](listCandidatesFailure(sdkgo.FailureTransport, "Ashby candidate page was interrupted"), 0)
	}
	var wire struct {
		Success           *bool              `json:"success"`
		Results           []CandidateProfile `json:"results"`
		NextCursor        string             `json:"nextCursor"`
		MoreDataAvailable bool               `json:"moreDataAvailable"`
		SyncToken         string             `json:"syncToken"`
		ErrorInfo         struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Success == nil {
		return listCandidatesBranch(ListCandidatesBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby returned invalid candidate page JSON")
	}
	if !*wire.Success {
		if strings.TrimSpace(wire.ErrorInfo.Code) == "" {
			return listCandidatesBranch(ListCandidatesBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby candidate page rejection omitted its code")
		}
		return listCandidatesBranch(ListCandidatesBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected candidate listing")
	}
	if wire.Results == nil || wire.MoreDataAvailable && wire.NextCursor == "" {
		return listCandidatesBranch(ListCandidatesBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby candidate page is incomplete")
	}
	for _, candidate := range wire.Results {
		if !validCandidateID(candidate.ID) || strings.TrimSpace(candidate.Name) == "" {
			return listCandidatesBranch(ListCandidatesBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby candidate page contains an invalid identity")
		}
	}
	return sdkgo.NewQueryBranch(ListCandidatesBranchListed, CandidatePage{Candidates: wire.Results, NextCursor: wire.NextCursor, MoreDataAvailable: wire.MoreDataAvailable, SyncToken: wire.SyncToken}, nil, sdkgo.Receipt{})
}

func listCandidatesBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CandidatePage] {
	failure := listCandidatesFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, CandidatePage{}, &failure, sdkgo.Receipt{})
}

func listCandidatesFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "ashby", Operation: "listCandidates", Message: message}
}

// UpdateCandidateInput replaces the candidate's display name.
type UpdateCandidateInput struct {
	// CandidateID is the non-secret UUID of the candidate to rename.
	CandidateID string `json:"candidateId"`
	// Name is the complete new display name.
	Name string `json:"name"`
}

// UpdateCandidateOperation sets a candidate name to an absolute value.
type UpdateCandidateOperation struct{ client *Client }

// UpdateCandidate binds the Mutation to this client.
func (client *Client) UpdateCandidate() UpdateCandidateOperation {
	return UpdateCandidateOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (UpdateCandidateOperation) Definition() sdkgo.MutationDefinition {
	return UpdateCandidateDefinition
}

// IdempotencyKey derives the receipt identity from the stable Dex call ID; Ashby does not receive this key.
func (UpdateCandidateOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateCandidateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the name; repeating the same request leaves the same candidate state.
func (operation UpdateCandidateOperation) Invoke(call sdkgo.Call, input UpdateCandidateInput) sdkgo.MutationAttempt[CandidateProfile] {
	if !validCandidateID(input.CandidateID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 512 {
		return updateCandidateBranch(UpdateCandidateBranchDefect, sdkgo.FailureValidation, "candidate ID or name is invalid")
	}
	response := operation.client.postOperation(call, "/candidate.update", input)
	if errors.Is(response.err, errCredential) {
		return updateCandidateBranch(UpdateCandidateBranchDefect, sdkgo.FailureAuthentication, "Ashby credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[CandidateProfile](updateCandidateFailure(sdkgo.FailureTransport, "Ashby candidate update request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[CandidateProfile](updateCandidateFailure(sdkgo.FailureRateLimit, "Ashby rate limited candidate updating"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[CandidateProfile](updateCandidateFailure(sdkgo.FailureAvailability, "Ashby candidate updating is unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return updateCandidateBranch(UpdateCandidateBranchNotFound, sdkgo.FailureNotFound, "Ashby did not expose the candidate")
	}
	if response.status != http.StatusOK {
		return updateCandidateBranch(UpdateCandidateBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected candidate updating with HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return updateCandidateBranch(UpdateCandidateBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Ashby candidate update response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[CandidateProfile](updateCandidateFailure(sdkgo.FailureTransport, "Ashby candidate update response was interrupted"), 0)
	}
	profile, rejectionCode, err := decodeCandidate(response.body, input.CandidateID)
	if err != nil {
		return updateCandidateBranch(UpdateCandidateBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby returned invalid updated candidate JSON")
	}
	if rejectionCode == "candidate_not_found" {
		return updateCandidateBranch(UpdateCandidateBranchNotFound, sdkgo.FailureNotFound, "Ashby did not expose the candidate")
	}
	if rejectionCode != "" {
		return updateCandidateBranch(UpdateCandidateBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected candidate updating")
	}
	if profile.Name != input.Name {
		return updateCandidateBranch(UpdateCandidateBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby returned a different candidate name")
	}
	return sdkgo.NewMutationBranch(UpdateCandidateBranchUpdated, profile, nil, sdkgo.Receipt{})
}

func updateCandidateFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "ashby", Operation: "updateCandidate", Message: message}
}

func updateCandidateBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CandidateProfile] {
	failure := updateCandidateFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, CandidateProfile{}, &failure, sdkgo.Receipt{})
}
