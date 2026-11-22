// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestMutationAttemptAccessorsReadEachConstructor(t *testing.T) {
	rejection := failure(sdkgo.FailureProviderRejection, "channel is archived")
	receipt := sdkgo.Receipt{Provider: "example-chat", ProviderObjectID: "m-1"}
	testCases := []struct {
		name       string
		attempt    sdkgo.MutationAttempt[string]
		kind       sdkgo.AttemptKind
		branch     sdkgo.BranchID
		value      string
		receipt    sdkgo.Receipt
		failure    *sdkgo.Failure
		retryAfter time.Duration
	}{
		{name: "branch", attempt: sdkgo.NewMutationBranch("posted", "m-1", nil, receipt), kind: sdkgo.AttemptKindBranch, branch: "posted", value: "m-1", receipt: receipt},
		{
			name: "branch with failure", attempt: sdkgo.NewMutationBranch("providerRejected", "", &rejection, receipt),
			kind: sdkgo.AttemptKindBranch, branch: "providerRejected", receipt: receipt, failure: &rejection,
		},
		{
			name: "uncertain", attempt: sdkgo.NewMutationUncertain("m-1", rejection, receipt),
			kind: sdkgo.AttemptKindUncertain, value: "m-1", receipt: receipt, failure: &rejection,
		},
		{name: "retry", attempt: sdkgo.NewMutationRetry[string](rejection, 3*time.Second), kind: sdkgo.AttemptKindRetry, failure: &rejection, retryAfter: 3 * time.Second},
		{name: "retry with Step backoff", attempt: sdkgo.NewMutationRetry[string](rejection, 0), kind: sdkgo.AttemptKindRetry, failure: &rejection},
		{name: "zero attempt", attempt: sdkgo.MutationAttempt[string]{}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.kind, testCase.attempt.Kind())
			require.Equal(t, testCase.branch, testCase.attempt.Branch())
			require.Equal(t, testCase.value, testCase.attempt.Value())
			require.Equal(t, testCase.receipt, testCase.attempt.Receipt())
			require.Equal(t, testCase.failure, testCase.attempt.Failure())
			require.Equal(t, testCase.retryAfter, testCase.attempt.RetryAfter())
		})
	}
}

func TestQueryAttemptAccessorsReadEachConstructor(t *testing.T) {
	notFound := failure(sdkgo.FailureNotFound, "profile is missing")
	receipt := sdkgo.Receipt{Provider: "mock-provider", ProviderRequestID: "r-1"}

	branch := sdkgo.NewQueryBranch(testQuerySucceeded, "profile", nil, receipt)
	require.Equal(t, sdkgo.AttemptKindBranch, branch.Kind())
	require.Equal(t, testQuerySucceeded, branch.Branch())
	require.Equal(t, "profile", branch.Value())
	require.Equal(t, receipt, branch.Receipt())
	require.Nil(t, branch.Failure())
	require.Zero(t, branch.RetryAfter())

	failed := sdkgo.NewQueryBranch(testQueryFailed, "", &notFound, receipt)
	require.Equal(t, testQueryFailed, failed.Branch())
	require.Equal(t, &notFound, failed.Failure())

	retry := sdkgo.NewQueryRetry[string](notFound, 2*time.Second)
	require.Equal(t, sdkgo.AttemptKindRetry, retry.Kind())
	require.Empty(t, retry.Branch())
	require.Empty(t, retry.Value())
	require.Equal(t, sdkgo.Receipt{}, retry.Receipt())
	require.Equal(t, &notFound, retry.Failure())
	require.Equal(t, 2*time.Second, retry.RetryAfter())

	var zero sdkgo.QueryAttempt[string]
	require.Empty(t, zero.Kind())
	require.Nil(t, zero.Failure())
}

func TestAttemptFailureReturnsACopy(t *testing.T) {
	rejection := failure(sdkgo.FailureProviderRejection, "channel is archived")
	attempt := sdkgo.NewMutationBranch("providerRejected", "", &rejection, sdkgo.Receipt{})
	rejection.Message = "changed after construction"
	read := attempt.Failure()
	read.Message = "changed by a reader"
	require.Equal(t, "channel is archived", attempt.Failure().Message)
}

// innerPostMutation stands in for a connector Mutation: it records each Call and picks the attempt kind by
// input.
type innerPostMutation struct {
	calls []sdkgo.Call
}

var innerPostDefinition = sdkgo.MutationDefinition{
	Operation: sdkgo.OperationRef{ConnectorID: "example-chat", OperationID: "postThreadReply"},
	Branches: []sdkgo.BranchDefinition{
		{ID: "sent", Description: "The reply was posted."},
		{ID: "providerRejected", Description: "The provider refused the reply.", Optional: true},
		{ID: sdkgo.UncertainBranchID, Description: "The reply may have been posted.", Optional: true},
		{ID: sdkgo.DefectBranchID, Description: "Local input or configuration is invalid.", Optional: true},
	},
	RepeatOnUnknown: sdkgo.RepeatOnUnknownLimited,
	MaxSends:        3,
}

func (*innerPostMutation) Definition() sdkgo.MutationDefinition { return innerPostDefinition }

func (*innerPostMutation) IdempotencyKey(id sdkgo.CallID, _ string) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey("chat-" + string(id))
}

func (inner *innerPostMutation) Invoke(call sdkgo.Call, text string) sdkgo.MutationAttempt[string] {
	inner.calls = append(inner.calls, call)
	receipt := sdkgo.Receipt{Provider: "example-chat", ProviderObjectID: "ts-1"}
	switch text {
	case "refuse":
		rejection := failure(sdkgo.FailureProviderRejection, "channel is archived")
		return sdkgo.NewMutationBranch("providerRejected", "", &rejection, receipt)
	case "timeout":
		return sdkgo.NewMutationUncertain("ts-maybe", failure(sdkgo.FailureTransport, "response lost"), receipt)
	case "throttled":
		return sdkgo.NewMutationRetry[string](failure(sdkgo.FailureRateLimit, "slow down"), 4*time.Second)
	default:
		return sdkgo.NewMutationBranch("sent", "ts-1", nil, receipt)
	}
}

type reviewNote struct {
	Thread string `json:"thread"`
	Draft  int    `json:"draft"`
}

// reviewNoteMutation calls the wrapped Invoke under the Call it received and adds a thread and draft number.
type reviewNoteMutation struct {
	inner *innerPostMutation
}

func (wrapper reviewNoteMutation) Definition() sdkgo.MutationDefinition {
	return wrapper.inner.Definition()
}

func (wrapper reviewNoteMutation) IdempotencyKey(id sdkgo.CallID, text string) sdkgo.IdempotencyKey {
	return wrapper.inner.IdempotencyKey(id, text)
}

func (wrapper reviewNoteMutation) Invoke(call sdkgo.Call, text string) sdkgo.MutationAttempt[reviewNote] {
	attempt := wrapper.inner.Invoke(call, text)
	note := reviewNote{Thread: attempt.Value(), Draft: 1}
	switch attempt.Kind() {
	case sdkgo.AttemptKindBranch:
		return sdkgo.NewMutationBranch(attempt.Branch(), note, attempt.Failure(), attempt.Receipt())
	case sdkgo.AttemptKindUncertain:
		return sdkgo.NewMutationUncertain(note, *attempt.Failure(), attempt.Receipt())
	case sdkgo.AttemptKindRetry:
		return sdkgo.NewMutationRetry[reviewNote](*attempt.Failure(), attempt.RetryAfter())
	default:
		return sdkgo.MutationAttempt[reviewNote]{}
	}
}

func TestWrapperMutationMapsTheWrappedAttemptUnderTheSameCall(t *testing.T) {
	connection := sdkgo.ConnectionRef{Provider: "example-chat", Name: "work"}
	direct := &innerPostMutation{}
	directResult, err := sdkgo.RunMutation(testsupport.NewDexContext("review-flow", "post-review"), direct, connection, "Looks good.", sdkgo.WithMaxSends(5))
	require.NoError(t, err)
	require.Equal(t, sdkgo.BranchID("sent"), directResult.Branch)

	inner := &innerPostMutation{}
	wrapper := reviewNoteMutation{inner: inner}
	result, err := sdkgo.RunMutation(testsupport.NewDexContext("review-flow", "post-review"), wrapper, connection, "Looks good.", sdkgo.WithMaxSends(5))
	require.NoError(t, err)
	require.Len(t, inner.calls, 1)
	wrappedCall := inner.calls[0]
	require.Equal(t, direct.calls[0].ID, wrappedCall.ID, "the wrapped call keeps the Call ID of a direct call")
	require.Equal(t, direct.calls[0].IdempotencyKey, wrappedCall.IdempotencyKey)
	require.Equal(t, int32(5), wrappedCall.MaxSends())
	require.Equal(t, sdkgo.BranchID("sent"), result.Branch)
	require.Equal(t, reviewNote{Thread: "ts-1", Draft: 1}, result.Value)
	require.Nil(t, result.Failure)
	require.Equal(t, directResult.Receipt.CallID, result.Receipt.CallID)
	require.Equal(t, directResult.Receipt.IdempotencyKey, result.Receipt.IdempotencyKey)
	require.Equal(t, "ts-1", result.Receipt.ProviderObjectID)

	ctx := testsupport.NewDexContext("review-flow", "post-review-outcomes")
	refused, err := sdkgo.RunMutation(ctx, wrapper, connection, "refuse")
	require.NoError(t, err)
	require.Equal(t, sdkgo.BranchID("providerRejected"), refused.Branch)
	require.Equal(t, "channel is archived", refused.Failure.Message)

	uncertain, err := sdkgo.RunMutation(ctx, wrapper, connection, "timeout")
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, uncertain.Branch)
	require.Equal(t, reviewNote{Thread: "ts-maybe", Draft: 1}, uncertain.Value)
	require.Equal(t, sdkgo.FailureTransport, uncertain.Failure.Kind)

	_, err = sdkgo.RunMutation(ctx, wrapper, connection, "throttled")
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 4*time.Second, retryAfter.After)
}
