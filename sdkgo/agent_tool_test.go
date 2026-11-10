// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
	"github.com/superdurable/dex/sdk-go/dex"
)

var (
	agentConnection          = sdkgo.ConnectionRef{Provider: "agent-provider", Name: "work"}
	agentQueryRef            = sdkgo.OperationRef{ConnectorID: "agent-provider", OperationID: "listStoryIDs"}
	agentMutationRef         = sdkgo.OperationRef{ConnectorID: "agent-provider", OperationID: "postReply"}
	agentBranchSucceeded     = sdkgo.BranchID("succeeded")
	agentBranchNotFound      = sdkgo.BranchID("notFound")
	agentBranchRateLimited   = sdkgo.BranchID("rateLimited")
	agentBranchRejected      = sdkgo.BranchID("providerRejected")
	agentBranchInvalidReply  = sdkgo.BranchID("invalidResponse")
	agentBranchConflict      = sdkgo.BranchID("conflict")
	agentBranchUndeclared    = sdkgo.BranchID("undeclared")
	agentValidFailure        = sdkgo.Failure{Kind: sdkgo.FailureProviderRejection, Provider: "agent-provider", Operation: "postReply", Message: "provider refused"}
	agentInputSchema         = json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"}}}`)
	agentRetryPolicyInterval = 2 * time.Second
)

type agentInput struct {
	Text  string `json:"text"`
	Limit int    `json:"limit,omitempty"`
}

type agentOutput struct {
	Echo string `json:"echo"`
}

func agentQueryDefinition() sdkgo.QueryDefinition {
	return sdkgo.QueryDefinition{
		Operation: agentQueryRef,
		Branches: []sdkgo.BranchDefinition{
			{ID: agentBranchSucceeded, Description: "succeeded"},
			{ID: agentBranchNotFound, Description: "not found", Optional: true},
			{ID: agentBranchRateLimited, Description: "rate limited", Optional: true},
			{ID: sdkgo.DefectBranchID, Description: "defect", Optional: true},
		},
		StepDefaults: sdkgo.StepDefaults{ExecuteMethodTimeout: 30 * time.Second},
	}
}

func agentMutationDefinition() sdkgo.MutationDefinition {
	return sdkgo.MutationDefinition{
		Operation: agentMutationRef,
		Branches: []sdkgo.BranchDefinition{
			{ID: agentBranchSucceeded, Description: "succeeded"},
			{ID: agentBranchRejected, Description: "provider rejected", Optional: true},
			{ID: agentBranchInvalidReply, Description: "invalid response", Optional: true},
			{ID: agentBranchConflict, Description: "conflict", Optional: true},
			{ID: sdkgo.UncertainBranchID, Description: "uncertain", Optional: true},
			{ID: sdkgo.DefectBranchID, Description: "defect", Optional: true},
		},
		StepDefaults: sdkgo.StepDefaults{
			ExecuteMethodTimeout: 30 * time.Second,
			ExecuteRetry:         &dex.RetryPolicy{InitialInterval: agentRetryPolicyInterval, BackoffCoefficient: 2},
		},
	}
}

func agentQueryMetadata() sdkgo.AgentToolMetadata {
	definition := agentQueryDefinition()
	return sdkgo.AgentToolMetadata{
		Operation: definition.Operation, Kind: sdkgo.AgentOperationKindQuery,
		ToolName: "agent-provider_list_story_ids", Description: "List story IDs.",
		BranchOutcomes: map[sdkgo.BranchID]sdkgo.AgentBranchOutcome{agentBranchNotFound: sdkgo.AgentBranchOutcomeRejected},
		InputType:      "ListStoryIDsInput", InputSchema: agentInputSchema,
		Branches: definition.Branches, StepDefaults: definition.StepDefaults,
	}
}

func agentMutationMetadata() sdkgo.AgentToolMetadata {
	definition := agentMutationDefinition()
	return sdkgo.AgentToolMetadata{
		Operation: definition.Operation, Kind: sdkgo.AgentOperationKindMutation,
		ToolName: "agent-provider_post_reply", Description: "Post a reply.", IsApprovalRecommended: true,
		BranchOutcomes: map[sdkgo.BranchID]sdkgo.AgentBranchOutcome{
			agentBranchRejected:     sdkgo.AgentBranchOutcomeRejected,
			agentBranchInvalidReply: sdkgo.AgentBranchOutcomeUnknown,
		},
		InputType: "PostReplyInput", InputSchema: agentInputSchema,
		Branches: definition.Branches, StepDefaults: definition.StepDefaults,
	}
}

type agentQuery struct {
	invoke      func(sdkgo.Call, agentInput) sdkgo.QueryAttempt[agentOutput]
	invocations int
	inputs      []agentInput
}

func (*agentQuery) Definition() sdkgo.QueryDefinition { return agentQueryDefinition() }

func (query *agentQuery) Invoke(call sdkgo.Call, input agentInput) sdkgo.QueryAttempt[agentOutput] {
	query.invocations++
	query.inputs = append(query.inputs, input)
	if query.invoke == nil {
		return sdkgo.NewQueryBranch(agentBranchSucceeded, agentOutput{Echo: input.Text}, nil, sdkgo.Receipt{})
	}
	return query.invoke(call, input)
}

type agentMutation struct {
	invoke         func(sdkgo.Call, agentInput) sdkgo.MutationAttempt[agentOutput]
	idempotencyKey func(sdkgo.CallID) sdkgo.IdempotencyKey
	invocations    int
	calls          []sdkgo.Call
}

func (*agentMutation) Definition() sdkgo.MutationDefinition { return agentMutationDefinition() }

func (mutation *agentMutation) IdempotencyKey(id sdkgo.CallID, _ agentInput) sdkgo.IdempotencyKey {
	if mutation.idempotencyKey == nil {
		return sdkgo.IdempotencyKey("key-" + string(id))
	}
	return mutation.idempotencyKey(id)
}

func (mutation *agentMutation) Invoke(call sdkgo.Call, input agentInput) sdkgo.MutationAttempt[agentOutput] {
	mutation.invocations++
	mutation.calls = append(mutation.calls, call)
	if mutation.invoke == nil {
		return sdkgo.NewMutationBranch(agentBranchSucceeded, agentOutput{Echo: input.Text}, nil, sdkgo.Receipt{})
	}
	return mutation.invoke(call, input)
}

func agentStepContext() *singledispatchtest.Context {
	return singledispatchtest.NewContext("flow-agent-tool", "step-execution-1")
}

func agentRPCContext() *singledispatchtest.Context {
	return singledispatchtest.NewContext("flow-agent-tool", "")
}

func mutationBranchAttempt(branch sdkgo.BranchID) func(sdkgo.Call, agentInput) sdkgo.MutationAttempt[agentOutput] {
	return func(sdkgo.Call, agentInput) sdkgo.MutationAttempt[agentOutput] {
		failure := agentValidFailure
		return sdkgo.NewMutationBranch(branch, agentOutput{}, &failure, sdkgo.Receipt{})
	}
}

func queryBranchAttempt(branch sdkgo.BranchID) func(sdkgo.Call, agentInput) sdkgo.QueryAttempt[agentOutput] {
	return func(sdkgo.Call, agentInput) sdkgo.QueryAttempt[agentOutput] {
		failure := agentValidFailure
		return sdkgo.NewQueryBranch(branch, agentOutput{}, &failure, sdkgo.Receipt{})
	}
}

func TestAgentToolEffectFollowsKindAndRetrySafety(t *testing.T) {
	query := agentQueryMetadata()
	require.Equal(t, sdkgo.AgentToolEffectRead, query.Effect())

	mutation := agentMutationMetadata()
	require.Equal(t, sdkgo.AgentToolEffectNonIdempotentWrite, mutation.Effect())
	mutation.IsRetrySafe = true
	require.Equal(t, sdkgo.AgentToolEffectIdempotentWrite, mutation.Effect())

	mutation.Kind = "subscription"
	require.Equal(t, sdkgo.AgentToolEffectNonIdempotentWrite, mutation.Effect(), "an unknown kind fails closed")
	require.Equal(t, "read", string(sdkgo.AgentToolEffectRead))
	require.Equal(t, "idempotent_write", string(sdkgo.AgentToolEffectIdempotentWrite))
	require.Equal(t, "non_idempotent_write", string(sdkgo.AgentToolEffectNonIdempotentWrite))
}

func TestMutationAgentToolClassifiesEveryBranch(t *testing.T) {
	invalidUncertainty := func(sdkgo.Call, agentInput) sdkgo.MutationAttempt[agentOutput] {
		return sdkgo.NewMutationUncertain(agentOutput{}, sdkgo.Failure{}, sdkgo.Receipt{})
	}
	validUncertainty := func(sdkgo.Call, agentInput) sdkgo.MutationAttempt[agentOutput] {
		return sdkgo.NewMutationUncertain(agentOutput{}, agentValidFailure, sdkgo.Receipt{})
	}
	invalidRetry := func(sdkgo.Call, agentInput) sdkgo.MutationAttempt[agentOutput] {
		return sdkgo.NewMutationRetry[agentOutput](sdkgo.Failure{}, 0)
	}
	foreignReceipt := func(sdkgo.Call, agentInput) sdkgo.MutationAttempt[agentOutput] {
		return sdkgo.NewMutationBranch(agentBranchSucceeded, agentOutput{}, nil, sdkgo.Receipt{IdempotencyKey: "another-key"})
	}
	defectListedRejected := func(metadata *sdkgo.AgentToolMetadata) {
		metadata.BranchOutcomes[sdkgo.DefectBranchID] = sdkgo.AgentBranchOutcomeRejected
	}
	cases := []struct {
		name                   string
		invoke                 func(sdkgo.Call, agentInput) sdkgo.MutationAttempt[agentOutput]
		idempotencyKey         func(sdkgo.CallID) sdkgo.IdempotencyKey
		isRPCContext           bool
		changeMetadata         func(*sdkgo.AgentToolMetadata)
		wantOutcome            sdkgo.AgentToolAttemptOutcome
		wantBranch             sdkgo.BranchID
		wantHasInvoked         bool
		wantFailureKind        sdkgo.FailureKind
		wantFailureMessagePart string
	}{
		{name: "required branch succeeds", wantOutcome: sdkgo.AgentToolAttemptSucceeded, wantBranch: agentBranchSucceeded, wantHasInvoked: true},
		{name: "listed rejected branch", invoke: mutationBranchAttempt(agentBranchRejected), wantOutcome: sdkgo.AgentToolAttemptRejected, wantBranch: agentBranchRejected, wantHasInvoked: true, wantFailureKind: sdkgo.FailureProviderRejection},
		{name: "listed unknown branch", invoke: mutationBranchAttempt(agentBranchInvalidReply), wantOutcome: sdkgo.AgentToolAttemptUnknown, wantBranch: agentBranchInvalidReply, wantHasInvoked: true},
		{name: "unlisted optional branch is unknown", invoke: mutationBranchAttempt(agentBranchConflict), wantOutcome: sdkgo.AgentToolAttemptUnknown, wantBranch: agentBranchConflict, wantHasInvoked: true},
		{name: "uncertain is unknown without a listing", invoke: validUncertainty, wantOutcome: sdkgo.AgentToolAttemptUnknown, wantBranch: sdkgo.UncertainBranchID, wantHasInvoked: true},
		{name: "uncertain listed unknown", invoke: validUncertainty, changeMetadata: func(metadata *sdkgo.AgentToolMetadata) {
			metadata.BranchOutcomes[sdkgo.UncertainBranchID] = sdkgo.AgentBranchOutcomeUnknown
		}, wantOutcome: sdkgo.AgentToolAttemptUnknown, wantBranch: sdkgo.UncertainBranchID, wantHasInvoked: true},
		{name: "connector defect is unknown despite a rejected listing", invoke: mutationBranchAttempt(sdkgo.DefectBranchID), changeMetadata: defectListedRejected, wantOutcome: sdkgo.AgentToolAttemptUnknown, wantBranch: sdkgo.DefectBranchID, wantHasInvoked: true},
		{name: "connector defect unlisted is unknown", invoke: mutationBranchAttempt(sdkgo.DefectBranchID), wantOutcome: sdkgo.AgentToolAttemptUnknown, wantBranch: sdkgo.DefectBranchID, wantHasInvoked: true},
		{name: "post-Invoke invalid uncertainty defect is unknown despite a rejected listing", invoke: invalidUncertainty, changeMetadata: defectListedRejected, wantOutcome: sdkgo.AgentToolAttemptUnknown, wantBranch: sdkgo.DefectBranchID, wantHasInvoked: true, wantFailureKind: sdkgo.FailureLocalDefect, wantFailureMessagePart: "invalid uncertainty failure"},
		{name: "post-Invoke undeclared branch defect is unknown despite a rejected listing", invoke: mutationBranchAttempt(agentBranchUndeclared), changeMetadata: defectListedRejected, wantOutcome: sdkgo.AgentToolAttemptUnknown, wantBranch: sdkgo.DefectBranchID, wantHasInvoked: true, wantFailureMessagePart: "unknown branch"},
		{name: "post-Invoke invalid retry defect is unknown despite a rejected listing", invoke: invalidRetry, changeMetadata: defectListedRejected, wantOutcome: sdkgo.AgentToolAttemptUnknown, wantBranch: sdkgo.DefectBranchID, wantHasInvoked: true, wantFailureMessagePart: "invalid retry attempt"},
		{name: "post-Invoke foreign receipt defect is unknown despite a rejected listing", invoke: foreignReceipt, changeMetadata: defectListedRejected, wantOutcome: sdkgo.AgentToolAttemptUnknown, wantBranch: sdkgo.DefectBranchID, wantHasInvoked: true, wantFailureMessagePart: "another idempotency key"},
		{name: "pre-Invoke RPC context defect is unknown despite a rejected listing", isRPCContext: true, changeMetadata: defectListedRejected, wantOutcome: sdkgo.AgentToolAttemptUnknown, wantBranch: sdkgo.DefectBranchID, wantHasInvoked: false, wantFailureMessagePart: "RPC invocation is not allowed"},
		{name: "pre-Invoke RPC context defect unlisted is unknown", isRPCContext: true, wantOutcome: sdkgo.AgentToolAttemptUnknown, wantBranch: sdkgo.DefectBranchID, wantHasInvoked: false},
		{name: "pre-Invoke blank idempotency key defect is unknown despite a rejected listing", idempotencyKey: func(sdkgo.CallID) sdkgo.IdempotencyKey { return "  " }, changeMetadata: defectListedRejected, wantOutcome: sdkgo.AgentToolAttemptUnknown, wantBranch: sdkgo.DefectBranchID, wantHasInvoked: false, wantFailureMessagePart: "invalid idempotency key"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			operation := &agentMutation{invoke: testCase.invoke, idempotencyKey: testCase.idempotencyKey}
			metadata := agentMutationMetadata()
			if testCase.changeMetadata != nil {
				testCase.changeMetadata(&metadata)
			}
			tool, err := sdkgo.NewMutationAgentTool(operation, agentConnection, metadata)
			require.NoError(t, err)
			ctx := agentStepContext()
			if testCase.isRPCContext {
				ctx = agentRPCContext()
			}

			attempt, err := tool.RunAttempt(ctx, json.RawMessage(`{"text":"hello"}`))
			require.NoError(t, err)
			require.Equal(t, testCase.wantOutcome, attempt.Outcome)
			require.Equal(t, testCase.wantBranch, attempt.Branch)
			require.Equal(t, testCase.wantHasInvoked, attempt.HasInvokedOperation)
			require.Equal(t, testCase.wantHasInvoked, operation.invocations == 1)
			require.Zero(t, attempt.RetryAfter)
			require.NotNil(t, attempt.Value)
			if testCase.wantOutcome == sdkgo.AgentToolAttemptSucceeded {
				require.Nil(t, attempt.Failure)
				require.JSONEq(t, `{"echo":"hello"}`, string(attempt.Value))
				return
			}
			require.NotNil(t, attempt.Failure)
			if testCase.wantFailureKind != "" {
				require.Equal(t, testCase.wantFailureKind, attempt.Failure.Kind)
			}
			require.Contains(t, attempt.Failure.Message, testCase.wantFailureMessagePart)
		})
	}
}

func TestQueryAgentToolClassifiesEveryBranch(t *testing.T) {
	undeclared := queryBranchAttempt(agentBranchUndeclared)
	cases := []struct {
		name           string
		invoke         func(sdkgo.Call, agentInput) sdkgo.QueryAttempt[agentOutput]
		isRPCContext   bool
		wantOutcome    sdkgo.AgentToolAttemptOutcome
		wantBranch     sdkgo.BranchID
		wantHasInvoked bool
	}{
		{name: "required branch succeeds", wantOutcome: sdkgo.AgentToolAttemptSucceeded, wantBranch: agentBranchSucceeded, wantHasInvoked: true},
		{name: "listed rejected branch", invoke: queryBranchAttempt(agentBranchNotFound), wantOutcome: sdkgo.AgentToolAttemptRejected, wantBranch: agentBranchNotFound, wantHasInvoked: true},
		{name: "unlisted optional branch is failed", invoke: queryBranchAttempt(agentBranchRateLimited), wantOutcome: sdkgo.AgentToolAttemptFailed, wantBranch: agentBranchRateLimited, wantHasInvoked: true},
		{name: "connector defect unlisted is failed", invoke: queryBranchAttempt(sdkgo.DefectBranchID), wantOutcome: sdkgo.AgentToolAttemptFailed, wantBranch: sdkgo.DefectBranchID, wantHasInvoked: true},
		{name: "post-Invoke sdkgo defect unlisted is failed", invoke: undeclared, wantOutcome: sdkgo.AgentToolAttemptFailed, wantBranch: sdkgo.DefectBranchID, wantHasInvoked: true},
		{name: "pre-Invoke defect unlisted is failed", isRPCContext: true, wantOutcome: sdkgo.AgentToolAttemptFailed, wantBranch: sdkgo.DefectBranchID, wantHasInvoked: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			operation := &agentQuery{invoke: testCase.invoke}
			tool, err := sdkgo.NewQueryAgentTool(operation, agentConnection, agentQueryMetadata())
			require.NoError(t, err)
			ctx := agentStepContext()
			if testCase.isRPCContext {
				ctx = agentRPCContext()
			}

			attempt, err := tool.RunAttempt(ctx, json.RawMessage(`{"text":"hello","limit":3}`))
			require.NoError(t, err)
			require.Equal(t, testCase.wantOutcome, attempt.Outcome)
			require.Equal(t, testCase.wantBranch, attempt.Branch)
			require.Equal(t, testCase.wantHasInvoked, attempt.HasInvokedOperation)
			require.Equal(t, testCase.wantHasInvoked, operation.invocations == 1)
			if testCase.wantHasInvoked {
				require.Equal(t, []agentInput{{Text: "hello", Limit: 3}}, operation.inputs)
			}
			if testCase.wantOutcome == sdkgo.AgentToolAttemptSucceeded {
				require.JSONEq(t, `{"echo":"hello"}`, string(attempt.Value))
				require.Nil(t, attempt.Failure)
			} else {
				require.NotNil(t, attempt.Failure)
			}
		})
	}
}

func TestAgentToolPassesConnectorRetryThrough(t *testing.T) {
	retryFailure := sdkgo.Failure{Kind: sdkgo.FailureRateLimit, Provider: "agent-provider", Operation: "postReply", Message: "slow down"}
	for _, retryAfter := range []time.Duration{0, 7 * time.Second} {
		mutation := &agentMutation{invoke: func(sdkgo.Call, agentInput) sdkgo.MutationAttempt[agentOutput] {
			return sdkgo.NewMutationRetry[agentOutput](retryFailure, retryAfter)
		}}
		tool, err := sdkgo.NewMutationAgentTool(mutation, agentConnection, agentMutationMetadata())
		require.NoError(t, err)
		attempt, err := tool.RunAttempt(agentStepContext(), nil)
		require.NoError(t, err)
		require.Equal(t, sdkgo.AgentToolAttempt{
			Outcome: sdkgo.AgentToolAttemptRetry, Failure: &retryFailure, RetryAfter: retryAfter, HasInvokedOperation: true,
		}, attempt)

		query := &agentQuery{invoke: func(sdkgo.Call, agentInput) sdkgo.QueryAttempt[agentOutput] {
			return sdkgo.NewQueryRetry[agentOutput](retryFailure, retryAfter)
		}}
		queryTool, err := sdkgo.NewQueryAgentTool(query, agentConnection, agentQueryMetadata())
		require.NoError(t, err)
		attempt, err = queryTool.RunAttempt(agentStepContext(), nil)
		require.NoError(t, err)
		require.Equal(t, sdkgo.AgentToolAttempt{
			Outcome: sdkgo.AgentToolAttemptRetry, Failure: &retryFailure, RetryAfter: retryAfter, HasInvokedOperation: true,
		}, attempt)
	}
}

func TestAgentToolRunsThroughStableCallIdentity(t *testing.T) {
	mutation := &agentMutation{}
	tool, err := sdkgo.NewMutationAgentTool(mutation, agentConnection, agentMutationMetadata())
	require.NoError(t, err)
	require.Equal(t, agentConnection, tool.Connection())

	ctx := agentStepContext()
	_, err = tool.RunAttempt(ctx, json.RawMessage(`{"text":"one"}`))
	require.NoError(t, err)
	ctx.AdvanceAttempt()
	_, err = tool.RunAttempt(ctx, json.RawMessage(`{"text":"one"}`))
	require.NoError(t, err)

	require.Len(t, mutation.calls, 2)
	first, second := mutation.calls[0], mutation.calls[1]
	require.NoError(t, first.ID.Validate())
	require.Equal(t, first.ID, second.ID)
	require.Equal(t, sdkgo.IdempotencyKey("key-"+string(first.ID)), first.IdempotencyKey)
	require.Equal(t, first.IdempotencyKey, second.IdempotencyKey)
	require.Equal(t, agentConnection, first.Connection)
	require.True(t, first.Context == dex.Context(ctx), "the operation receives the caller's own Dex context")
	require.Equal(t, agentMutationRef, first.Operation)
	require.Equal(t, int32(2), second.Context.Attempt())
}

func TestAgentToolDecodesArgumentsStrictly(t *testing.T) {
	invalid := map[string]string{
		"unknown field":  `{"text":"hello","channel":"C1"}`,
		"trailing data":  `{"text":"hello"} {"text":"again"}`,
		"trailing token": `{"text":"hello"}]`,
		"array":          `[{"text":"hello"}]`,
		"null":           `null`,
		"string":         `"hello"`,
		"wrong type":     `{"text":3}`,
		"malformed":      `{"text":`,
	}
	for name, arguments := range invalid {
		t.Run(name, func(t *testing.T) {
			mutation := &agentMutation{}
			tool, err := sdkgo.NewMutationAgentTool(mutation, agentConnection, agentMutationMetadata())
			require.NoError(t, err)
			attempt, err := tool.RunAttempt(agentStepContext(), json.RawMessage(arguments))
			require.ErrorIs(t, err, sdkgo.ErrAgentToolArgumentsInvalid)
			require.ErrorContains(t, err, "agent-provider_post_reply")
			require.Equal(t, sdkgo.AgentToolAttempt{}, attempt)
			require.Zero(t, mutation.invocations)
		})
	}
	for name, arguments := range map[string]string{"empty": ``, "whitespace": " \n\t", "empty object": `{}`} {
		t.Run(name, func(t *testing.T) {
			query := &agentQuery{}
			tool, err := sdkgo.NewQueryAgentTool(query, agentConnection, agentQueryMetadata())
			require.NoError(t, err)
			attempt, err := tool.RunAttempt(agentStepContext(), json.RawMessage(arguments))
			require.NoError(t, err)
			require.Equal(t, sdkgo.AgentToolAttemptSucceeded, attempt.Outcome)
			require.Equal(t, []agentInput{{}}, query.inputs)
		})
	}
}

func TestAgentToolRecoversPanicsAndReportsInvocation(t *testing.T) {
	invokePanics := &agentMutation{invoke: func(sdkgo.Call, agentInput) sdkgo.MutationAttempt[agentOutput] {
		panic("provider response secret-token")
	}}
	tool, err := sdkgo.NewMutationAgentTool(invokePanics, agentConnection, agentMutationMetadata())
	require.NoError(t, err)
	attempt, err := tool.RunAttempt(agentStepContext(), nil)
	require.ErrorIs(t, err, sdkgo.ErrAgentToolPanicked)
	require.NotContains(t, err.Error(), "secret-token")
	require.Equal(t, sdkgo.AgentToolAttempt{HasInvokedOperation: true}, attempt)

	keyPanics := &agentMutation{idempotencyKey: func(sdkgo.CallID) sdkgo.IdempotencyKey { panic("bad key") }}
	tool, err = sdkgo.NewMutationAgentTool(keyPanics, agentConnection, agentMutationMetadata())
	require.NoError(t, err)
	attempt, err = tool.RunAttempt(agentStepContext(), nil)
	require.ErrorIs(t, err, sdkgo.ErrAgentToolPanicked)
	require.Equal(t, sdkgo.AgentToolAttempt{HasInvokedOperation: false}, attempt)
	require.Zero(t, keyPanics.invocations)

	queryPanics := &agentQuery{invoke: func(sdkgo.Call, agentInput) sdkgo.QueryAttempt[agentOutput] { panic(errors.New("boom")) }}
	queryTool, err := sdkgo.NewQueryAgentTool(queryPanics, agentConnection, agentQueryMetadata())
	require.NoError(t, err)
	attempt, err = queryTool.RunAttempt(agentStepContext(), nil)
	require.ErrorIs(t, err, sdkgo.ErrAgentToolPanicked)
	require.True(t, attempt.HasInvokedOperation)
}

type unencodableOutput struct{}

func (unencodableOutput) MarshalJSON() ([]byte, error) { return nil, errors.New("cannot encode") }

type unencodableMutation struct{ invocations int }

func (*unencodableMutation) Definition() sdkgo.MutationDefinition { return agentMutationDefinition() }

func (*unencodableMutation) IdempotencyKey(id sdkgo.CallID, _ agentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

func (mutation *unencodableMutation) Invoke(sdkgo.Call, agentInput) sdkgo.MutationAttempt[unencodableOutput] {
	mutation.invocations++
	return sdkgo.NewMutationBranch(agentBranchSucceeded, unencodableOutput{}, nil, sdkgo.Receipt{})
}

func TestAgentToolReportsUnencodableResultAfterInvoke(t *testing.T) {
	mutation := &unencodableMutation{}
	tool, err := sdkgo.NewMutationAgentTool(mutation, agentConnection, agentMutationMetadata())
	require.NoError(t, err)
	attempt, err := tool.RunAttempt(agentStepContext(), nil)
	require.ErrorIs(t, err, sdkgo.ErrAgentToolResultUnencodable)
	require.ErrorContains(t, err, "agent-provider postReply result value failed with")
	require.Equal(t, sdkgo.AgentToolAttempt{HasInvokedOperation: true}, attempt)
	require.Equal(t, 1, mutation.invocations)
}

func TestAgentToolConstructorsValidateMetadata(t *testing.T) {
	mutationCases := map[string]struct {
		change      func(*sdkgo.AgentToolMetadata)
		connection  sdkgo.ConnectionRef
		wantMessage string
	}{
		"other operation":                   {change: func(m *sdkgo.AgentToolMetadata) { m.Operation.OperationID = "deleteReply" }, wantMessage: "does not match the operation definition"},
		"query kind":                        {change: func(m *sdkgo.AgentToolMetadata) { m.Kind = sdkgo.AgentOperationKindQuery }, wantMessage: "kind"},
		"blank tool name":                   {change: func(m *sdkgo.AgentToolMetadata) { m.ToolName = "" }, wantMessage: "agent tool name"},
		"tool name without prefix":          {change: func(m *sdkgo.AgentToolMetadata) { m.ToolName = "post_reply" }, wantMessage: "agent tool name"},
		"tool name with camel case":         {change: func(m *sdkgo.AgentToolMetadata) { m.ToolName = "agent-provider_postReply" }, wantMessage: "agent tool name"},
		"tool name with another snake case": {change: func(m *sdkgo.AgentToolMetadata) { m.ToolName = "agent-provider_postreply" }, wantMessage: `must be "agent-provider_post_reply"`},
		"blank description":                 {change: func(m *sdkgo.AgentToolMetadata) { m.Description = " " }, wantMessage: "description is required"},
		"branches differ":                   {change: func(m *sdkgo.AgentToolMetadata) { m.Branches = m.Branches[:2] }, wantMessage: "branches do not match"},
		"step defaults differ":              {change: func(m *sdkgo.AgentToolMetadata) { m.StepDefaults.ExecuteMethodTimeout = time.Minute }, wantMessage: "Step defaults do not match"},
		"retry policy differs":              {change: func(m *sdkgo.AgentToolMetadata) { m.StepDefaults.ExecuteRetry = nil }, wantMessage: "Step defaults do not match"},
		"required branch listed": {change: func(m *sdkgo.AgentToolMetadata) {
			m.BranchOutcomes[agentBranchSucceeded] = sdkgo.AgentBranchOutcomeRejected
		}, wantMessage: "not an optional branch"},
		"undeclared branch listed": {change: func(m *sdkgo.AgentToolMetadata) {
			m.BranchOutcomes[agentBranchUndeclared] = sdkgo.AgentBranchOutcomeRejected
		}, wantMessage: "not an optional branch"},
		"unknown outcome value": {change: func(m *sdkgo.AgentToolMetadata) { m.BranchOutcomes[agentBranchConflict] = "failed" }, wantMessage: "must be rejected or unknown"},
		"uncertain listed rejected": {change: func(m *sdkgo.AgentToolMetadata) {
			m.BranchOutcomes[sdkgo.UncertainBranchID] = sdkgo.AgentBranchOutcomeRejected
		}, wantMessage: "uncertain cannot be rejected"},
		"missing input schema":      {change: func(m *sdkgo.AgentToolMetadata) { m.InputSchema = nil }, wantMessage: "input schema must be a JSON object"},
		"null input schema":         {change: func(m *sdkgo.AgentToolMetadata) { m.InputSchema = json.RawMessage(`null`) }, wantMessage: "input schema must be a JSON object"},
		"array input schema":        {change: func(m *sdkgo.AgentToolMetadata) { m.InputSchema = json.RawMessage(`[]`) }, wantMessage: "input schema must be a JSON object"},
		"invalid connection":        {connection: sdkgo.ConnectionRef{Provider: "agent-provider"}, wantMessage: "agent tool connection"},
		"retry safe mutation is ok": {change: func(m *sdkgo.AgentToolMetadata) { m.IsRetrySafe = true }},
		"listed defect unknown is ok": {change: func(m *sdkgo.AgentToolMetadata) {
			m.BranchOutcomes[sdkgo.DefectBranchID] = sdkgo.AgentBranchOutcomeUnknown
		}},
	}
	for name, testCase := range mutationCases {
		t.Run("mutation "+name, func(t *testing.T) {
			metadata := agentMutationMetadata()
			if testCase.change != nil {
				testCase.change(&metadata)
			}
			connection := agentConnection
			if testCase.connection != (sdkgo.ConnectionRef{}) {
				connection = testCase.connection
			}
			tool, err := sdkgo.NewMutationAgentTool(&agentMutation{}, connection, metadata)
			if testCase.wantMessage == "" {
				require.NoError(t, err)
				require.NotNil(t, tool)
				return
			}
			require.ErrorContains(t, err, testCase.wantMessage)
			require.Nil(t, tool)
		})
	}

	queryCases := map[string]struct {
		change      func(*sdkgo.AgentToolMetadata)
		wantMessage string
	}{
		"mutation kind":              {change: func(m *sdkgo.AgentToolMetadata) { m.Kind = sdkgo.AgentOperationKindMutation }, wantMessage: "kind"},
		"dex-agents style tool name": {change: func(m *sdkgo.AgentToolMetadata) { m.ToolName = "agent-provider_list_story_i_d_s" }, wantMessage: `must be "agent-provider_list_story_ids"`},
		"retry safe query":           {change: func(m *sdkgo.AgentToolMetadata) { m.IsRetrySafe = true }, wantMessage: "only a Mutation may be retry safe"},
		"unknown query outcome": {change: func(m *sdkgo.AgentToolMetadata) {
			m.BranchOutcomes[agentBranchRateLimited] = sdkgo.AgentBranchOutcomeUnknown
		}, wantMessage: "a Query branch may only be rejected"},
		"listed defect rejected": {change: func(m *sdkgo.AgentToolMetadata) {
			m.BranchOutcomes[sdkgo.DefectBranchID] = sdkgo.AgentBranchOutcomeRejected
		}},
		"no branch outcomes": {change: func(m *sdkgo.AgentToolMetadata) { m.BranchOutcomes = nil }},
	}
	for name, testCase := range queryCases {
		t.Run("query "+name, func(t *testing.T) {
			metadata := agentQueryMetadata()
			testCase.change(&metadata)
			tool, err := sdkgo.NewQueryAgentTool(&agentQuery{}, agentConnection, metadata)
			if testCase.wantMessage == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, testCase.wantMessage)
			require.Nil(t, tool)
		})
	}

	_, err := sdkgo.NewQueryAgentTool[agentInput, agentOutput](nil, agentConnection, agentQueryMetadata())
	require.ErrorContains(t, err, "Query operation is required")
	var nilMutation *agentMutation
	_, err = sdkgo.NewMutationAgentTool[agentInput, agentOutput](nilMutation, agentConnection, agentMutationMetadata())
	require.ErrorContains(t, err, "Mutation operation is required")
}

func TestAgentToolMetadataIsIsolatedFromCallers(t *testing.T) {
	metadata := agentMutationMetadata()
	tool, err := sdkgo.NewMutationAgentTool(&agentMutation{invoke: mutationBranchAttempt(agentBranchRejected)}, agentConnection, metadata)
	require.NoError(t, err)

	metadata.BranchOutcomes[agentBranchRejected] = sdkgo.AgentBranchOutcomeUnknown
	returned := tool.Metadata()
	returned.BranchOutcomes[agentBranchRejected] = sdkgo.AgentBranchOutcomeUnknown
	returned.InputSchema[0] = '['
	returned.Branches[0].Optional = true
	returned.StepDefaults.ExecuteRetry.InitialInterval = time.Hour

	attempt, err := tool.RunAttempt(agentStepContext(), nil)
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptRejected, attempt.Outcome)
	fresh := tool.Metadata()
	require.Equal(t, sdkgo.AgentBranchOutcomeRejected, fresh.BranchOutcomes[agentBranchRejected])
	require.JSONEq(t, string(agentInputSchema), string(fresh.InputSchema))
	require.False(t, fresh.Branches[0].Optional)
	require.Equal(t, agentRetryPolicyInterval, fresh.StepDefaults.ExecuteRetry.InitialInterval)
	require.Equal(t, "agent-provider_post_reply", fresh.ToolName)
	require.True(t, fresh.IsApprovalRecommended)
	require.Equal(t, "PostReplyInput", fresh.InputType)
}

// fencedMutation sends through the singledispatch Fence and then selects branch, as a connector that
// misclassifies a post-send outcome would.
type fencedMutation struct {
	branch        sdkgo.BranchID
	sendErr       error
	shouldSend    bool
	afterDispatch func(sdkgo.Call)
	duringSend    func()
	providerSent  int
	callIDs       []sdkgo.CallID
}

func (*fencedMutation) Definition() sdkgo.MutationDefinition { return agentMutationDefinition() }

func (*fencedMutation) IdempotencyKey(id sdkgo.CallID, _ agentInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(id)
}

func (mutation *fencedMutation) Invoke(call sdkgo.Call, _ agentInput) sdkgo.MutationAttempt[agentOutput] {
	mutation.callIDs = append(mutation.callIDs, call.ID)
	failure := agentValidFailure
	fence, err := singledispatch.NewFence(call, 30*time.Second)
	if err != nil {
		panic(err)
	}
	decision := fence.Decide()
	if mutation.shouldSend && decision.Kind == singledispatch.SendAllowed {
		dispatchErr := fence.Dispatch(func(context.Context) error {
			mutation.providerSent++
			if mutation.duringSend != nil {
				mutation.duringSend()
			}
			return mutation.sendErr
		})
		if errors.Is(dispatchErr, errProviderOverloaded) {
			if err := fence.RecordCanResend(); err != nil {
				panic(err)
			}
		}
	}
	if mutation.afterDispatch != nil {
		mutation.afterDispatch(call)
	}
	return sdkgo.NewMutationBranch(mutation.branch, agentOutput{}, &failure, sdkgo.Receipt{})
}

var errProviderOverloaded = errors.New("provider answered 503")

func TestMutationAgentToolTreatsFenceClaimAsPossibleSend(t *testing.T) {
	cases := []struct {
		name         string
		mutation     *fencedMutation
		prepare      func(*testing.T, *singledispatchtest.Context)
		wantOutcome  sdkgo.AgentToolAttemptOutcome
		wantProvider int
	}{
		{
			name:     "claimed send then a defect is unknown",
			mutation: &fencedMutation{branch: sdkgo.DefectBranchID, shouldSend: true}, wantOutcome: sdkgo.AgentToolAttemptUnknown, wantProvider: 1,
		},
		{
			name:     "claimed send then a listed rejected provider branch is unknown",
			mutation: &fencedMutation{branch: agentBranchRejected, shouldSend: true}, wantOutcome: sdkgo.AgentToolAttemptUnknown, wantProvider: 1,
		},
		{
			// A claim stays a possible dispatch even after canResend proves the request unprocessed.
			name:     "claimed send proven unprocessed is still unknown",
			mutation: &fencedMutation{branch: agentBranchRejected, shouldSend: true, sendErr: errProviderOverloaded}, wantOutcome: sdkgo.AgentToolAttemptUnknown, wantProvider: 1,
		},
		{
			name:     "fence decided without a send keeps the listed rejected outcome",
			mutation: &fencedMutation{branch: agentBranchRejected}, wantOutcome: sdkgo.AgentToolAttemptRejected,
		},
		{
			name:     "unreadable fence checkpoint is unknown",
			mutation: &fencedMutation{branch: agentBranchRejected, shouldSend: true},
			prepare: func(_ *testing.T, ctx *singledispatchtest.Context) {
				ctx.SetReadError(errors.New("heartbeat unavailable"))
			},
			wantOutcome: sdkgo.AgentToolAttemptUnknown,
		},
		{
			name:     "foreign fence checkpoint is unknown",
			mutation: &fencedMutation{branch: agentBranchRejected, shouldSend: true},
			prepare: func(t *testing.T, ctx *singledispatchtest.Context) {
				foreign := singledispatch.Marker{CallID: "4f1c6f4e-0000-5000-8000-000000000000", Attempt: 1, State: singledispatch.StateNotSent}
				require.NoError(t, ctx.SetPersistedCheckpoint(foreign))
				ctx.AdvanceAttempt()
			},
			wantOutcome: sdkgo.AgentToolAttemptUnknown,
		},
		{
			name:     "lost checkpoint after attempt one is unknown",
			mutation: &fencedMutation{branch: agentBranchRejected, shouldSend: true},
			prepare: func(_ *testing.T, ctx *singledispatchtest.Context) {
				ctx.AdvanceAttempt()
			},
			wantOutcome: sdkgo.AgentToolAttemptUnknown,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tool, err := sdkgo.NewMutationAgentTool(testCase.mutation, agentConnection, agentMutationMetadata())
			require.NoError(t, err)
			ctx := agentStepContext()
			if testCase.prepare != nil {
				testCase.prepare(t, ctx)
			}
			attempt, err := tool.RunAttempt(ctx, nil)
			require.NoError(t, err)
			require.Equal(t, testCase.wantOutcome, attempt.Outcome)
			require.Equal(t, testCase.wantProvider, testCase.mutation.providerSent)
		})
	}
}

func TestMutationAgentToolReadsPriorFenceCheckpoint(t *testing.T) {
	metadata := agentMutationMetadata()

	claimed := &fencedMutation{branch: agentBranchRejected, shouldSend: true}
	tool, err := sdkgo.NewMutationAgentTool(claimed, agentConnection, metadata)
	require.NoError(t, err)
	ctx := agentStepContext()
	attempt, err := tool.RunAttempt(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptUnknown, attempt.Outcome)

	// Attempt two finds the claim, may not send, and its listed rejected branch stays unknown.
	ctx.AdvanceAttempt()
	attempt, err = tool.RunAttempt(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptUnknown, attempt.Outcome)
	require.Equal(t, 1, claimed.providerSent)

	overloaded := &fencedMutation{branch: agentBranchRejected, shouldSend: true, sendErr: errProviderOverloaded}
	tool, err = sdkgo.NewMutationAgentTool(overloaded, agentConnection, metadata)
	require.NoError(t, err)
	ctx = agentStepContext()
	_, err = tool.RunAttempt(ctx, nil)
	require.NoError(t, err)
	// Attempt two finds canResend, may send again, and claims again, so it is unknown.
	ctx.AdvanceAttempt()
	attempt, err = tool.RunAttempt(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptUnknown, attempt.Outcome)
	require.Equal(t, 2, overloaded.providerSent)

	declined := &fencedMutation{branch: agentBranchRejected}
	tool, err = sdkgo.NewMutationAgentTool(declined, agentConnection, metadata)
	require.NoError(t, err)
	ctx = agentStepContext()
	require.NoError(t, ctx.SetPersistedCheckpoint(singledispatch.Marker{
		CallID: firstCallID(t, overloaded), Attempt: 1, State: singledispatch.StateCanResend,
	}))
	ctx.AdvanceAttempt()
	// Attempt two may send after canResend, declines to, and keeps the listed rejected outcome.
	attempt, err = tool.RunAttempt(ctx, nil)
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptRejected, attempt.Outcome)
}

func TestMutationAgentToolIgnoresNonFenceHeartbeats(t *testing.T) {
	mutation := &agentMutation{invoke: func(call sdkgo.Call, _ agentInput) sdkgo.MutationAttempt[agentOutput] {
		if err := call.Context.RecordHeartbeat(map[string]string{"state": "claimed"}); err != nil {
			panic(err)
		}
		failure := agentValidFailure
		return sdkgo.NewMutationBranch(agentBranchRejected, agentOutput{}, &failure, sdkgo.Receipt{})
	}}
	tool, err := sdkgo.NewMutationAgentTool(mutation, agentConnection, agentMutationMetadata())
	require.NoError(t, err)
	attempt, err := tool.RunAttempt(agentStepContext(), nil)
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptRejected, attempt.Outcome)
}

const agentSecret = "sk-live-agent-secret"

type secretRefusingInput struct {
	Token secretRefusingToken `json:"token"`
}

type secretRefusingToken string

func (*secretRefusingToken) UnmarshalJSON(contents []byte) error {
	return fmt.Errorf("token %s is not accepted", contents)
}

type secretRefusingQuery struct{ invocations int }

func (*secretRefusingQuery) Definition() sdkgo.QueryDefinition { return agentQueryDefinition() }

func (query *secretRefusingQuery) Invoke(sdkgo.Call, secretRefusingInput) sdkgo.QueryAttempt[agentOutput] {
	query.invocations++
	return sdkgo.NewQueryBranch(agentBranchSucceeded, agentOutput{}, nil, sdkgo.Receipt{})
}

type secretLeakingOutput struct{}

func (secretLeakingOutput) MarshalJSON() ([]byte, error) {
	return nil, errors.New("response held " + agentSecret)
}

type secretLeakingQuery struct{}

func (secretLeakingQuery) Definition() sdkgo.QueryDefinition { return agentQueryDefinition() }

func (secretLeakingQuery) Invoke(sdkgo.Call, agentInput) sdkgo.QueryAttempt[secretLeakingOutput] {
	return sdkgo.NewQueryBranch(agentBranchSucceeded, secretLeakingOutput{}, nil, sdkgo.Receipt{})
}

func TestAgentToolErrorsNeverQuoteArgumentsOrResults(t *testing.T) {
	refusing := &secretRefusingQuery{}
	tool, err := sdkgo.NewQueryAgentTool(refusing, agentConnection, agentQueryMetadata())
	require.NoError(t, err)
	_, err = tool.RunAttempt(agentStepContext(), json.RawMessage(`{"token":"`+agentSecret+`"}`))
	require.ErrorIs(t, err, sdkgo.ErrAgentToolArgumentsInvalid)
	require.NotContains(t, err.Error(), agentSecret)
	require.Contains(t, err.Error(), "refused by the operation input type")
	require.Zero(t, refusing.invocations)

	plain, err := sdkgo.NewQueryAgentTool(&agentQuery{}, agentConnection, agentQueryMetadata())
	require.NoError(t, err)
	for _, arguments := range []string{
		`{"text":"` + agentSecret + `"`,
		`{"text":"` + agentSecret + `",}`,
		`{"limit":"` + agentSecret + `"}`,
		`{"` + agentSecret + `":1}`,
		`{"text":"ok"} "` + agentSecret + `"`,
	} {
		_, err = plain.RunAttempt(agentStepContext(), json.RawMessage(arguments))
		require.ErrorIs(t, err, sdkgo.ErrAgentToolArgumentsInvalid, arguments)
		require.NotContains(t, err.Error(), agentSecret, arguments)
	}

	leaking, err := sdkgo.NewQueryAgentTool[agentInput, secretLeakingOutput](secretLeakingQuery{}, agentConnection, agentQueryMetadata())
	require.NoError(t, err)
	attempt, err := leaking.RunAttempt(agentStepContext(), nil)
	require.ErrorIs(t, err, sdkgo.ErrAgentToolResultUnencodable)
	require.NotContains(t, err.Error(), agentSecret)
	require.True(t, attempt.HasInvokedOperation)
}

func TestMutationAgentToolKeepsClaimDespiteLaterHeartbeats(t *testing.T) {
	var typedNilMarker *singledispatch.Marker
	overwrites := map[string]func(sdkgo.Call) any{
		"zero marker":      func(sdkgo.Call) any { return singledispatch.Marker{} },
		"typed nil marker": func(sdkgo.Call) any { return typedNilMarker },
		"nil heartbeat":    func(sdkgo.Call) any { return nil },
		"foreign marker": func(call sdkgo.Call) any {
			return singledispatch.Marker{CallID: "4f1c6f4e-0000-5000-8000-000000000000", Attempt: call.Context.Attempt(), State: singledispatch.StateCanResend}
		},
		"own canResend marker written directly": func(call sdkgo.Call) any {
			return singledispatch.Marker{CallID: call.ID, Attempt: call.Context.Attempt(), State: singledispatch.StateCanResend}
		},
		"own notSent marker written directly": func(call sdkgo.Call) any {
			return singledispatch.Marker{CallID: call.ID, Attempt: call.Context.Attempt(), State: singledispatch.StateNotSent}
		},
	}
	for name, heartbeat := range overwrites {
		t.Run(name, func(t *testing.T) {
			mutation := &fencedMutation{branch: agentBranchRejected, shouldSend: true, afterDispatch: func(call sdkgo.Call) {
				if err := call.Context.RecordHeartbeat(heartbeat(call)); err != nil {
					panic(err)
				}
			}}
			tool, err := sdkgo.NewMutationAgentTool(mutation, agentConnection, agentMutationMetadata())
			require.NoError(t, err)
			attempt, err := tool.RunAttempt(agentStepContext(), nil)
			require.NoError(t, err)
			require.Equal(t, sdkgo.AgentToolAttemptUnknown, attempt.Outcome)
			require.Equal(t, 1, mutation.providerSent)
		})
	}
}

func TestMutationAgentToolObservesOnlyItsOwnAttempt(t *testing.T) {
	claimed := &fencedMutation{branch: agentBranchRejected, shouldSend: true}
	tool, err := sdkgo.NewMutationAgentTool(claimed, agentConnection, agentMutationMetadata())
	require.NoError(t, err)
	attempt, err := tool.RunAttempt(agentStepContext(), nil)
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptUnknown, attempt.Outcome)

	// The same Call ID and attempt in a fresh context starts a fresh observation.
	declined := &fencedMutation{branch: agentBranchRejected}
	tool, err = sdkgo.NewMutationAgentTool(declined, agentConnection, agentMutationMetadata())
	require.NoError(t, err)
	attempt, err = tool.RunAttempt(agentStepContext(), nil)
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptRejected, attempt.Outcome)

	// A Fence used outside RunAttempt reports to no observer.
	fence, err := singledispatch.NewFence(sdkgo.Call{Context: agentStepContext(), ID: "4f1c6f4e-0000-5000-8000-000000000001"}, 30*time.Second)
	require.NoError(t, err)
	require.Equal(t, singledispatch.SendAllowed, fence.Decide().Kind)
	require.NoError(t, fence.Dispatch(func(context.Context) error { return nil }))
}

func TestQueryAgentToolDefectFollowsItsListing(t *testing.T) {
	metadata := agentQueryMetadata()
	metadata.BranchOutcomes[sdkgo.DefectBranchID] = sdkgo.AgentBranchOutcomeRejected
	tool, err := sdkgo.NewQueryAgentTool(&agentQuery{invoke: queryBranchAttempt(sdkgo.DefectBranchID)}, agentConnection, metadata)
	require.NoError(t, err)
	attempt, err := tool.RunAttempt(agentStepContext(), nil)
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptRejected, attempt.Outcome)

	tool, err = sdkgo.NewQueryAgentTool(&agentQuery{}, agentConnection, metadata)
	require.NoError(t, err)
	attempt, err = tool.RunAttempt(agentRPCContext(), nil)
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptRejected, attempt.Outcome, "a listed defect is rejected before Invoke too")
	require.False(t, attempt.HasInvokedOperation)
	require.Equal(t, "failed", string(sdkgo.AgentToolAttemptFailed))
}

func firstCallID(t *testing.T, mutation *fencedMutation) sdkgo.CallID {
	t.Helper()
	require.NotEmpty(t, mutation.callIDs)
	return mutation.callIDs[0]
}

func runAgentToolAttemptAsync(tool sdkgo.AgentTool, ctx dex.Context) <-chan sdkgo.AgentToolAttempt {
	attempts := make(chan sdkgo.AgentToolAttempt, 1)
	go func() {
		attempt, err := tool.RunAttempt(ctx, nil)
		if err != nil {
			panic(err)
		}
		attempts <- attempt
	}()
	return attempts
}

func TestConcurrentSameCallAttemptsNeverClearAClaim(t *testing.T) {
	hasClaimed := make(chan struct{})
	release := make(chan struct{})
	claimed := &fencedMutation{branch: agentBranchRejected, shouldSend: true, duringSend: func() {
		close(hasClaimed)
		<-release
	}}
	resent := &fencedMutation{branch: agentBranchRejected, shouldSend: true, sendErr: errProviderOverloaded, afterDispatch: func(sdkgo.Call) {
		close(release)
	}}
	claimedTool, err := sdkgo.NewMutationAgentTool(claimed, agentConnection, agentMutationMetadata())
	require.NoError(t, err)
	resentTool, err := sdkgo.NewMutationAgentTool(resent, agentConnection, agentMutationMetadata())
	require.NoError(t, err)

	claimedAttempt := runAgentToolAttemptAsync(claimedTool, agentStepContext())
	<-hasClaimed
	// The second attempt shares the Call ID, connection, and attempt, and records canResend mid-send.
	resentAttempt := runAgentToolAttemptAsync(resentTool, agentStepContext())
	require.Equal(t, sdkgo.AgentToolAttemptUnknown, (<-claimedAttempt).Outcome)
	require.Equal(t, sdkgo.AgentToolAttemptUnknown, (<-resentAttempt).Outcome)
	require.Equal(t, 1, claimed.providerSent)
	require.Equal(t, 1, resent.providerSent)
}

func TestConcurrentAttemptsOnOtherConnectionsAreIsolated(t *testing.T) {
	hasClaimed := make(chan struct{})
	release := make(chan struct{})
	claimed := &fencedMutation{branch: agentBranchRejected, shouldSend: true, duringSend: func() {
		close(hasClaimed)
		<-release
	}}
	claimedTool, err := sdkgo.NewMutationAgentTool(claimed, agentConnection, agentMutationMetadata())
	require.NoError(t, err)
	otherConnection := sdkgo.ConnectionRef{Provider: agentConnection.Provider, Name: "personal"}
	declinedTool, err := sdkgo.NewMutationAgentTool(&fencedMutation{branch: agentBranchRejected}, otherConnection, agentMutationMetadata())
	require.NoError(t, err)

	claimedAttempt := runAgentToolAttemptAsync(claimedTool, agentStepContext())
	<-hasClaimed
	declined, err := declinedTool.RunAttempt(agentStepContext(), nil)
	require.NoError(t, err)
	require.Equal(t, sdkgo.AgentToolAttemptRejected, declined.Outcome, "another connection's claim does not taint this call")
	close(release)
	require.Equal(t, sdkgo.AgentToolAttemptUnknown, (<-claimedAttempt).Outcome)
}
