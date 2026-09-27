// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package deepseek_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/deepseek"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type generatedTarget struct {
	dex.StepDefaultsNoWaitFor[deepseek.GenerateTextResult]
}

func (generatedTarget) Execute(dex.Context, deepseek.GenerateTextResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func TestGenerateTextFactoryAppliesTheQueuedStreamingBudget(t *testing.T) {
	step := deepseek.NewGenerateTextStep(deepseek.GenerateTextStepConfig[string]{
		StepType: "GenerateAnswer", ConnectionName: testConnection.Name, Annotations: deepSeekAnnotations(),
		Connection: factoryConnection(t), MapToOperationInput: userRequest, Generated: sdkgo.GoTo(generatedTarget{}),
	})
	options := step.GetStepOptions()
	require.Equal(t, dex.StepDurabilitySync, options.ExecuteDurability)
	require.Equal(t, 1200*time.Second, options.ExecuteMethodTimeout, "DeepSeek may queue a request for 10 minutes")
	require.Equal(t, 60*time.Second, options.HeartbeatTimeout)
	require.Equal(t, &dex.RetryPolicy{
		InitialInterval: 2 * time.Second, BackoffCoefficient: 2, MaximumInterval: time.Minute,
		MaximumAttempts: 4, TotalDuration: 30 * time.Minute,
	}, options.ExecuteRetry)
}

func TestGenerateTextFactoryFailsClosed(t *testing.T) {
	connection := factoryConnection(t)
	require.Panics(t, func() {
		deepseek.NewGenerateTextStep(deepseek.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", Annotations: deepSeekAnnotations(), Connection: connection, MapToOperationInput: userRequest,
		})
	}, "the generated branch is required")
	require.Panics(t, func() {
		deepseek.NewGenerateTextStep(deepseek.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", ConnectionName: "another-connection", Annotations: deepSeekAnnotations(),
			Connection: connection, MapToOperationInput: userRequest, Generated: sdkgo.GoTo(generatedTarget{}),
		})
	}, "a static connection name must match the runtime connection")
	require.Panics(t, func() {
		deepseek.NewGenerateTextStep(deepseek.GenerateTextStepConfig[string]{Connection: deepseek.Connection{}})
	}, "a zero Connection is rejected")
	for _, rendering := range []string{connection.String(), fmt.Sprintf("%#v", connection), fmt.Sprintf("%+v", connection)} {
		require.Equal(t, "deepseek.Connection{[REDACTED]}", rendering)
	}
}

func factoryConnection(t *testing.T) deepseek.Connection {
	t.Helper()
	client, err := deepseek.New(deepseek.Config{}, testCredentials())
	require.NoError(t, err)
	connection, err := deepseek.NewConnection(client, testConnection)
	require.NoError(t, err)
	return connection
}

func deepSeekAnnotations() sdkgo.StepAnnotations {
	return sdkgo.StepAnnotations{GroupID: "deepseek", GroupLabel: "DeepSeek", Explanation: "Generate an answer with a DeepSeek model."}
}
