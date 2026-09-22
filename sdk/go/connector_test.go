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
)

var (
	testConnection  = connector.ConnectionRef{Provider: "mock", Name: "default"}
	testQueryRef    = connector.OperationRef{ConnectorID: "mock-provider", OperationID: "getProfile"}
	testMutationRef = connector.OperationRef{ConnectorID: "mock-provider", OperationID: "grantCredit"}
)

type queryOperation struct {
	call connector.Call
	err  error
}

func (*queryOperation) Definition() connector.QueryDefinition {
	return connector.QueryDefinition{Operation: testQueryRef}
}

func (operation *queryOperation) Invoke(call connector.Call, input string) (connector.QueryResult[string], error) {
	operation.call = call
	return connector.QueryResult[string]{Value: input}, operation.err
}

type mutationOperation struct{ err error }

func (mutationOperation) Definition() connector.MutationDefinition {
	return connector.MutationDefinition{Operation: testMutationRef}
}

func (operation mutationOperation) Invoke(connector.Call, string) (connector.MutationResult[string], error) {
	if operation.err != nil {
		return connector.MutationResult[string]{}, operation.err
	}
	return connector.MutationResult[string]{Outcome: connector.MutationSucceeded, Value: "ok"}, nil
}

func TestCallIDUsesStableDexAndOperationIdentity(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "step-execution-1")
	operation := &queryOperation{}
	first, err := connector.RunQuery(ctx, operation, testConnection, "profile")
	require.NoError(t, err)
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

	changes := []*testsupport.DexContext{
		testsupport.NewDexContext("customer/43", "step-execution-1"),
		testsupport.NewDexContext("customer/42", "step-execution-2"),
	}
	for _, changed := range changes {
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

func (*queryOperationWithRef) Invoke(connector.Call, string) (connector.QueryResult[string], error) {
	return connector.QueryResult[string]{Value: "ok"}, nil
}

func TestRPCContextCannotInvokeConnectorOperation(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "")
	operation := &queryOperation{}
	_, err := connector.RunQuery(ctx, operation, testConnection, "profile")
	require.ErrorContains(t, err, "RPC invocation is not allowed")
	require.Equal(t, connector.Call{}, operation.call)

	_, err = connector.RunMutation(ctx, mutationOperation{}, testConnection, "credits")
	require.ErrorContains(t, err, "RPC invocation is not allowed")
}

func TestCredentialProviderReceivesCompleteCallMetadata(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "step-execution-1")
	provider := &capturingCredentialProvider{}
	operation := credentialQuery{provider: provider}
	_, err := connector.RunQuery(ctx, operation, testConnection, "profile")
	require.NoError(t, err)
	require.Same(t, ctx, provider.call.Context)
	require.Equal(t, testConnection, provider.call.Connection)
	require.Equal(t, testQueryRef, provider.call.Operation)
	require.NotEmpty(t, provider.call.ID)
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

func (operation credentialQuery) Invoke(call connector.Call, _ string) (connector.QueryResult[string], error) {
	_, err := operation.provider.Resolve(call)
	return connector.QueryResult[string]{Value: "ok"}, err
}

func TestCredentialCannotSerializeOrFormatSecret(t *testing.T) {
	credential := connector.NewCredential(map[string]string{"api_key": "super-secret"})
	require.NotContains(t, fmt.Sprintf("%v", credential), "super-secret")
	require.NotContains(t, fmt.Sprintf("%#v", credential), "super-secret")
	_, err := json.Marshal(credential)
	require.ErrorContains(t, err, "cannot be serialized")
}

func TestDexRetryAcceptsOnlySafeRetryErrors(t *testing.T) {
	rateLimit := connector.NewError(connector.ErrorRateLimit, "mock", "query", "try later", nil).WithRetryAfter(3 * time.Second)
	retry, ok := connector.DexRetry(rateLimit, time.Second)
	require.True(t, ok)
	require.Equal(t, 3*time.Second, retry.After)
	require.ErrorIs(t, retry, rateLimit)

	availability := connector.NewError(connector.ErrorRetryableAvailability, "mock", "query", "unavailable", nil)
	retry, ok = connector.DexRetry(availability, 2*time.Second)
	require.True(t, ok)
	require.Equal(t, 2*time.Second, retry.After)

	unknown := connector.NewError(connector.ErrorUnknownMutation, "mock", "mutation", "unknown", nil)
	_, ok = connector.DexRetry(unknown, time.Second)
	require.False(t, ok)
	_, ok = connector.DexRetry(errors.New("ordinary error"), time.Second)
	require.False(t, ok)
}

func TestMutationErrorsBecomeDistinctResults(t *testing.T) {
	ctx := testsupport.NewDexContext("customer/42", "step-execution-1")
	succeeded, err := connector.RunMutation(ctx, mutationOperation{}, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, connector.MutationSucceeded, succeeded.Outcome)

	failed, err := connector.RunMutation(ctx, mutationOperation{err: connector.NewError(
		connector.ErrorAuthorization, "mock-provider", "grantCredit", "denied", nil,
	)}, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, connector.MutationFailed, failed.Outcome)
	require.Equal(t, connector.ErrorAuthorization, failed.Failure.Kind)

	unknown, err := connector.RunMutation(ctx, mutationOperation{err: connector.NewError(
		connector.ErrorUnknownMutation, "mock-provider", "grantCredit", "response lost", nil,
	)}, testConnection, "credits")
	require.NoError(t, err)
	require.Equal(t, connector.MutationUnknown, unknown.Outcome)
	require.Equal(t, connector.ErrorUnknownMutation, unknown.Failure.Kind)

	_, err = connector.RunMutation(ctx, mutationOperation{err: connector.NewError(
		connector.ErrorRateLimit, "mock-provider", "grantCredit", "try later", nil,
	)}, testConnection, "credits")
	require.True(t, connector.IsKind(err, connector.ErrorRateLimit))
}

func TestTypedErrorRetainsCauseAndClassification(t *testing.T) {
	cause := errors.New("dial failed")
	err := connector.NewError(connector.ErrorRetryableAvailability, "mock", "query", "provider unavailable", cause)
	require.True(t, connector.IsRetryable(err))
	require.ErrorIs(t, err, cause)
	require.NotContains(t, err.Error(), cause.Error())
}
