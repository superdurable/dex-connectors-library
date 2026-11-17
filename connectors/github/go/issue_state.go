// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package github

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const setIssueStateOperationID = "setIssueState"

// Issue states and state reasons accepted by SetIssueStateInput.
const (
	// IssueStateOpen is an open issue.
	IssueStateOpen = "open"
	// IssueStateClosed is a closed issue.
	IssueStateClosed = "closed"
	// IssueStateReasonCompleted closes an issue as completed. GitHub applies it when a close omits the reason.
	IssueStateReasonCompleted = "completed"
	// IssueStateReasonNotPlanned closes an issue as not planned.
	IssueStateReasonNotPlanned = "not_planned"
	// IssueStateReasonReopened reopens a closed issue. GitHub applies it when a reopen omits the reason.
	IssueStateReasonReopened = "reopened"
)

// SetIssueStateInput names one existing issue and the state to set.
type SetIssueStateInput struct {
	// Owner is the GitHub login that owns the repository. Surrounding whitespace is ignored.
	Owner string `json:"owner"`
	// Repository is the repository name without its owner, such as hello-world.
	Repository string `json:"repository"`
	// IssueNumber is the positive issue number within the repository.
	IssueNumber int `json:"issueNumber"`
	// State is open or closed.
	State string `json:"state"`
	// StateReason is completed or not_planned with State closed, or reopened with State open. Empty lets
	// GitHub choose: completed for a close and reopened for a reopen. Setting the same state with another
	// reason changes only the reason.
	StateReason string `json:"stateReason,omitempty"`
}

// UpdatedIssueState is the issue's state after the update. On every other branch only Number is set,
// echoing the requested issue number.
type UpdatedIssueState struct {
	// Number is the issue number within its repository.
	Number int `json:"number"`
	// URL is the issue's GitHub web URL, or empty when GitHub returns an unsafe URL.
	URL string `json:"url,omitempty"`
	// State is open or closed as GitHub returned it.
	State string `json:"state,omitempty"`
	// StateReason is GitHub's reason for the state, such as completed, not_planned, or reopened, when set.
	StateReason string `json:"stateReason,omitempty"`
	// UpdatedAt is the issue's last update time in UTC.
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// SetIssueStateOperation implements the setIssueState Mutation.
type SetIssueStateOperation struct{ client *Client }

type setIssueStateRequestBody struct {
	State       string `json:"state"`
	StateReason string `json:"state_reason,omitempty"`
}

// Definition returns the manifest-generated execution and branch contract.
func (SetIssueStateOperation) Definition() sdkgo.MutationDefinition { return SetIssueStateDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable Call ID. GitHub accepts no
// idempotency key; an absolute state update is safe to send again instead.
func (SetIssueStateOperation) IdempotencyKey(callID sdkgo.CallID, _ SetIssueStateInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one PATCH that sets the issue's state. Invalid input and a connection without the
// github-oauth-repository method select a branch before GitHub is called. Rate limits, 5xx answers, and
// transport failures return Retry, because sending the same absolute state again is harmless.
func (operation SetIssueStateOperation) Invoke(call sdkgo.Call, input SetIssueStateInput) sdkgo.MutationAttempt[UpdatedIssueState] {
	requested := UpdatedIssueState{Number: max(input.IssueNumber, 0)}
	owner, repository, request, failure := buildSetIssueStateRequest(input)
	if failure != nil {
		return sdkgo.NewMutationBranch(SetIssueStateBranchDefect, requested, failure, sdkgo.Receipt{})
	}
	credential, failure := operation.client.resolveCredential(call, setIssueStateOperationID)
	if failure != nil {
		return sdkgo.NewMutationBranch(SetIssueStateBranchDefect, requested, failure, sdkgo.Receipt{})
	}
	if credential.AuthMethodID != RepositoryAuthMethodID {
		return issueStateBranch(SetIssueStateBranchInsufficientScope, requested, sdkgo.FailureAuthorization,
			"setIssueState requires a connection authorized with the github-oauth-repository method", sdkgo.Receipt{})
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return issueStateBranch(SetIssueStateBranchDefect, requested, sdkgo.FailureLocalDefect, "GitHub issue state request could not be encoded", sdkgo.Receipt{})
	}
	path := issuesPath(owner, repository) + "/" + strconv.Itoa(input.IssueNumber)
	response, err := operation.client.sendRequest(call, &credential, http.MethodPatch, path, nil, payload)
	if err != nil {
		if errors.Is(err, errResponseTooLarge) {
			return issueStateBranch(SetIssueStateBranchInvalidResponse, requested, sdkgo.FailureResponseTooLarge, "GitHub response exceeds the configured size limit", receipt(response))
		}
		return sdkgo.NewMutationRetry[UpdatedIssueState](providerFailure(setIssueStateOperationID, sdkgo.FailureTransport, "GitHub issue state update did not complete"), 0)
	}
	if terminal := operation.client.classify(setIssueStateOperationID, response, issueStateResponseBranches()); terminal != nil {
		if terminal.isRetryable {
			return sdkgo.NewMutationRetry[UpdatedIssueState](terminal.failure, terminal.delay)
		}
		if terminal.branch == SetIssueStateBranchProviderRejected {
			return issueStateBranch(terminal.branch, requested, statusFailureKind(response.statusCode),
				"GitHub rejected the issue state update with HTTP "+strconv.Itoa(response.statusCode), receipt(response))
		}
		return sdkgo.NewMutationBranch(terminal.branch, requested, &terminal.failure, receipt(response))
	}
	var updated githubIssue
	if decodeJSON(response.body, &updated) != nil || !isValidUpdatedIssue(updated, input.IssueNumber, request.State) {
		return issueStateBranch(SetIssueStateBranchInvalidResponse, requested, sdkgo.FailureProtocol, "GitHub returned an invalid issue", receipt(response))
	}
	updatedAt := updated.UpdatedAt.UTC()
	output := UpdatedIssueState{
		Number: updated.Number, URL: safeURL(updated.HTMLURL), State: updated.State,
		StateReason: boundedString(updated.StateReason, maximumIssueStateBytes), UpdatedAt: &updatedAt,
	}
	updateReceipt := receipt(response)
	updateReceipt.ProviderObjectID = updated.NodeID
	return sdkgo.NewMutationBranch(SetIssueStateBranchUpdated, output, nil, updateReceipt)
}

// buildSetIssueStateRequest validates input against GitHub's state and state reason combinations.
func buildSetIssueStateRequest(input SetIssueStateInput) (string, string, setIssueStateRequestBody, *sdkgo.Failure) {
	invalid := func(message string) (string, string, setIssueStateRequestBody, *sdkgo.Failure) {
		failure := providerFailure(setIssueStateOperationID, sdkgo.FailureValidation, message)
		return "", "", setIssueStateRequestBody{}, &failure
	}
	owner, repository, failure := validateRepository(setIssueStateOperationID, input.Owner, input.Repository)
	if failure != nil {
		return "", "", setIssueStateRequestBody{}, failure
	}
	if input.IssueNumber <= 0 {
		return invalid("GitHub issue number must be positive")
	}
	state, stateReason := strings.TrimSpace(input.State), strings.TrimSpace(input.StateReason)
	switch state {
	case IssueStateOpen:
		if stateReason != "" && stateReason != IssueStateReasonReopened {
			return invalid("GitHub issue state open accepts only the state reason reopened")
		}
	case IssueStateClosed:
		if stateReason != "" && stateReason != IssueStateReasonCompleted && stateReason != IssueStateReasonNotPlanned {
			return invalid("GitHub issue state closed accepts only the state reasons completed and not_planned")
		}
	default:
		return invalid("GitHub issue state must be open or closed")
	}
	return owner, repository, setIssueStateRequestBody{State: state, StateReason: stateReason}, nil
}

// isValidUpdatedIssue requires the requested issue in the requested state, which rules out a redirect that
// read a transferred issue instead of updating it.
func isValidUpdatedIssue(issue githubIssue, issueNumber int, state string) bool {
	return issue.Number == issueNumber && issue.State == state && issue.UpdatedAt != nil && !issue.UpdatedAt.IsZero() &&
		len(issue.NodeID) <= maximumNodeIDBytes
}

func issueStateBranch(branch sdkgo.BranchID, requested UpdatedIssueState, kind sdkgo.FailureKind, message string, branchReceipt sdkgo.Receipt) sdkgo.MutationAttempt[UpdatedIssueState] {
	failure := providerFailure(setIssueStateOperationID, kind, message)
	return sdkgo.NewMutationBranch(branch, requested, &failure, branchReceipt)
}

func issueStateResponseBranches() responseBranches {
	return responseBranches{
		insufficientScope: SetIssueStateBranchInsufficientScope,
		revoked:           SetIssueStateBranchAuthorizationRevoked,
		notFound:          SetIssueStateBranchNotFound,
		providerRejected:  SetIssueStateBranchProviderRejected,
	}
}
