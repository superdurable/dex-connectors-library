// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package claude_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	claude "github.com/superdurable/dex-connectors-library/connectors/anthropic"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type generatedTarget struct {
	dex.StepDefaultsNoWaitFor[claude.GenerateTextResult]
}

func (generatedTarget) Execute(dex.Context, claude.GenerateTextResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func TestGenerateTextFactoryAppliesTheStreamingGenerationBudget(t *testing.T) {
	step := claude.NewGenerateTextStep(claude.GenerateTextStepConfig[string]{
		StepType: "GenerateAnswer", ConnectionName: testConnection.Name, Annotations: claudeAnnotations(),
		Connection: factoryConnection(t), MapToOperationInput: userRequest, Generated: sdkgo.GoTo(generatedTarget{}),
	})
	options := step.GetStepOptions()
	require.Equal(t, dex.StepDurabilitySync, options.ExecuteDurability)
	require.Equal(t, 900*time.Second, options.ExecuteMethodTimeout)
	require.Equal(t, 60*time.Second, options.HeartbeatTimeout)
	require.Equal(t, &dex.RetryPolicy{
		InitialInterval: 2 * time.Second, BackoffCoefficient: 2, MaximumInterval: time.Minute,
		MaximumAttempts: 4, TotalDuration: 30 * time.Minute,
	}, options.ExecuteRetry)
}

func TestGenerateTextFactoryFailsClosed(t *testing.T) {
	connection := factoryConnection(t)
	require.Panics(t, func() {
		claude.NewGenerateTextStep(claude.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", Annotations: claudeAnnotations(), Connection: connection, MapToOperationInput: userRequest,
		})
	}, "the generated branch is required")
	require.Panics(t, func() {
		claude.NewGenerateTextStep(claude.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", ConnectionName: "another-connection", Annotations: claudeAnnotations(),
			Connection: connection, MapToOperationInput: userRequest, Generated: sdkgo.GoTo(generatedTarget{}),
		})
	}, "a static connection name must match the runtime connection")
	require.Panics(t, func() {
		claude.NewGenerateTextStep(claude.GenerateTextStepConfig[string]{Connection: claude.Connection{}})
	}, "a zero Connection is rejected")
	for _, rendering := range []string{connection.String(), fmt.Sprintf("%#v", connection), fmt.Sprintf("%+v", connection)} {
		require.Equal(t, "claude.Connection{[REDACTED]}", rendering)
	}
}

func factoryConnection(t *testing.T) claude.Connection {
	t.Helper()
	client, err := claude.New(claude.Config{}, testCredentials())
	require.NoError(t, err)
	connection, err := claude.NewConnection(client, testConnection)
	require.NoError(t, err)
	return connection
}

func claudeAnnotations() sdkgo.StepAnnotations {
	return sdkgo.StepAnnotations{GroupID: "claude", GroupLabel: "Claude", Explanation: "Generate an answer with Claude."}
}
