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

// CreateCandidateNoteInput supplies a candidate ID and plaintext note.
type CreateCandidateNoteInput struct {
	// CandidateID is the non-secret UUID of the candidate.
	CandidateID string `json:"candidateId"`
	// Note is plaintext content, up to 8192 bytes; do not include secrets.
	Note string `json:"note"`
}

// CreateCandidateNoteOperation creates one candidate note with a bounded send count.
type CreateCandidateNoteOperation struct{ client *Client }

// CreateCandidateNote binds the Mutation to this client.
func (client *Client) CreateCandidateNote() CreateCandidateNoteOperation {
	return CreateCandidateNoteOperation{client}
}

// Definition returns the manifest-generated Mutation definition.
func (CreateCandidateNoteOperation) Definition() sdkgo.MutationDefinition {
	return CreateCandidateNoteDefinition
}

// IdempotencyKey records the stable Dex call identity; Ashby does not deduplicate by this key.
func (CreateCandidateNoteOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateCandidateNoteInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a candidate and repeats an ambiguous send only within the configured cap.
func (operation CreateCandidateNoteOperation) Invoke(call sdkgo.Call, input CreateCandidateNoteInput) sdkgo.MutationAttempt[CandidateNoteSummary] {
	if !validCandidateID(input.CandidateID) || strings.TrimSpace(input.Note) == "" || len(input.Note) > 8192 {
		return createCandidateNoteBranch(CreateCandidateNoteBranchDefect, sdkgo.FailureValidation, "candidate note is invalid")
	}
	fence, err := singledispatch.NewFence(call, CreateCandidateNoteDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return createCandidateNoteBranch(CreateCandidateNoteBranchDefect, sdkgo.FailureLocalDefect, "Ashby create dispatch fence could not be initialized")
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationUncertain(CandidateNoteSummary{}, createCandidateNoteFailure(sdkgo.FailureTransport, "Ashby create checkpoint could not be trusted"), sdkgo.Receipt{})
	case singledispatch.ReadBackRequired:
		return sdkgo.NewMutationUncertain(CandidateNoteSummary{}, createCandidateNoteFailure(sdkgo.FailureTransport, "Ashby candidate note may have been created, but no stable lookup key was supplied"), sdkgo.Receipt{})
	}
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = operation.client.postOperationWithContext(call, requestContext, "/candidate.createNote", input)
		return nil
	})
	if err != nil {
		return sdkgo.NewMutationRetry[CandidateNoteSummary](createCandidateNoteFailure(sdkgo.FailureAvailability, "Ashby create request was not dispatched"), 0)
	}
	if errors.Is(response.err, errCredential) {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CandidateNoteSummary{}, createCandidateNoteFailure(sdkgo.FailureAuthentication, "Ashby credential failed after an earlier possible candidate note create"), sdkgo.Receipt{})
		}
		return createCandidateNoteBranch(CreateCandidateNoteBranchDefect, sdkgo.FailureAuthentication, "Ashby credentials are unavailable or invalid")
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return sdkgo.NewMutationRetry[CandidateNoteSummary](createCandidateNoteFailure(sdkgo.FailureAvailability, "Ashby create resend checkpoint was unavailable"), 0)
		}
		kind := sdkgo.FailureAvailability
		if response.status == http.StatusTooManyRequests {
			kind = sdkgo.FailureRateLimit
		}
		return sdkgo.NewMutationRetry[CandidateNoteSummary](createCandidateNoteFailure(kind, "Ashby rejected candidate note creation temporarily"), response.retryAfter)
	}
	if errors.Is(response.err, errRequest) || response.status == http.StatusRequestTimeout || response.status >= 500 || response.err != nil {
		return sdkgo.NewMutationRetry[CandidateNoteSummary](createCandidateNoteFailure(sdkgo.FailureTransport, "Ashby candidate note create outcome is unknown"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CandidateNoteSummary{}, createCandidateNoteFailure(sdkgo.FailureProviderRejection, "an earlier Ashby candidate note create may have succeeded"), sdkgo.Receipt{})
		}
		return createCandidateNoteBranch(CreateCandidateNoteBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected candidate note creation with HTTP "+strconv.Itoa(response.status))
	}
	note, rejectionCode, err := decodeCreatedCandidateNote(response.body)
	if err != nil {
		return sdkgo.NewMutationRetry[CandidateNoteSummary](createCandidateNoteFailure(sdkgo.FailureProtocol, "Ashby accepted candidate note creation but returned an unusable response"), 0)
	}
	if rejectionCode != "" {
		if decision.MaximumPriorSends > 0 {
			return sdkgo.NewMutationUncertain(CandidateNoteSummary{}, createCandidateNoteFailure(sdkgo.FailureProviderRejection, "an earlier Ashby candidate note create may have succeeded"), sdkgo.Receipt{})
		}
		return createCandidateNoteBranch(CreateCandidateNoteBranchProviderRejected, sdkgo.FailureProviderRejection, "Ashby rejected candidate note creation")
	}
	return sdkgo.NewMutationBranch(CreateCandidateNoteBranchCreated, note, nil, sdkgo.Receipt{})
}

func decodeCreatedCandidateNote(body []byte) (CandidateNoteSummary, string, error) {
	var wire struct {
		Success   *bool                `json:"success"`
		Results   CandidateNoteSummary `json:"results"`
		ErrorInfo struct {
			Code string `json:"code"`
		} `json:"errorInfo"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.Success == nil {
		return CandidateNoteSummary{}, "", errors.New("invalid Ashby candidate note response")
	}
	if !*wire.Success {
		if wire.ErrorInfo.Code == "" {
			return CandidateNoteSummary{}, "", errors.New("missing Ashby error code")
		}
		return CandidateNoteSummary{}, wire.ErrorInfo.Code, nil
	}
	if !validCandidateID(wire.Results.ID) || wire.Results.CreatedAt == "" {
		return CandidateNoteSummary{}, "", errors.New("incomplete Ashby candidate note")
	}
	return wire.Results, "", nil
}

func createCandidateNoteFailure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "ashby", Operation: "createCandidateNote", Message: message}
}

func createCandidateNoteBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[CandidateNoteSummary] {
	failure := createCandidateNoteFailure(kind, message)
	return sdkgo.NewMutationBranch(branch, CandidateNoteSummary{}, &failure, sdkgo.Receipt{})
}
