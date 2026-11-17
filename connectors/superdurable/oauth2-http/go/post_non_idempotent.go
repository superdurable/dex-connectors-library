// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package oauth2http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// postNonIdempotentWriteWindow gives the POST most of the Execute budget, because no read-back follows it.
const postNonIdempotentWriteWindow = 5.0 / 6

// PostNonIdempotentInput describes one HTTP POST that must never be sent twice.
type PostNonIdempotentInput struct {
	// Path is the request path under the connection's apiBaseUrl. It starts with a single slash, may be
	// percent-encoded, and carries no query, fragment, backslash, or .. segment. Set exactly one of Path and URL.
	Path string `json:"path,omitempty"`
	// URL is an absolute http or https URL with the scheme, host, and port of the connection's apiBaseUrl.
	// Set exactly one of Path and URL.
	URL string `json:"url,omitempty"`
	// Query holds query parameters that are added to those already in URL. A name may repeat through several values.
	Query map[string][]string `json:"query,omitempty"`
	// Headers holds extra request headers, such as Accept. Authorization, Cookie, Host, and the headers the
	// transport owns select defect; the connector always sends Authorization: Bearer with the access token.
	Headers map[string]string `json:"headers,omitempty"`
	// Body is the request body, sent as text and limited to the connection's maxBodyBytes. Content-Type
	// defaults to application/json when Headers sets none. Blank sends an empty body.
	Body string `json:"body,omitempty"`
}

// PostNonIdempotentOperation implements the postNonIdempotent Mutation. Build it with
// Client.PostNonIdempotent.
type PostNonIdempotentOperation struct{ client *Client }

// Definition returns the immutable postNonIdempotent operation definition.
func (PostNonIdempotentOperation) Definition() sdkgo.MutationDefinition {
	return PostNonIdempotentDefinition
}

// IdempotencyKey is the stable Call ID. The POST does not carry it; the single-dispatch fence uses the
// Call ID to send at most once per Step execution.
func (PostNonIdempotentOperation) IdempotencyKey(callID sdkgo.CallID, _ PostNonIdempotentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends the POST at most once per Step execution under the single-dispatch fence. A 2xx selects
// completed. A definite refusal (400, 401, 403, 404, 411, 413, 414, 415, or 422) of the only send, or an
// authorization the server will no longer renew, selects rejected. A 429, a 503, a 401 after which the
// token was renewed, and a POST that provably never left this process return Retry, because the API did
// not process it. Any other outcome after a possible send, and any attempt after an earlier possible
// send, selects uncertain and never sends again. An unreadable checkpoint returns Retry.
func (operation PostNonIdempotentOperation) Invoke(call sdkgo.Call, input PostNonIdempotentInput) sdkgo.MutationAttempt[SendRequestOutput] {
	client := operation.client
	operationName := PostNonIdempotentDefinition.Operation.OperationID
	request, err := client.planRequest(operationName, SendRequestInput{
		Method: http.MethodPost, Path: input.Path, URL: input.URL, Query: input.Query, Headers: input.Headers, Body: input.Body,
	})
	if err != nil {
		return operationBranch(operationName, PostNonIdempotentBranchDefect, sdkgo.FailureValidation, err.Error(), SendRequestOutput{}, sdkgo.Receipt{})
	}
	request.isFencedWrite = true
	fence, err := singledispatch.NewFence(call, PostNonIdempotentDefinition.StepDefaults.ExecuteMethodTimeout,
		singledispatch.WithWriteWindow(postNonIdempotentWriteWindow), singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return operationBranch(operationName, PostNonIdempotentBranchDefect, sdkgo.FailureLocalDefect,
			"the single-dispatch fence could not be constructed", SendRequestOutput{}, sdkgo.Receipt{})
	}
	decision := fence.Decide()
	switch decision.Kind {
	case singledispatch.FailClosed:
		return sdkgo.NewMutationRetry[SendRequestOutput](operationFailure(operationName, sdkgo.FailureAvailability,
			"the single-dispatch checkpoint could not be read; nothing was sent in this attempt"), 0)
	case singledispatch.ReadBackRequired:
		return sdkgo.NewMutationUncertain(SendRequestOutput{}, operationFailure(operationName, sdkgo.FailureTransport,
			"an earlier attempt may have sent the POST, so it is not sent again"), client.receipt(call, httpExchange{}))
	}
	outcome := postOutcome{
		operation: operation, call: call, fence: fence,
		// An earlier send may have been processed, so a refusal of this one cannot prove nothing changed.
		hasPossibleEarlierSend: decision.MaximumPriorSends > 0,
	}
	ctx, cancel := context.WithTimeout(call.Context, operationTimeout)
	defer cancel()
	credentials, err := sdkgo.ResolveCredential(ctx, client.credentials, call, client.refreshDriver)
	if problem := classifyCredentialResolution(ctx, err, credentials); problem.isFailed() {
		if checkpointErr := fence.RecordNotSent(); checkpointErr != nil && !problem.isTerminal() {
			return sdkgo.NewMutationRetry[SendRequestOutput](operationFailure(operationName, sdkgo.FailureAvailability,
				"the OAuth access token and the no-send checkpoint are unavailable now"), 0)
		}
		if problem.isTerminal() && outcome.hasPossibleEarlierSend {
			return outcome.uncertain(SendRequestOutput{}, sdkgo.FailureAuthentication,
				"an earlier attempt may have sent the POST, and the connection's credentials cannot send it now", httpExchange{})
		}
		return problem.attempt(operationName, PostNonIdempotentBranchRejected, PostNonIdempotentBranchDefect)
	}
	var answer httpExchange
	err = fence.Dispatch(func(requestContext context.Context) error {
		answer = client.exchange(requestContext, call, request, credentials)
		return nil
	})
	switch {
	case errors.Is(err, singledispatch.ErrCheckpoint):
		return sdkgo.NewMutationRetry[SendRequestOutput](operationFailure(operationName, sdkgo.FailureAvailability,
			"the single-dispatch claim could not be recorded; nothing was sent"), 0)
	case errors.Is(err, singledispatch.ErrInsufficientTime):
		return sdkgo.NewMutationRetry[SendRequestOutput](operationFailure(operationName, sdkgo.FailureAvailability,
			"too little of the Step's time remained to send the POST; nothing was sent"), 0)
	case err != nil:
		return sdkgo.NewMutationRetry[SendRequestOutput](operationFailure(operationName, sdkgo.FailureLocalDefect,
			"the single-dispatch fence refused the POST; nothing was sent"), 0)
	}
	return outcome.attemptFor(ctx, answer, credentials)
}

// postOutcome classifies the answer to one fenced POST.
type postOutcome struct {
	operation              PostNonIdempotentOperation
	call                   sdkgo.Call
	fence                  *singledispatch.Fence
	hasPossibleEarlierSend bool
}

// attemptFor chooses the branch for the answer to this attempt's only send.
func (outcome postOutcome) attemptFor(ctx context.Context, answer httpExchange, credentials Credentials) sdkgo.MutationAttempt[SendRequestOutput] {
	client := outcome.operation.client
	output := responseOutput(answer, []string{credentials.AccessToken.Reveal()})
	status := answer.statusCode
	switch {
	case answer.refusal != nil:
		return outcome.refuse(PostNonIdempotentBranchDefect, SendRequestOutput{}, sdkgo.FailureValidation,
			describeRefusal("apiBaseUrl", answer.refusal), answer)
	case answer.err != nil && !answer.isDispatched:
		return outcome.retryUnprocessed(sdkgo.FailureAvailability, "the API could not be reached; the POST was not sent", 0)
	case answer.err != nil:
		return outcome.uncertain(output, sdkgo.FailureTransport, "the POST was sent but the API did not answer completely", answer)
	case status >= 200 && status < 300:
		return sdkgo.NewMutationBranch(PostNonIdempotentBranchCompleted, output, nil, client.receipt(outcome.call, answer))
	case status == http.StatusTooManyRequests:
		return outcome.retryUnprocessed(sdkgo.FailureRateLimit, "the API answered HTTP 429 without processing the POST", client.retryAfter(answer))
	case status == http.StatusServiceUnavailable:
		return outcome.retryUnprocessed(sdkgo.FailureAvailability, "the API answered HTTP 503 without processing the POST", client.retryAfter(answer))
	case status == http.StatusUnauthorized:
		return outcome.renewAfterRejection(ctx, output, credentials, answer)
	case isDefiniteRefusal(status):
		return outcome.refuse(PostNonIdempotentBranchRejected, output, rejectionFailureKind(status),
			fmt.Sprintf("the API answered HTTP %d", status), answer)
	case status >= 500:
		return outcome.uncertain(output, sdkgo.FailureAvailability,
			fmt.Sprintf("the API answered HTTP %d after the POST was sent", status), answer)
	default:
		return outcome.uncertain(output, rejectionFailureKind(status),
			fmt.Sprintf("the API answered HTTP %d, which does not prove the POST was refused", status), answer)
	}
}

// renewAfterRejection renews the token after a 401 and returns Retry, so the next attempt sends the POST
// with the new token; an authorization that cannot be renewed selects rejected.
func (outcome postOutcome) renewAfterRejection(
	ctx context.Context, output SendRequestOutput, credentials Credentials, answer httpExchange,
) sdkgo.MutationAttempt[SendRequestOutput] {
	client := outcome.operation.client
	replacement, err := sdkgo.ResolveCredentialAfterRejection(ctx, client.credentials, outcome.call, client.refreshDriver)
	problem := classifyCredentialResolution(ctx, err, replacement)
	isReplacementUsable := !problem.isFailed() && replacement.AccessToken.Reveal() != credentials.AccessToken.Reveal()
	if isReplacementUsable || problem.kind == credentialTemporarilyUnavailable {
		return outcome.retryUnprocessed(sdkgo.FailureAuthentication,
			"the API answered HTTP 401 without processing the POST; it is sent again with a renewed token", 0)
	}
	return outcome.refuse(PostNonIdempotentBranchRejected, output, sdkgo.FailureAuthentication,
		"the API answered HTTP 401 and the OAuth authorization could not be renewed", answer)
}

// retryUnprocessed records that the API did not process this send, then returns Retry.
func (outcome postOutcome) retryUnprocessed(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[SendRequestOutput] {
	operationName := PostNonIdempotentDefinition.Operation.OperationID
	if err := outcome.fence.RecordCanResend(); err != nil {
		// The claim stays the last checkpoint, so the next attempt selects uncertain instead of sending.
		return sdkgo.NewMutationRetry[SendRequestOutput](operationFailure(operationName, sdkgo.FailureAvailability,
			"the retry checkpoint could not be recorded"), 0)
	}
	return sdkgo.NewMutationRetry[SendRequestOutput](operationFailure(operationName, kind, message), retryAfter)
}

// refuse selects branch for a definite refusal of the only send, or uncertain after an earlier possible send.
func (outcome postOutcome) refuse(
	branch sdkgo.BranchID, output SendRequestOutput, kind sdkgo.FailureKind, message string, answer httpExchange,
) sdkgo.MutationAttempt[SendRequestOutput] {
	if outcome.hasPossibleEarlierSend {
		return outcome.uncertain(output, kind, message+"; an earlier attempt may have sent the POST", answer)
	}
	// Best effort: the branch is terminal, and a lost canResend can only make a later attempt uncertain.
	_ = outcome.fence.RecordCanResend()
	failure := operationFailure(PostNonIdempotentDefinition.Operation.OperationID, kind, message)
	return sdkgo.NewMutationBranch(branch, output, &failure, outcome.operation.client.receipt(outcome.call, answer))
}

// uncertain selects the uncertain branch; the claim stays the last checkpoint, so nothing is sent again.
func (outcome postOutcome) uncertain(output SendRequestOutput, kind sdkgo.FailureKind, message string, answer httpExchange) sdkgo.MutationAttempt[SendRequestOutput] {
	return sdkgo.NewMutationUncertain(output, operationFailure(PostNonIdempotentDefinition.Operation.OperationID, kind, message),
		outcome.operation.client.receipt(outcome.call, answer))
}
