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

// CandidateTagInput identifies the candidate and existing tag to associate or disassociate.
type CandidateTagInput struct {
	// CandidateID is the candidate UUID from Ashby Candidates or candidate.list.
	CandidateID string `json:"candidateId"`
	// TagID is the non-secret UUID of an existing Ashby candidate tag.
	TagID string `json:"tagId"`
}

// AddCandidateTagOperation associates an existing tag with one candidate.
type AddCandidateTagOperation struct{ client *Client }

// AddCandidateTag binds the Mutation to this client.
func (client *Client) AddCandidateTag() AddCandidateTagOperation {
	return AddCandidateTagOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (AddCandidateTagOperation) Definition() sdkgo.MutationDefinition {
	return AddCandidateTagDefinition
}

// IdempotencyKey records the stable call identity; Ashby does not receive this key.
func (AddCandidateTagOperation) IdempotencyKey(callID sdkgo.CallID, _ CandidateTagInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke adds a tag, leaving the candidate-tag membership unchanged on repetition.
func (operation AddCandidateTagOperation) Invoke(call sdkgo.Call, input CandidateTagInput) sdkgo.MutationAttempt[CandidateProfile] {
	return operation.client.mutateCandidateTag(call, input, "addCandidateTag", "/candidate.addTag", AddCandidateTagBranchAdded, AddCandidateTagBranchProviderRejected, AddCandidateTagBranchInvalidResponse, AddCandidateTagBranchDefect)
}

// RemoveCandidateTagOperation removes an existing tag from one candidate.
type RemoveCandidateTagOperation struct{ client *Client }

// RemoveCandidateTag binds the Mutation to this client.
func (client *Client) RemoveCandidateTag() RemoveCandidateTagOperation {
	return RemoveCandidateTagOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (RemoveCandidateTagOperation) Definition() sdkgo.MutationDefinition {
	return RemoveCandidateTagDefinition
}

// IdempotencyKey records the stable call identity; Ashby does not receive this key.
func (RemoveCandidateTagOperation) IdempotencyKey(callID sdkgo.CallID, _ CandidateTagInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke removes a tag, leaving the candidate-tag membership unchanged on repetition.
func (operation RemoveCandidateTagOperation) Invoke(call sdkgo.Call, input CandidateTagInput) sdkgo.MutationAttempt[CandidateProfile] {
	return operation.client.mutateCandidateTag(call, input, "removeCandidateTag", "/candidate.removeTag", RemoveCandidateTagBranchRemoved, RemoveCandidateTagBranchProviderRejected, RemoveCandidateTagBranchInvalidResponse, RemoveCandidateTagBranchDefect)
}

func (client *Client) mutateCandidateTag(call sdkgo.Call, input CandidateTagInput, operationName string, operationPath string, successBranch sdkgo.BranchID, rejectedBranch sdkgo.BranchID, invalidBranch sdkgo.BranchID, defectBranch sdkgo.BranchID) sdkgo.MutationAttempt[CandidateProfile] {
	failure := func(kind sdkgo.FailureKind, message string) sdkgo.Failure {
		return sdkgo.Failure{Kind: kind, Provider: "ashby", Operation: operationName, Message: message}
	}
	branch := func(branchID sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CandidateProfile] {
		problem := failure(kind, message)
		return sdkgo.NewMutationBranch(branchID, CandidateProfile{}, &problem, sdkgo.Receipt{})
	}
	if !validCandidateID(input.CandidateID) || !validCandidateID(input.TagID) {
		return branch(defectBranch, sdkgo.FailureValidation, "candidate or tag ID is invalid")
	}
	response := client.postOperation(call, operationPath, input)
	if errors.Is(response.err, errCredential) {
		return branch(defectBranch, sdkgo.FailureAuthentication, "Ashby credentials are unavailable or invalid")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewMutationRetry[CandidateProfile](failure(sdkgo.FailureTransport, "Ashby tag request failed before dispatch"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[CandidateProfile](failure(sdkgo.FailureRateLimit, "Ashby rate limited candidate tags"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewMutationRetry[CandidateProfile](failure(sdkgo.FailureAvailability, "Ashby candidate tags are unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return branch(rejectedBranch, sdkgo.FailureProviderRejection, "Ashby rejected candidate tag request with HTTP "+strconv.Itoa(response.status))
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return branch(invalidBranch, sdkgo.FailureResponseTooLarge, "Ashby candidate tag response exceeded the configured limit")
	}
	if response.err != nil {
		return sdkgo.NewMutationRetry[CandidateProfile](failure(sdkgo.FailureTransport, "Ashby candidate tag response was interrupted"), 0)
	}
	profile, rejectionCode, err := decodeCandidate(response.body, input.CandidateID)
	if err != nil {
		return branch(invalidBranch, sdkgo.FailureProtocol, "Ashby returned invalid tagged candidate JSON")
	}
	if rejectionCode != "" {
		return branch(rejectedBranch, sdkgo.FailureProviderRejection, "Ashby rejected candidate tag request")
	}
	return sdkgo.NewMutationBranch(successBranch, profile, nil, sdkgo.Receipt{})
}
