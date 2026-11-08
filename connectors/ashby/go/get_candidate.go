// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// GetCandidateInput selects a candidate by its non-secret Ashby ID.
type GetCandidateInput struct {
	// CandidateID is the ID shown in Ashby Candidates or returned by the API.
	CandidateID string `json:"candidateId"`
}

// CandidateProfile excludes email, phone, files, and custom fields.
type CandidateProfile struct {
	// ID is the Ashby candidate UUID.
	ID string `json:"id"`
	// Name is the candidate display name.
	Name string `json:"name"`
	// CreatedAt is Ashby's creation timestamp.
	CreatedAt string `json:"createdAt"`
	// UpdatedAt is Ashby's update timestamp.
	UpdatedAt string `json:"updatedAt"`
}

// GetCandidateOperation reads one candidate's identity and state.
type GetCandidateOperation struct{ client *Client }

// GetCandidate binds the Query to this client.
func (client *Client) GetCandidate() GetCandidateOperation {
	return GetCandidateOperation{client}
}

// Definition returns the manifest-generated Query candidate.
func (GetCandidateOperation) Definition() sdkgo.QueryDefinition { return GetCandidateDefinition }

// Invoke reads one candidate and classifies retryable and definitive outcomes.
func (operation GetCandidateOperation) Invoke(call sdkgo.Call, input GetCandidateInput) sdkgo.QueryAttempt[CandidateProfile] {
	if !validCandidateID(input.CandidateID) {
		return candidateBranch(GetCandidateBranchDefect, sdkgo.FailureValidation, "candidate ID must be a UUID")
	}
	response := operation.client.postCandidateInfo(call, input.CandidateID)
	if errors.Is(response.err, errCredential) {
		return candidateBranch(GetCandidateBranchDefect, sdkgo.FailureAuthentication, "Ashby credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[CandidateProfile](candidateFailure(sdkgo.FailureTransport, "Ashby request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[CandidateProfile](candidateFailure(sdkgo.FailureRateLimit, "Ashby rate limited the request"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[CandidateProfile](candidateFailure(sdkgo.FailureAvailability, "Ashby is temporarily unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return candidateBranch(GetCandidateBranchNotFound, sdkgo.FailureNotFound, "Ashby did not expose the candidate")
	}
	if response.status != http.StatusOK {
		return candidateBranch(GetCandidateBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return candidateBranch(GetCandidateBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Ashby response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[CandidateProfile](candidateFailure(sdkgo.FailureTransport, "Ashby response was interrupted"), 0)
	}
	profile, rejectionCode, err := decodeCandidate(response.body, input.CandidateID)
	if err != nil {
		return candidateBranch(GetCandidateBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby returned invalid candidate JSON")
	}
	if rejectionCode == "candidate_not_found" {
		return candidateBranch(GetCandidateBranchNotFound, sdkgo.FailureNotFound, "Ashby did not expose the candidate")
	}
	if rejectionCode != "" {
		return candidateBranch(GetCandidateBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected the candidate lookup")
	}
	return sdkgo.NewQueryBranch(GetCandidateBranchFound, profile, nil, sdkgo.Receipt{})
}

var candidateIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[1-8][0-9a-fA-F]{3}-[89abAB][0-9a-fA-F]{3}-[0-9a-fA-F]{12}$`)

func validCandidateID(candidateID string) bool { return candidateIDPattern.MatchString(candidateID) }

func decodeCandidate(body []byte, requestedCandidateID string) (CandidateProfile, string, error) {
	var wire struct {
		Success *bool `json:"success"`
		Results struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			CreatedAt string `json:"createdAt"`
			UpdatedAt string `json:"updatedAt"`
		} `json:"results"`
		ErrorInfo struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return CandidateProfile{}, "", err
	}
	if wire.Success == nil {
		return CandidateProfile{}, "", errors.New("success missing")
	}
	if !*wire.Success {
		if strings.TrimSpace(wire.ErrorInfo.Code) == "" {
			return CandidateProfile{}, "", errors.New("error code missing")
		}
		return CandidateProfile{}, wire.ErrorInfo.Code, nil
	}
	if wire.Results.ID != requestedCandidateID || strings.TrimSpace(wire.Results.Name) == "" || strings.TrimSpace(wire.Results.CreatedAt) == "" || strings.TrimSpace(wire.Results.UpdatedAt) == "" {
		return CandidateProfile{}, "", fmt.Errorf("candidate identity or required profile fields missing")
	}
	return CandidateProfile{ID: wire.Results.ID, Name: wire.Results.Name, CreatedAt: wire.Results.CreatedAt, UpdatedAt: wire.Results.UpdatedAt}, "", nil
}

func candidateFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "ashby", Operation: "getCandidate", Message: message}
}

func candidateBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CandidateProfile] {
	failure := candidateFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, CandidateProfile{}, &failure, sdkgo.Receipt{})
}
