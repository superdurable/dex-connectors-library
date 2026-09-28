// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package meta_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/meta"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type generatedTarget struct {
	dex.StepDefaultsNoWaitFor[meta.GenerateTextResult]
}

func (generatedTarget) Execute(dex.Context, meta.GenerateTextResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func TestGenerateTextFactoryAppliesTheStreamingGenerationBudget(t *testing.T) {
	step := meta.NewGenerateTextStep(meta.GenerateTextStepConfig[string]{
		StepType: "GenerateAnswer", ConnectionName: testConnection.Name, Annotations: metaAnnotations(),
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
		meta.NewGenerateTextStep(meta.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", Annotations: metaAnnotations(), Connection: connection, MapToOperationInput: userRequest,
		})
	}, "the generated branch is required")
	require.Panics(t, func() {
		meta.NewGenerateTextStep(meta.GenerateTextStepConfig[string]{
			StepType: "GenerateAnswer", ConnectionName: "another-connection", Annotations: metaAnnotations(),
			Connection: connection, MapToOperationInput: userRequest, Generated: sdkgo.GoTo(generatedTarget{}),
		})
	}, "a static connection name must match the runtime connection")
	require.Panics(t, func() {
		meta.NewGenerateTextStep(meta.GenerateTextStepConfig[string]{Connection: meta.Connection{}})
	}, "a zero Connection is rejected")
	for _, rendering := range []string{connection.String(), fmt.Sprintf("%#v", connection), fmt.Sprintf("%+v", connection)} {
		require.Equal(t, "meta.Connection{[REDACTED]}", rendering)
	}
}

func factoryConnection(t *testing.T) meta.Connection {
	t.Helper()
	client, err := meta.New(meta.Config{}, testCredentials())
	require.NoError(t, err)
	connection, err := meta.NewConnection(client, testConnection)
	require.NoError(t, err)
	return connection
}

func metaAnnotations() sdkgo.StepAnnotations {
	return sdkgo.StepAnnotations{GroupID: "meta", GroupLabel: "Meta", Explanation: "Generate an answer with Muse Spark."}
}
