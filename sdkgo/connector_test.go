// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sdkgo_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/internal/testsupport"
	"github.com/superdurable/dex/sdk-go/dex"
	"gopkg.in/yaml.v3"
)

var (
	testConnection        = sdkgo.ConnectionRef{Provider: "mock", Name: "default"}
	testQueryRef          = sdkgo.OperationRef{ConnectorID: "mock-provider", OperationID: "getProfile"}
	testMutationRef       = sdkgo.OperationRef{ConnectorID: "mock-provider", OperationID: "grantCredit"}
	testQuerySucceeded    = sdkgo.BranchID("succeeded")
	testQueryFailed       = sdkgo.BranchID("failed")
	testQueryDefect       = sdkgo.BranchID("defect")
	testMutationSucceeded = sdkgo.BranchID("succeeded")
	testMutationRejected  = sdkgo.BranchID("rejected")
	testMutationUncertain = sdkgo.BranchID("uncertain")
	testMutationDefect    = sdkgo.BranchID("defect")
)

func queryDefinition(ref sdkgo.OperationRef) sdkgo.QueryDefinition {
	return sdkgo.QueryDefinition{
		Operation: ref,
		Branches: []sdkgo.BranchDefinition{
			{ID: testQuerySucceeded, Description: "succeeded"},
			{ID: testQueryFailed, Description: "failed"},
			{ID: testQueryDefect, Description: "defect"},
		},
		DefectBranch: testQueryDefect,
	}
}

func mutationDefinition(ref sdkgo.OperationRef) sdkgo.MutationDefinition {
	return sdkgo.MutationDefinition{
		Operation: ref,
		Branches: []sdkgo.BranchDefinition{
			{ID: testMutationSucceeded, Description: "succeeded"},
			{ID: testMutationRejected, Description: "rejected"},
			{ID: testMutationUncertain, Description: "uncertain"},
			{ID: testMutationDefect, Description: "defect"},
		},
		DefectBranch: testMutationDefect, UncertainBranch: testMutationUncertain,
	}
}

type queryOperation struct {
	call    sdkgo.Call
	attempt sdkgo.QueryAttempt[string]
	useSet  bool
}

func (*queryOperation) Definition() sdkgo.QueryDefinition {
	return queryDefinition(testQueryRef)
}

func (operation *queryOperation) Invoke(call sdkgo.Call, input string) sdkgo.QueryAttempt[string] {
	operation.call = call
	if operation.useSet {
		return operation.attempt
	}
	return sdkgo.NewQueryBranch(testQuerySucceeded, input, nil, sdkgo.Receipt{})
}

type mutationOperation struct {
	call       sdkgo.Call
	attempt    sdkgo.MutationAttempt[string]
	useSet     bool
	derivedKey sdkgo.IdempotencyKey
}

func (*mutationOperation) Definition() sdkgo.MutationDefinition {
	return mutationDefinition(testMutationRef)
}

func (operation *mutationOperation) IdempotencyKey(sdkgo.CallID, string) sdkgo.IdempotencyKey {
	return operation.derivedKey
}

func (operation *mutationOperation) Invoke(call sdkgo.Call, _ string) sdkgo.MutationAttempt[string] {
	operation.call = call
	if operation.useSet {
		return operation.attempt
	}
	return sdkgo.NewMutationBranch(testMutationSucceeded, "ok", nil, sdkgo.Receipt{})
}

var (
	_ sdkgo.Query[string, string]    = (*queryOperation)(nil)
	_ sdkgo.Mutation[string, string] = (*mutationOperation)(nil)
)

func TestCallIDUsesStableDexAndOperationIdentity(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "step-execution-1")
	operation := &queryOperation{}
	first, err := sdkgo.RunQuery(ctx, operation, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, testQuerySucceeded, first.Branch)
	firstID := first.Receipt.CallID
	require.Equal(t, sdkgo.CallID("8fe78557-483c-5b53-ace7-7d6a13eb30db"), firstID)

	ctx.Run = "run-2"
	ctx.AttemptNo = 9
	second, err := sdkgo.RunQuery(ctx, operation, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, firstID, second.Receipt.CallID)
	otherConnection, err := sdkgo.RunQuery(ctx, operation, sdkgo.ConnectionRef{Provider: "mock", Name: "other"}, "profile")
	require.NoError(t, err)
	require.Equal(t, firstID, otherConnection.Receipt.CallID)

	for _, changed := range []*testsupport.DexContext{
		testsupport.NewDexContext("customer/43", "step-execution-1"),
		testsupport.NewDexContext("customer/42", "step-execution-2"),
	} {
		result, runErr := sdkgo.RunQuery(changed, operation, testConnection, "profile")
		require.NoError(t, runErr)
		require.NotEqual(t, firstID, result.Receipt.CallID)
	}

	otherOperation := &queryOperationWithRef{ref: sdkgo.OperationRef{ConnectorID: "mock-provider", OperationID: "getAccount"}}
	result, err := sdkgo.RunQuery(ctx, otherOperation, testConnection, "profile")
	require.NoError(t, err)
	require.NotEqual(t, firstID, result.Receipt.CallID)

	otherConnector := &queryOperationWithRef{ref: sdkgo.OperationRef{ConnectorID: "other-provider", OperationID: "getProfile"}}
	result, err = sdkgo.RunQuery(ctx, otherConnector, testConnection, "profile")
	require.NoError(t, err)
	require.NotEqual(t, firstID, result.Receipt.CallID)

	left, err := sdkgo.RunQuery(testsupport.NewDexContext("a", "bc"), operation, testConnection, "profile")
	require.NoError(t, err)
	right, err := sdkgo.RunQuery(testsupport.NewDexContext("ab", "c"), operation, testConnection, "profile")
	require.NoError(t, err)
	require.NotEqual(t, left.Receipt.CallID, right.Receipt.CallID)
}

type queryOperationWithRef struct{ ref sdkgo.OperationRef }

func (operation *queryOperationWithRef) Definition() sdkgo.QueryDefinition {
	return queryDefinition(operation.ref)
}

func (*queryOperationWithRef) Invoke(sdkgo.Call, string) sdkgo.QueryAttempt[string] {
	return sdkgo.NewQueryBranch(testQuerySucceeded, "ok", nil, sdkgo.Receipt{})
}

func TestMutationDerivesStableProviderIdempotencyKey(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "step-execution-1")
	operation := &mutationOperation{derivedKey: "provider-prefix-stable"}
	first, err := sdkgo.RunMutation(ctx, operation, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, testMutationSucceeded, first.Branch)
	require.Equal(t, operation.call.ID, first.Receipt.CallID)
	require.Equal(t, sdkgo.IdempotencyKey("provider-prefix-stable"), operation.call.IdempotencyKey)
	require.Equal(t, operation.call.IdempotencyKey, first.Receipt.IdempotencyKey)

	firstCallID := operation.call.ID
	ctx.Run = "another-run"
	ctx.AttemptNo = 7
	second, err := sdkgo.RunMutation(ctx, operation, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, firstCallID, operation.call.ID)
	require.Equal(t, first.Receipt.IdempotencyKey, second.Receipt.IdempotencyKey)

	fallback := &mutationOperation{}
	result, err := sdkgo.RunMutation(ctx, fallback, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, sdkgo.IdempotencyKey(fallback.call.ID), fallback.call.IdempotencyKey)
	require.Equal(t, fallback.call.IdempotencyKey, result.Receipt.IdempotencyKey)

	otherStep := testsupport.NewDexContext("customer/42", "step-execution-2")
	result, err = sdkgo.RunMutation(otherStep, fallback, testConnection, "credits")
	require.NoError(t, err)
	require.NotEqual(t, firstCallID, result.Receipt.CallID)

	invalid := &mutationOperation{derivedKey: " "}
	result, err = sdkgo.RunMutation(ctx, invalid, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, testMutationDefect, result.Branch)
	require.Equal(t, sdkgo.FailureLocalDefect, result.Failure.Kind)
	require.Empty(t, result.Receipt.IdempotencyKey)
	require.Equal(t, sdkgo.Call{}, invalid.call)
}

func TestRPCContextFailsBeforeOperationInvocation(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "")
	query := &queryOperation{}
	queryResult, err := sdkgo.RunQuery(ctx, query, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, testQueryDefect, queryResult.Branch)
	require.Equal(t, sdkgo.FailureLocalDefect, queryResult.Failure.Kind)
	require.Contains(t, queryResult.Failure.Message, "RPC invocation is not allowed")
	require.Equal(t, sdkgo.Call{}, query.call)

	mutation := &mutationOperation{}
	mutationResult, err := sdkgo.RunMutation(ctx, mutation, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, testMutationDefect, mutationResult.Branch)
	require.Equal(t, sdkgo.Call{}, mutation.call)
}

func TestCredentialProviderReceivesCompleteCallMetadata(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "step-execution-1")
	provider := &capturingCredentialProvider{}
	operation := credentialQuery{provider: provider}
	result, err := sdkgo.RunQuery(ctx, operation, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, testQuerySucceeded, result.Branch)
	require.Same(t, ctx, provider.call.Context)
	require.Equal(t, testConnection, provider.call.Connection)
	require.Equal(t, testQueryRef, provider.call.Operation)
	require.NotEmpty(t, provider.call.ID)
	require.Empty(t, provider.call.IdempotencyKey)
	require.NotContains(t, fmt.Sprintf("%+v", provider.call), "super-secret")
}

type testCredentials struct{ APIKey sdkgo.SecretString }

type capturingCredentialProvider struct{ call sdkgo.Call }

func (provider *capturingCredentialProvider) Resolve(call sdkgo.Call) (testCredentials, error) {
	provider.call = call
	return testCredentials{APIKey: sdkgo.NewSecretString("super-secret")}, nil
}

type credentialQuery struct {
	provider sdkgo.CredentialProvider[testCredentials]
}

func (credentialQuery) Definition() sdkgo.QueryDefinition {
	return queryDefinition(testQueryRef)
}

func (operation credentialQuery) Invoke(call sdkgo.Call, _ string) sdkgo.QueryAttempt[string] {
	if _, err := operation.provider.Resolve(call); err != nil {
		providerFailure := failure(sdkgo.FailureAuthentication, "credentials unavailable")
		return sdkgo.NewQueryBranch(testQueryFailed, "", &providerFailure, sdkgo.Receipt{})
	}
	return sdkgo.NewQueryBranch(testQuerySucceeded, "ok", nil, sdkgo.Receipt{})
}

func TestSecretStringCannotSerializeOrFormatSecret(t *testing.T) {
	secret := sdkgo.NewSecretString("super-secret")
	require.NotContains(t, fmt.Sprintf("%v", secret), "super-secret")
	require.NotContains(t, fmt.Sprintf("%#v", secret), "super-secret")
	_, err := json.Marshal(secret)
	require.ErrorContains(t, err, "cannot be serialized")
	_, err = secret.MarshalText()
	require.ErrorContains(t, err, "cannot be serialized")
	_, err = yaml.Marshal(secret)
	require.ErrorContains(t, err, "cannot be serialized")
}

func TestStrictAttemptsAndRetryMapping(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "step-execution-1")

	queryFailureValue := failure(sdkgo.FailureNotFound, "missing")
	queryFailure := &queryOperation{useSet: true, attempt: sdkgo.NewQueryBranch(testQueryFailed, "", &queryFailureValue, sdkgo.Receipt{})}
	result, err := sdkgo.RunQuery(ctx, queryFailure, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, testQueryFailed, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)

	queryRetry := &queryOperation{useSet: true, attempt: sdkgo.NewQueryRetry[string](failure(sdkgo.FailureNotFound, "eventually consistent"), 3*time.Second)}
	_, err = sdkgo.RunQuery(ctx, queryRetry, testConnection, "profile")
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 3*time.Second, retryAfter.After)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureNotFound, retry.Failure.Kind)

	queryDefaultBackoff := &queryOperation{useSet: true, attempt: sdkgo.NewQueryRetry[string](failure(sdkgo.FailureAvailability, "unavailable"), 0)}
	_, err = sdkgo.RunQuery(ctx, queryDefaultBackoff, testConnection, "profile")
	require.ErrorAs(t, err, &retry)
	require.False(t, errors.As(err, &retryAfter))
}

func TestMutationAttemptsAreDistinctResults(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "step-execution-1")

	succeeded, err := sdkgo.RunMutation(ctx, &mutationOperation{}, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, testMutationSucceeded, succeeded.Branch)

	failedValue := failure(sdkgo.FailureAuthorization, "denied")
	failedAttempt := sdkgo.NewMutationBranch(testMutationRejected, "", &failedValue, sdkgo.Receipt{})
	failed, err := sdkgo.RunMutation(ctx, &mutationOperation{useSet: true, attempt: failedAttempt}, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, testMutationRejected, failed.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, failed.Failure.Kind)

	unknownAttempt := sdkgo.NewMutationUncertain("provider-response-id", failure(sdkgo.FailureTransport, "response lost"), sdkgo.Receipt{})
	unknown, err := sdkgo.RunMutation(ctx, &mutationOperation{useSet: true, attempt: unknownAttempt}, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, testMutationUncertain, unknown.Branch)
	require.Equal(t, "provider-response-id", unknown.Value)

	_, err = sdkgo.RunMutation(ctx, &mutationOperation{useSet: true, attempt: sdkgo.NewMutationRetry[string](failure(sdkgo.FailureRateLimit, "try later"), 0)}, testConnection, "credits")
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}

func TestZeroAttemptsFailClosed(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "step-execution-1")

	queryResult, err := sdkgo.RunQuery(ctx, invalidQuery{}, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, testQueryDefect, queryResult.Branch)
	require.Equal(t, sdkgo.FailureLocalDefect, queryResult.Failure.Kind)

	mutationResult, err := sdkgo.RunMutation(ctx, invalidMutation{}, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, testMutationUncertain, mutationResult.Branch)
	require.Equal(t, sdkgo.FailureLocalDefect, mutationResult.Failure.Kind)

	queryResult, err = sdkgo.RunQuery(ctx, &queryOperation{
		useSet: true, attempt: sdkgo.NewQueryBranch("unknown", "", nil, sdkgo.Receipt{}),
	}, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, testQueryDefect, queryResult.Branch)

	mutationResult, err = sdkgo.RunMutation(ctx, &mutationOperation{
		useSet: true, attempt: sdkgo.NewMutationBranch("unknown", "", nil, sdkgo.Receipt{}),
	}, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, testMutationUncertain, mutationResult.Branch)

	invalidRetry := sdkgo.Failure{Kind: "MADE_UP", Provider: "mock", Operation: "query", Message: "invalid"}
	queryResult, err = sdkgo.RunQuery(ctx, &queryOperation{
		useSet: true, attempt: sdkgo.NewQueryRetry[string](invalidRetry, 0),
	}, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, testQueryDefect, queryResult.Branch)

	mutationResult, err = sdkgo.RunMutation(ctx, &mutationOperation{
		useSet: true, attempt: sdkgo.NewMutationRetry[string](invalidRetry, 0),
	}, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, testMutationUncertain, mutationResult.Branch)
}

type invalidQuery struct{}

func (invalidQuery) Definition() sdkgo.QueryDefinition {
	return queryDefinition(testQueryRef)
}

func (invalidQuery) Invoke(sdkgo.Call, string) sdkgo.QueryAttempt[string] {
	return sdkgo.QueryAttempt[string]{}
}

type invalidMutation struct{}

func (invalidMutation) Definition() sdkgo.MutationDefinition {
	return mutationDefinition(testMutationRef)
}

func (invalidMutation) IdempotencyKey(sdkgo.CallID, string) sdkgo.IdempotencyKey { return "" }

func (invalidMutation) Invoke(sdkgo.Call, string) sdkgo.MutationAttempt[string] {
	return sdkgo.MutationAttempt[string]{}
}

func TestProgressMethodsAreNoOpWithoutStreams(t *testing.T) {
	operation := &queryOperation{}
	result, err := sdkgo.RunQuery(testsupport.NewDexContext("customer/42", "step-execution-1"), operation, testConnection, "profile")
	require.NoError(t, err)
	require.Equal(t, testQuerySucceeded, result.Branch)
	require.False(t, operation.call.HasProgressStream())
	require.False(t, operation.call.HasTextStream())
	require.NoError(t, operation.call.ReportProgress(sdkgo.Progress{Phase: "started"}))
	require.NoError(t, operation.call.WriteText("hello"))
}

func failure(kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "mock-provider", Operation: "getProfile", Message: message}
}
