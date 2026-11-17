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

// ListCandidateProjectsInput selects one page of a candidate's projects.
type ListCandidateProjectsInput struct {
	// CandidateID is the non-secret UUID of the candidate whose projects are listed.
	CandidateID string `json:"candidateId"`
	// Cursor is Ashby's opaque pagination cursor; blank starts at the first page.
	Cursor string `json:"cursor,omitempty"`
	// Limit caps the page at 100 projects; zero uses Ashby's default.
	Limit int `json:"limit,omitempty"`
}

// CandidateProjectSummary identifies a project without its description or members.
type CandidateProjectSummary struct {
	// ID is the project's UUID.
	ID string `json:"id"`
	// Title is the project's name.
	Title string `json:"title"`
	// CreatedAt is Ashby's creation timestamp in UTC.
	CreatedAt string `json:"createdAt"`
	// IsArchived reports whether the project is archived.
	IsArchived bool `json:"isArchived"`
}

// CandidateProjectPage is a bounded page with the next cursor when present.
type CandidateProjectPage struct {
	// Projects are the candidate's associated project identities.
	Projects []CandidateProjectSummary `json:"projects"`
	// NextCursor is passed to the next call when MoreDataAvailable is true.
	NextCursor string `json:"nextCursor,omitempty"`
	// MoreDataAvailable reports whether another page exists.
	MoreDataAvailable bool `json:"moreDataAvailable"`
}

// ListCandidateProjectsOperation reads candidate project membership.
type ListCandidateProjectsOperation struct{ client *Client }

// ListCandidateProjects binds the Query to this client.
func (client *Client) ListCandidateProjects() ListCandidateProjectsOperation {
	return ListCandidateProjectsOperation{client}
}

// Definition returns the manifest-generated Query definition.
func (ListCandidateProjectsOperation) Definition() sdkgo.QueryDefinition {
	return ListCandidateProjectsDefinition
}

// Invoke reads one page and omits project descriptions from the result.
func (operation ListCandidateProjectsOperation) Invoke(call sdkgo.Call, input ListCandidateProjectsInput) sdkgo.QueryAttempt[CandidateProjectPage] {
	if !validCandidateID(input.CandidateID) || input.Limit < 0 || input.Limit > 100 || len(input.Cursor) > 1024 {
		return candidateProjectsBranch(ListCandidateProjectsBranchDefect, sdkgo.FailureValidation, "candidate project page input is invalid")
	}
	response := operation.client.postOperation(call, "/candidate.listProjects", input)
	if errors.Is(response.err, errCredential) {
		return candidateProjectsBranch(ListCandidateProjectsBranchDefect, sdkgo.FailureAuthentication, "Ashby credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[CandidateProjectPage](candidateProjectsFailure(sdkgo.FailureTransport, "Ashby candidate projects request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[CandidateProjectPage](candidateProjectsFailure(sdkgo.FailureRateLimit, "Ashby rate limited candidate projects"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[CandidateProjectPage](candidateProjectsFailure(sdkgo.FailureAvailability, "Ashby candidate projects are unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return candidateProjectsBranch(ListCandidateProjectsBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected candidate projects with HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return candidateProjectsBranch(ListCandidateProjectsBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Ashby candidate project page exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[CandidateProjectPage](candidateProjectsFailure(sdkgo.FailureTransport, "Ashby candidate project response was interrupted"), 0)
	}
	var wire struct {
		Success           *bool                     `json:"success"`
		Results           []CandidateProjectSummary `json:"results"`
		NextCursor        string                    `json:"nextCursor"`
		MoreDataAvailable bool                      `json:"moreDataAvailable"`
		ErrorInfo         struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if json.Unmarshal(response.body, &wire) != nil || wire.Success == nil {
		return candidateProjectsBranch(ListCandidateProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby returned invalid candidate project page JSON")
	}
	if !*wire.Success {
		if strings.TrimSpace(wire.ErrorInfo.Code) == "" {
			return candidateProjectsBranch(ListCandidateProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby project rejection omitted its code")
		}
		return candidateProjectsBranch(ListCandidateProjectsBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected candidate projects")
	}
	if wire.Results == nil || wire.MoreDataAvailable && wire.NextCursor == "" {
		return candidateProjectsBranch(ListCandidateProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby candidate project page is incomplete")
	}
	for _, project := range wire.Results {
		if !validCandidateID(project.ID) || strings.TrimSpace(project.Title) == "" || project.CreatedAt == "" {
			return candidateProjectsBranch(ListCandidateProjectsBranchInvalidResponse, sdkgo.FailureProtocol, "Ashby candidate project page contains an invalid project")
		}
	}
	return sdkgo.NewQueryBranch(ListCandidateProjectsBranchListed, CandidateProjectPage{Projects: wire.Results, NextCursor: wire.NextCursor, MoreDataAvailable: wire.MoreDataAvailable}, nil, sdkgo.Receipt{})
}

func candidateProjectsFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "ashby", Operation: "listCandidateProjects", Message: message}
}

func candidateProjectsBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[CandidateProjectPage] {
	failure := candidateProjectsFailure(kind, message)
	return sdkgo.NewQueryBranch(branch, CandidateProjectPage{}, &failure, sdkgo.Receipt{})
}
