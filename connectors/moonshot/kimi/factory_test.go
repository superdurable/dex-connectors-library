// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package kimi_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/moonshot/kimi"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type generatedTarget struct {
	dex.StepDefaultsNoWaitFor[kimi.GenerateTextResult]
}

func (generatedTarget) Execute(dex.Context, kimi.GenerateTextResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func TestGenerateTextFactoryAppliesTheStreamingGenerationBudget(t *testing.T) {
	step := kimi.NewGenerateTextStep(kimi.GenerateTextStepConfig[string]{
		StepType: "GenerateAnswer", ConnectionName: testConnection.Name, Annotations: kimiAnnotations(),
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
		kimi.NewGenerateTextStep(kimi.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", Annotations: kimiAnnotations(), Connection: connection, MapToOperationInput: userRequest,
		})
	}, "the generated branch is required")
	require.Panics(t, func() {
		kimi.NewGenerateTextStep(kimi.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", ConnectionName: "another-connection", Annotations: kimiAnnotations(),
			Connection: connection, MapToOperationInput: userRequest, Generated: sdkgo.GoTo(generatedTarget{}),
		})
	}, "a static connection name must match the runtime connection")
	require.Panics(t, func() {
		kimi.NewGenerateTextStep(kimi.GenerateTextStepConfig[string]{Connection: kimi.Connection{}})
	}, "a zero Connection is rejected")
	for _, rendering := range []string{connection.String(), fmt.Sprintf("%#v", connection), fmt.Sprintf("%+v", connection)} {
		require.Equal(t, "kimi.Connection{[REDACTED]}", rendering)
	}
}

func factoryConnection(t *testing.T) kimi.Connection {
	t.Helper()
	client, err := kimi.New(kimi.Config{}, testCredentials())
	require.NoError(t, err)
	connection, err := kimi.NewConnection(client, testConnection)
	require.NoError(t, err)
	return connection
}

func kimiAnnotations() sdkgo.StepAnnotations {
	return sdkgo.StepAnnotations{GroupID: "kimi", GroupLabel: "Kimi", Explanation: "Generate an answer with a Kimi model."}
}
