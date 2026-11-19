// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// CandidateProjectInput identifies the candidate and existing project to associate or disassociate.
type CandidateProjectInput struct {
	// CandidateID is the candidate UUID from Ashby Candidates or candidate.list.
	CandidateID string `json:"candidateId"`
	// ProjectID is the non-secret UUID of an existing Ashby candidate project.
	ProjectID string `json:"projectId"`
}

// AddCandidateProjectOperation associates an existing project with one candidate.
type AddCandidateProjectOperation struct{ client *Client }

// AddCandidateProject binds the Mutation to this client.
func (client *Client) AddCandidateProject() AddCandidateProjectOperation {
	return AddCandidateProjectOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (AddCandidateProjectOperation) Definition() sdkgo.MutationDefinition {
	return AddCandidateProjectDefinition
}

// IdempotencyKey records the stable call identity; Ashby does not receive this key.
func (AddCandidateProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ CandidateProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke adds a project, leaving the candidate-project membership unchanged on repetition.
func (operation AddCandidateProjectOperation) Invoke(call sdkgo.Call, input CandidateProjectInput) sdkgo.MutationAttempt[CandidateProfile] {
	return operation.client.mutateCandidateProject(call, input, "addCandidateProject", "/candidate.addProject", AddCandidateProjectBranchAdded, AddCandidateProjectBranchProviderRejected, AddCandidateProjectBranchInvalidResponse, AddCandidateProjectBranchDefect)
}

// RemoveCandidateProjectOperation removes an existing project from one candidate.
type RemoveCandidateProjectOperation struct{ client *Client }

// RemoveCandidateProject binds the Mutation to this client.
func (client *Client) RemoveCandidateProject() RemoveCandidateProjectOperation {
	return RemoveCandidateProjectOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (RemoveCandidateProjectOperation) Definition() sdkgo.MutationDefinition {
	return RemoveCandidateProjectDefinition
}

// IdempotencyKey records the stable call identity; Ashby does not receive this key.
func (RemoveCandidateProjectOperation) IdempotencyKey(callID sdkgo.CallID, _ CandidateProjectInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes a project, leaving the candidate-project membership unchanged on repetition.
func (operation RemoveCandidateProjectOperation) Invoke(call sdkgo.Call, input CandidateProjectInput) sdkgo.MutationAttempt[CandidateProfile] {
	return operation.client.mutateCandidateProject(call, input, "removeCandidateProject", "/candidate.removeProject", RemoveCandidateProjectBranchRemoved, RemoveCandidateProjectBranchProviderRejected, RemoveCandidateProjectBranchInvalidResponse, RemoveCandidateProjectBranchDefect)
}

func (client *Client) mutateCandidateProject(call sdkgo.Call, input CandidateProjectInput, operationName string, operationPath string, successBranch sdkgo.BranchID, rejectedBranch sdkgo.BranchID, invalidBranch sdkgo.BranchID, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[CandidateProfile] {
	failure := func(kind sdkgo.FailureKind, message string) sdkgo.Failure {
		return sdkgo.Failure{Kind: kind, Provider: "ashby", Operation: operationName, Message: message}
	}
	branch := func(branchID sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CandidateProfile] {
		problem := failure(kind, message)
		return sdkgo.NewMutationBranch(branchID, CandidateProfile{}, &problem, sdkgo.Receipt{})
	}
	if !validCandidateID(input.CandidateID) || !validCandidateID(input.ProjectID) {
		return branch(defectBranch, sdkgo.FailureValidation, "candidate or project ID is invalid")
	}
	response := client.postOperation(call, operationPath, input)
	if errors.Is(response.err, errCredential) {
		return branch(defectBranch, sdkgo.FailureAuthentication, "Ashby credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[CandidateProfile](failure(sdkgo.FailureTransport, "Ashby project request failed before dispatch"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[CandidateProfile](failure(sdkgo.FailureRateLimit, "Ashby rate limited candidate projects"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[CandidateProfile](failure(sdkgo.FailureAvailability, "Ashby candidate projects are unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return branch(rejectedBranch, sdkgo.FailureProviderRejection, "Ashby rejected candidate project request with HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return branch(invalidBranch, sdkgo.FailureResponseTooLarge, "Ashby candidate project response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[CandidateProfile](failure(sdkgo.FailureTransport, "Ashby candidate project response was interrupted"), 0)
	}
	profile, rejectionCode, err := decodeCandidate(response.body, input.CandidateID)
	if err != nil {
		return branch(invalidBranch, sdkgo.FailureProtocol, "Ashby returned invalid candidate project response JSON")
	}
	if rejectionCode != "" {
		return branch(rejectedBranch, sdkgo.FailureProviderRejection, "Ashby rejected candidate project request")
	}
	return sdkgo.NewMutationBranch(successBranch, profile, nil, sdkgo.Receipt{})
}
