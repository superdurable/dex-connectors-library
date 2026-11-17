// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ashby

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateCandidateInput supplies the candidate's complete display name.
type CreateCandidateInput struct {
	// Name is the first and last name, up to 512 bytes; blank is invalid.
	Name string `json:"name"`
}

// CreateCandidateOperation creates one candidate with a bounded send count.
type CreateCandidateOperation struct{ client *Client }

// CreateCandidate binds the Mutation to this client.
func (client *Client) CreateCandidate() CreateCandidateOperation {
	return CreateCandidateOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (CreateCandidateOperation) Definition() sdkgo.MutationDefinition {
	return CreateCandidateDefinition
}

// IdempotencyKey records the stable Dex call identity; Ashby does not deduplicate by this key.
func (CreateCandidateOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCandidateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a candidate and repeats an ambiguous send only within the configured cap.
func (operation CreateCandidateOperation) Invoke(call sdkgo.Call, input CreateCandidateInput) sdkgo.MutationAttempt[CandidateProfile] {
	if strings.TrimSpace(input.Name) == "" || len(input.Name) > 512 {
		return createCandidateBranch(CreateCandidateBranchDefect, sdkgo.FailureValidation, "candidate name is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateCandidateDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createCandidateBranch(CreateCandidateBranchDefect, sdkgo.FailureLocalDefect, "Ashby create dispatch fence could not be initialized")
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationUncertain(CandidateProfile{}, createCandidateFailure(sdkgo.FailureTransport, "Ashby create checkpoint could not be trusted"), sdkgo.Receipt{})
	case singledispatch.ReadBackRequired:
		return sdkgo.NewMutationUncertain(CandidateProfile{}, createCandidateFailure(sdkgo.FailureTransport, "Ashby candidate may have been created, but no stable lookup key was supplied"), sdkgo.Receipt{})
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.postOperationWithContext(call, requestContext, "/candidate.create", input)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[CandidateProfile](createCandidateFailure(sdkgo.FailureAvailability, "Ashby create request was not dispatched"), 0)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CandidateProfile{}, createCandidateFailure(sdkgo.FailureAuthentication, "Ashby credential failed after an earlier possible candidate create"), sdkgo.Receipt{})
		}
		return createCandidateBranch(CreateCandidateBranchDefect, sdkgo.FailureAuthentication, "Ashby credentials are unavailable or invalid")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[CandidateProfile](createCandidateFailure(sdkgo.FailureAvailability, "Ashby create resend checkpoint was unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[CandidateProfile](createCandidateFailure(kind, "Ashby rejected candidate creation temporarily"), response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return sdkgo.NewMutationRetry[CandidateProfile](createCandidateFailure(sdkgo.FailureTransport, "Ashby candidate create outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CandidateProfile{}, createCandidateFailure(sdkgo.FailureProviderRejection, "an earlier Ashby candidate create may have succeeded"), sdkgo.Receipt{})
		}
		return createCandidateBranch(CreateCandidateBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected candidate creation with HTTP "+strconv.Itoa(response.status))
	}
	profile, rejectionCode, err := decodeCreatedCandidate(response.body, input.Name)
	if err != nil {
		return sdkgo.NewMutationRetry[CandidateProfile](createCandidateFailure(sdkgo.FailureProtocol, "Ashby accepted candidate creation but returned an unusable response"), 0)
	}
	if rejectionCode != "" {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CandidateProfile{}, createCandidateFailure(sdkgo.FailureProviderRejection, "an earlier Ashby candidate create may have succeeded"), sdkgo.Receipt{})
		}
		return createCandidateBranch(CreateCandidateBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected candidate creation")
	}
	return sdkgo.NewMutationBranch(CreateCandidateBranchCreated, profile, nil, sdkgo.Receipt{})
}

func decodeCreatedCandidate(body []byte, expectedName string) (CandidateProfile, string, error) {
	var wire struct {
		Success   *bool            `json:"success"`
		Results   CandidateProfile `json:"results"`
		ErrorInfo struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil {
		return CandidateProfile{}, "", errors.New("invalid Ashby candidate response")
	}
	if !*wire.Success {
		if wire.ErrorInfo.Code == "" {
			return CandidateProfile{}, "", errors.New("missing Ashby error code")
		}
		return CandidateProfile{}, wire.ErrorInfo.Code, nil
	}
	if !validCandidateID(wire.Results.ID) || wire.Results.Name != expectedName || wire.Results.CreatedAt == "" || wire.Results.UpdatedAt == "" {
		return CandidateProfile{}, "", errors.New("incomplete Ashby candidate")
	}
	return wire.Results, "", nil
}

func createCandidateFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "ashby", Operation: "createCandidate", Message: message}
}

func createCandidateBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CandidateProfile] {
	failure := createCandidateFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, CandidateProfile{}, &failure, sdkgo.Receipt{})
}
