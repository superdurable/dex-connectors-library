// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package connector_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/internal/testsupport"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
	"github.com/superdurable/dex/sdk-go/dex"
)

var (
	testConnection  = connector.ConnectionRef{Provider: "mock", Name: "default"}
	testQueryRef    = connector.OperationRef{ConnectorID: "mock-provider", OperationID: "getProfile"}
	testMutationRef = connector.OperationRef{ConnectorID: "mock-provider", OperationID: "grantCredit"}
)

type queryOperation struct {
	call    connector.Call
	attempt connector.QueryAttempt[string]
	useSet  bool
}

func (*queryOperation) Definition() connector.QueryDefinition {
	return connector.QueryDefinition{Operation: testQueryRef}
}

func (operation *queryOperation) Invoke(call connector.Call, input string) connector.QueryAttempt[string] {
	operation.call = call
	if operation.useSet {
		return operation.attempt
	}
	return connector.NewQuerySuccess(input, connector.Receipt{})
}

type mutationOperation struct {
	call       connector.Call
	attempt    connector.MutationAttempt[string]
	useSet     bool
	derivedKey connector.IdempotencyKey
}

func (*mutationOperation) Definition() connector.MutationDefinition {
	return connector.MutationDefinition{Operation: testMutationRef}
}

func (operation *mutationOperation) IdempotencyKey(connector.CallID, string) connector.IdempotencyKey {
	return operation.derivedKey
}

func (operation *mutationOperation) Invoke(call connector.Call, _ string) connector.MutationAttempt[string] {
	operation.call = call
	if operation.useSet {
		return operation.attempt
	}
	return connector.NewMutationSuccess("ok", connector.Receipt{})
}

var (
	_ connector.Query[string, string]    = (*queryOperation)(nil)
	_ connector.Mutation[string, string] = (*mutationOperation)(nil)
)

func TestCallIDUsesStableDexAndOperationIdentity(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "step-execution-1")
	operation := &queryOperation{}
	first, err := connector.RunQuery(ctx, operation, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, connector.QuerySucceeded, first.Outcome)
	firstID := first.Receipt.CallID
	require.Equal(t, connector.CallID("8fe78557-483c-5b53-ace7-7d6a13eb30db"), firstID)

	ctx.Run = "run-2"
	ctx.AttemptNo = 9
	second, err := connector.RunQuery(ctx, operation, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, firstID, second.Receipt.CallID)
	otherConnection, err := connector.RunQuery(ctx, operation, connector.ConnectionRef{Provider: "mock", Name: "other"}, "profile")
	require.NoError(t, err)
	require.Equal(t, firstID, otherConnection.Receipt.CallID)

	for _, changed := range []*testsupport.DexContext{
		testsupport.NewDexContext("customer/43", "step-execution-1"),
		testsupport.NewDexContext("customer/42", "step-execution-2"),
	} {
		result, runErr := connector.RunQuery(changed, operation, testConnection, "profile")
		require.NoError(t, runErr)
		require.NotEqual(t, firstID, result.Receipt.CallID)
	}

	otherOperation := &queryOperationWithRef{ref: connector.OperationRef{ConnectorID: "mock-provider", OperationID: "getAccount"}}
	result, err := connector.RunQuery(ctx, otherOperation, testConnection, "profile")
	require.NoError(t, err)
	require.NotEqual(t, firstID, result.Receipt.CallID)

	otherConnector := &queryOperationWithRef{ref: connector.OperationRef{ConnectorID: "other-provider", OperationID: "getProfile"}}
	result, err = connector.RunQuery(ctx, otherConnector, testConnection, "profile")
	require.NoError(t, err)
	require.NotEqual(t, firstID, result.Receipt.CallID)

	left, err := connector.RunQuery(testsupport.NewDexContext("a", "bc"), operation, testConnection, "profile")
	require.NoError(t, err)
	right, err := connector.RunQuery(testsupport.NewDexContext("ab", "c"), operation, testConnection, "profile")
	require.NoError(t, err)
	require.NotEqual(t, left.Receipt.CallID, right.Receipt.CallID)
}

type queryOperationWithRef struct{ ref connector.OperationRef }

func (operation *queryOperationWithRef) Definition() connector.QueryDefinition {
	return connector.QueryDefinition{Operation: operation.ref}
}

func (*queryOperationWithRef) Invoke(connector.Call, string) connector.QueryAttempt[string] {
	return connector.NewQuerySuccess("ok", connector.Receipt{})
}

func TestMutationDerivesStableProviderIdempotencyKey(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "step-execution-1")
	operation := &mutationOperation{derivedKey: "provider-prefix-stable"}
	first, err := connector.RunMutation(ctx, operation, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, connector.MutationSucceeded, first.Outcome)
	require.Equal(t, operation.call.ID, first.Receipt.CallID)
	require.Equal(t, connector.IdempotencyKey("provider-prefix-stable"), operation.call.IdempotencyKey)
	require.Equal(t, operation.call.IdempotencyKey, first.Receipt.IdempotencyKey)

	firstCallID := operation.call.ID
	ctx.Run = "another-run"
	ctx.AttemptNo = 7
	second, err := connector.RunMutation(ctx, operation, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, firstCallID, operation.call.ID)
	require.Equal(t, first.Receipt.IdempotencyKey, second.Receipt.IdempotencyKey)

	fallback := &mutationOperation{}
	result, err := connector.RunMutation(ctx, fallback, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, connector.IdempotencyKey(fallback.call.ID), fallback.call.IdempotencyKey)
	require.Equal(t, fallback.call.IdempotencyKey, result.Receipt.IdempotencyKey)

	otherStep := testsupport.NewDexContext("customer/42", "step-execution-2")
	result, err = connector.RunMutation(otherStep, fallback, testConnection, "credits")
	require.NoError(t, err)
	require.NotEqual(t, firstCallID, result.Receipt.CallID)

	invalid := &mutationOperation{derivedKey: " "}
	result, err = connector.RunMutation(ctx, invalid, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, connector.MutationFailed, result.Outcome)
	require.Equal(t, connector.FailureLocalDefect, result.Failure.Kind)
	require.Empty(t, result.Receipt.IdempotencyKey)
	require.Equal(t, connector.Call{}, invalid.call)
}

func TestRPCContextFailsBeforeOperationInvocation(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "")
	query := &queryOperation{}
	queryResult, err := connector.RunQuery(ctx, query, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, connector.QueryFailed, queryResult.Outcome)
	require.Equal(t, connector.FailureLocalDefect, queryResult.Failure.Kind)
	require.Contains(t, queryResult.Failure.Message, "RPC invocation is not allowed")
	require.Equal(t, connector.Call{}, query.call)

	mutation := &mutationOperation{}
	mutationResult, err := connector.RunMutation(ctx, mutation, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, connector.MutationFailed, mutationResult.Outcome)
	require.Equal(t, connector.Call{}, mutation.call)
}

func TestCredentialProviderReceivesCompleteCallMetadata(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "step-execution-1")
	provider := &capturingCredentialProvider{}
	operation := credentialQuery{provider: provider}
	result, err := connector.RunQuery(ctx, operation, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, connector.QuerySucceeded, result.Outcome)
	require.Same(t, ctx, provider.call.Context)
	require.Equal(t, testConnection, provider.call.Connection)
	require.Equal(t, testQueryRef, provider.call.Operation)
	require.NotEmpty(t, provider.call.ID)
	require.Empty(t, provider.call.IdempotencyKey)
	require.NotContains(t, fmt.Sprintf("%+v", provider.call), "super-secret")
}

type capturingCredentialProvider struct{ call connector.Call }

func (provider *capturingCredentialProvider) Resolve(call connector.Call) (connector.Credential, error) {
	provider.call = call
	return connector.NewCredential(map[string]string{"api_key": "super-secret"}), nil
}

type credentialQuery struct{ provider connector.CredentialProvider }

func (credentialQuery) Definition() connector.QueryDefinition {
	return connector.QueryDefinition{Operation: testQueryRef}
}

func (operation credentialQuery) Invoke(call connector.Call, _ string) connector.QueryAttempt[string] {
	if _, err := operation.provider.Resolve(call); err != nil {
		return connector.NewQueryFailure("", failure(connector.FailureAuthentication, "credentials unavailable"), connector.Receipt{})
	}
	return connector.NewQuerySuccess("ok", connector.Receipt{})
}

func TestCredentialCannotSerializeOrFormatSecret(t *testing.T) {
	credential := connector.NewCredential(map[string]string{"api_key": "super-secret"})
	require.NotContains(t, fmt.Sprintf("%v", credential), "super-secret")
	require.NotContains(t, fmt.Sprintf("%#v", credential), "super-secret")
	_, err := json.Marshal(credential)
	require.ErrorContains(t, err, "cannot be serialized")
}

func TestStrictAttemptsAndRetryMapping(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "step-execution-1")

	queryFailure := &queryOperation{useSet: true, attempt: connector.NewQueryFailure("", failure(connector.FailureNotFound, "missing"), connector.Receipt{})}
	result, err := connector.RunQuery(ctx, queryFailure, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, connector.QueryFailed, result.Outcome)
	require.Equal(t, connector.FailureNotFound, result.Failure.Kind)

	queryRetry := &queryOperation{useSet: true, attempt: connector.NewQueryRetry[string](failure(connector.FailureNotFound, "eventually consistent"), 3*time.Second)}
	_, err = connector.RunQuery(ctx, queryRetry, testConnection, "profile")
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 3*time.Second, retryAfter.After)
	var retry *connector.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, connector.FailureNotFound, retry.Failure.Kind)

	queryDefaultBackoff := &queryOperation{useSet: true, attempt: connector.NewQueryRetry[string](failure(connector.FailureAvailability, "unavailable"), 0)}
	_, err = connector.RunQuery(ctx, queryDefaultBackoff, testConnection, "profile")
	require.ErrorAs(t, err, &retry)
	require.False(t, errors.As(err, &retryAfter))
}

func TestMutationAttemptsAreDistinctResults(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "step-execution-1")

	succeeded, err := connector.RunMutation(ctx, &mutationOperation{}, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, connector.MutationSucceeded, succeeded.Outcome)

	failedAttempt := connector.NewMutationFailure("", failure(connector.FailureAuthorization, "denied"), connector.Receipt{})
	failed, err := connector.RunMutation(ctx, &mutationOperation{useSet: true, attempt: failedAttempt}, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, connector.MutationFailed, failed.Outcome)
	require.Equal(t, connector.FailureAuthorization, failed.Failure.Kind)

	unknownAttempt := connector.NewMutationUnknown("provider-response-id", failure(connector.FailureTransport, "response lost"), connector.Receipt{})
	unknown, err := connector.RunMutation(ctx, &mutationOperation{useSet: true, attempt: unknownAttempt}, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, connector.MutationUnknown, unknown.Outcome)
	require.Equal(t, "provider-response-id", unknown.Value)

	_, err = connector.RunMutation(ctx, &mutationOperation{useSet: true, attempt: connector.NewMutationRetry[string](failure(connector.FailureRateLimit, "try later"), 0)}, testConnection, "credits")
	var retry *connector.RetryError
	require.ErrorAs(t, err, &retry)
}

func TestZeroAttemptsFailClosed(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "step-execution-1")

	queryResult, err := connector.RunQuery(ctx, invalidQuery{}, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, connector.QueryFailed, queryResult.Outcome)
	require.Equal(t, connector.FailureLocalDefect, queryResult.Failure.Kind)

	mutationResult, err := connector.RunMutation(ctx, invalidMutation{}, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, connector.MutationUnknown, mutationResult.Outcome)
	require.Equal(t, connector.FailureLocalDefect, mutationResult.Failure.Kind)

	invalidRetry := connector.Failure{Kind: "MADE_UP", Provider: "mock", Operation: "query", Message: "invalid"}
	queryResult, err = connector.RunQuery(ctx, &queryOperation{
		useSet: true, attempt: connector.NewQueryRetry[string](invalidRetry, 0),
	}, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, connector.QueryFailed, queryResult.Outcome)

	mutationResult, err = connector.RunMutation(ctx, &mutationOperation{
		useSet: true, attempt: connector.NewMutationRetry[string](invalidRetry, 0),
	}, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, connector.MutationUnknown, mutationResult.Outcome)
}

type invalidQuery struct{}

func (invalidQuery) Definition() connector.QueryDefinition {
	return connector.QueryDefinition{Operation: testQueryRef}
}

func (invalidQuery) Invoke(connector.Call, string) connector.QueryAttempt[string] {
	return connector.QueryAttempt[string]{}
}

type invalidMutation struct{}

func (invalidMutation) Definition() connector.MutationDefinition {
	return connector.MutationDefinition{Operation: testMutationRef}
}

func (invalidMutation) IdempotencyKey(connector.CallID, string) connector.IdempotencyKey { return "" }

func (invalidMutation) Invoke(connector.Call, string) connector.MutationAttempt[string] {
	return connector.MutationAttempt[string]{}
}

func TestProgressMethodsAreNoOpWithoutStreams(t *testing.T) {
	operation := &queryOperation{}
	result, err := connector.RunQuery(testsupport.NewDexContext("customer/42", "step-execution-1"), operation, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, connector.QuerySucceeded, result.Outcome)
	require.False(t, operation.call.HasProgressStream())
	require.False(t, operation.call.HasTextStream())
	require.NoError(t, operation.call.ReportProgress(connector.Progress{Phase: "started"}))
	require.NoError(t, operation.call.WriteText("hello"))
}

func failure(kind connector.FailureKind, message string) connector.Failure {
	return connector.Failure{Kind: kind, Provider: "mock-provider", Operation: "getProfile", Message: message}
}
