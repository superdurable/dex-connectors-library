// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mistral_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/mistral"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type generatedTarget struct {
	dex.StepDefaultsNoWaitFor[mistral.GenerateTextResult]
}

func (generatedTarget) Execute(dex.Context, mistral.GenerateTextResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func TestGenerateTextFactoryAppliesTheStreamingGenerationBudget(t *testing.T) {
	step := mistral.NewGenerateTextStep(mistral.GenerateTextStepConfig[string]{
		StepType: "GenerateAnswer", ConnectionName: testConnection.Name, Annotations: mistralAnnotations(),
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
		mistral.NewGenerateTextStep(mistral.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", Annotations: mistralAnnotations(), Connection: connection, MapToOperationInput: userRequest,
		})
	}, "the generated branch is required")
	require.Panics(t, func() {
		mistral.NewGenerateTextStep(mistral.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", ConnectionName: "another-connection", Annotations: mistralAnnotations(),
			Connection: connection, MapToOperationInput: userRequest, Generated: sdkgo.GoTo(generatedTarget{}),
		})
	}, "a static connection name must match the runtime connection")
	require.Panics(t, func() {
		mistral.NewGenerateTextStep(mistral.GenerateTextStepConfig[string]{Connection: mistral.Connection{}})
	}, "a zero Connection is rejected")
	for _, rendering := range []string{connection.String(), fmt.Sprintf("%#v", connection), fmt.Sprintf("%+v", connection)} {
		require.Equal(t, "mistral.Connection{[REDACTED]}", rendering)
	}
}

func factoryConnection(t *testing.T) mistral.Connection {
	t.Helper()
	client, err := mistral.New(mistral.Config{}, testCredentials())
	require.NoError(t, err)
	connection, err := mistral.NewConnection(client, testConnection)
	require.NoError(t, err)
	return connection
}

func mistralAnnotations() sdkgo.StepAnnotations {
	return sdkgo.StepAnnotations{GroupID: "mistral", GroupLabel: "Mistral AI", Explanation: "Generate an answer with a Mistral model."}
}
