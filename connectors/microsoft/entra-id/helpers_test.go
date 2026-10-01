// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	entraid "github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testGroupID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"
	// missingObjectID is a well-formed object ID the fake never creates.
	missingObjectID = "ffffffff-ffff-4fff-8fff-ffffffffffff"
)

var testConnection = sdkgo.ConnectionRef{Provider: "microsoft", Name: "microsoft-entra-id-test"}

func staticCredentials() sdkgo.StaticCredentialProvider[entraid.Credentials] {
	return sdkgo.StaticCredentialProvider[entraid.Credentials]{
		testConnection: {AuthMethodID: entraid.EntraAppOnlyAuthMethodID, AccessToken: sdkgo.NewSecretString(graphfake.AccessToken)},
	}
}

func newTestClient(t *testing.T, graph *graphfake.Server, config entraid.Config) *entraid.Client {
	t.Helper()
	client, err := entraid.New(config, staticCredentials(), entraid.WithLocalProviderURL(graph.URL))
	require.NoError(t, err)
	return client
}

// requireRetry asserts that an operation asked Dex to retry and returns the safe Failure.
func requireRetry(t *testing.T, err error) sdkgo.Failure {
	t.Helper()
	var retry *sdkgo.RetryError
	require.True(t, errors.As(err, &retry), "expected a connector Retry, got %v", err)
	require.NotContains(t, retry.Error(), graphfake.MessageSentinel)
	return retry.Failure
}

// requireSafeFailure asserts that a Failure carries no provider message text.
func requireSafeFailure(t *testing.T, failure *sdkgo.Failure) {
	t.Helper()
	require.NotNil(t, failure)
	require.NotContains(t, failure.Message, graphfake.MessageSentinel)
}

type testDexContext struct {
	context.Context
	step           string
	firstAttemptAt time.Time
}

func newTestDexContext(step string) *testDexContext {
	return &testDexContext{Context: context.Background(), step: step, firstAttemptAt: time.Now()}
}

// newLateTestDexContext is a Step whose first attempt started after the propagation window ended.
func newLateTestDexContext(step string) *testDexContext {
	return &testDexContext{Context: context.Background(), step: step, firstAttemptAt: time.Now().Add(-2 * time.Minute)}
}

func (*testDexContext) FlowID() string                                  { return "entra-id-flow" }
func (*testDexContext) RunID() string                                   { return "run" }
func (*testDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *testDexContext) StepExecutionID() string                 { return context.step }
func (*testDexContext) FromStepExecutionID() string                     { return "" }
func (*testDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (context *testDexContext) FirstAttemptAt() time.Time               { return context.firstAttemptAt }
func (*testDexContext) Attempt() int32                                  { return 1 }
func (*testDexContext) HasTimerFired() bool                             { return false }
func (*testDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*testDexContext) WaitForMethodFailed() bool                       { return false }
func (*testDexContext) RecordHeartbeat(any) error                       { return nil }
func (*testDexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*testDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*testDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*testDexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*testDexContext)(nil)
