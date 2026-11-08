// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package greenhouse

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetCandidateInput selects a non-secret Greenhouse candidate ID.
type GetCandidateInput struct {
	// CandidateID is the positive integer ID from a Greenhouse candidate page or API list.
	CandidateID int64 `json:"candidateId"`
}

// CandidateProfile excludes email addresses, phone numbers, resumes, and notes.
type CandidateProfile struct {
	// ID is the Greenhouse candidate ID.
	ID int64 `json:"id"`
	// FirstName is the candidate's first name, or empty when Greenhouse stores null.
	FirstName string `json:"firstName"`
	// LastName is the candidate's last name, or empty when Greenhouse stores null.
	LastName string `json:"lastName"`
}

// GetCandidateOperation reads one candidate's selected profile fields.
type GetCandidateOperation struct{ client *Client }

// GetCandidate binds the Query to this client.
func (client *Client) GetCandidate() GetCandidateOperation { return GetCandidateOperation{client} }

// Definition returns the manifest-generated Query contract.
func (GetCandidateOperation) Definition() sdkgo.QueryDefinition { return GetCandidateDefinition }

// Invoke reads one candidate and classifies retryable and definitive outcomes.
func (operation GetCandidateOperation) Invoke(call sdkgo.Call, input GetCandidateInput) sdkgo.QueryAttempt[CandidateProfile] {
	if input.CandidateID <= 0 {
		return candidateBranch(GetCandidateBranchDefect, sdkgo.FailureValidation, "candidate ID must be positive")
	}
	response := operation.client.get(call, input.CandidateID)
	if errors.Is(response.err, errCredential) {
		return candidateBranch(GetCandidateBranchDefect, sdkgo.FailureAuthentication, "Greenhouse access token is unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[CandidateProfile](candidateFailure(sdkgo.FailureTransport, "Greenhouse request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[CandidateProfile](candidateFailure(sdkgo.FailureRateLimit, "Greenhouse rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[CandidateProfile](candidateFailure(sdkgo.FailureAvailability, "Greenhouse is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return candidateBranch(GetCandidateBranchNotFound, sdkgo.FailureNotFound, "Greenhouse did not expose the candidate")
	}
	if response.status != http.StatusOK {
		return candidateBranch(GetCandidateBranchProviderRejected, sdkgo.FailureProviderRejection, "Greenhouse rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return candidateBranch(GetCandidateBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Greenhouse response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[CandidateProfile](candidateFailure(sdkgo.FailureTransport, "Greenhouse response was interrupted"), 0)
	}
	profile, found, err := decodeCandidate(response.body, input.CandidateID)
	if err != nil {
		return candidateBranch(GetCandidateBranchInvalidResponse, sdkgo.FailureProtocol, "Greenhouse returned invalid candidate JSON")
	}
	if !found {
		return candidateBranch(GetCandidateBranchNotFound, sdkgo.FailureNotFound, "Greenhouse did not expose the candidate")
	}
	return sdkgo.NewQueryBranch(GetCandidateBranchFound, profile, nil, sdkgo.Receipt{})
}

func decodeCandidate(body []byte, requestedID int64) (CandidateProfile, bool, error) {
	var records []struct {
		ID        int64   `json:"id"`
		FirstName *string `json:"first_name"`
		LastName  *string `json:"last_name"`
	}
	if err := json.Unmarshal(body, &records); err != nil || records == nil || len(records) > 1 {
		return CandidateProfile{}, false, errors.New("invalid candidate list")
	}
	if len(records) == 0 {
		return CandidateProfile{}, false, nil
	}
	if records[0].ID != requestedID {
		return CandidateProfile{}, false, errors.New("candidate ID mismatch")
	}
	profile := CandidateProfile{ID: records[0].ID}
	if records[0].FirstName != nil {
		profile.FirstName = *records[0].FirstName
	}
	if records[0].LastName != nil {
		profile.LastName = *records[0].LastName
	}
	if strings.TrimSpace(profile.FirstName) == "" && strings.TrimSpace(profile.LastName) == "" {
		return CandidateProfile{}, false, errors.New("candidate name missing")
	}
	return profile, true, nil
}

func candidateFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "greenhouse", Operation: "getCandidate", Message: message}
}
func candidateBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CandidateProfile] {
	failure := candidateFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, CandidateProfile{}, &failure, sdkgo.Receipt{})
}
