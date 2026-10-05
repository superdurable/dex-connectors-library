// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/snowflake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type submitTarget struct {
	dex.StepDefaultsNoWaitFor[snowflake.SubmitStatementResult]
}

func (submitTarget) Execute(dex.Context, snowflake.SubmitStatementResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type resultTarget struct {
	dex.StepDefaultsNoWaitFor[snowflake.GetStatementResultResult]
}

func (resultTarget) Execute(dex.Context, snowflake.GetStatementResultResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func newFactoryConnection(t *testing.T, name string) snowflake.Connection {
	t.Helper()
	client, err := snowflake.New(snowflake.Config{AccountIdentifier: "myorg-analytics"}, sdkgo.StaticCredentialProvider[snowflake.Credentials]{})
	require.NoError(t, err)
	connection, err := snowflake.NewConnection(client, sdkgo.ConnectionRef{Provider: "snowflake", Name: name})
	require.NoError(t, err)
	return connection
}

func TestDefinitionsDeclareOneHappyPathAndAsyncDefaults(t *testing.T) {
	requireOneRequiredBranch(t, snowflake.SubmitStatementDefinition.Branches, snowflake.SubmitStatementBranchSubmitted)
	requireOneRequiredBranch(t, snowflake.GetStatementResultDefinition.Branches, snowflake.GetStatementResultBranchCompleted)
	requireOneRequiredBranch(t, snowflake.CancelStatementDefinition.Branches, snowflake.CancelStatementBranchCanceled)
	for _, definition := range []sdkgo.MutationDefinition{snowflake.SubmitStatementDefinition, snowflake.CancelStatementDefinition} {
		for _, branch := range definition.Branches {
			require.NotEqual(t, sdkgo.UncertainBranchID, branch.ID, "requestId makes a lost response safe to retry")
		}
	}
	for _, defaults := range []sdkgo.StepDefaults{
		snowflake.SubmitStatementDefinition.StepDefaults, snowflake.GetStatementResultDefinition.StepDefaults, snowflake.CancelStatementDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability, "no call waits for the statement, so each returns well inside seven seconds")
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout)
		require.Equal(t, int32(5), defaults.ExecuteRetry.MaximumAttempts)
		require.Equal(t, 2*time.Minute, defaults.ExecuteRetry.TotalDuration)
	}
}

func requireOneRequiredBranch(t *testing.T, branches []sdkgo.BranchDefinition, happyPath sdkgo.BranchID) {
	t.Helper()
	var required []sdkgo.BranchID
	hasDefect := false
	for _, branch := range branches {
		if !branch.Optional {
			required = append(required, branch.ID)
		}
		hasDefect = hasDefect || branch.ID == sdkgo.DefectBranchID
	}
	require.Equal(t, []sdkgo.BranchID{happyPath}, required)
	require.True(t, hasDefect)
}

func TestGeneratedFactoriesRequireTheHappyPathAndTheStaticConnection(t *testing.T) {
	connection := newFactoryConnection(t, "warehouse")
	annotations := sdkgo.StepAnnotations{GroupID: "snowflake", GroupLabel: "Snowflake", Explanation: "Run a statement."}
	submitStep := snowflake.NewSubmitStatementStep(snowflake.SubmitStatementStepConfig[string]{
		StepType: "SubmitReport", ConnectionName: "warehouse", Connection: connection, Annotations: annotations,
		MapToOperationInput: func(statement string) snowflake.SubmitStatementInput {
			return snowflake.SubmitStatementInput{Statement: statement}
		},
		Submitted: sdkgo.GoTo(submitTarget{}),
	})
	require.Equal(t, "SubmitReport", submitStep.GetStepType())
	require.Equal(t, dex.StepDurabilityAsync, submitStep.GetStepOptions().ExecuteDurability)

	require.Panics(t, func() {
		snowflake.NewGetStatementResultStep(snowflake.GetStatementResultStepConfig[string]{
			StepType: "ReadReport", ConnectionName: "warehouse", Connection: connection, Annotations: annotations,
			MapToOperationInput: func(handle string) snowflake.GetStatementResultInput {
				return snowflake.GetStatementResultInput{StatementHandle: handle}
			},
			Running: sdkgo.GoTo(resultTarget{}),
		})
	}, "completed is required")
	require.Panics(t, func() {
		snowflake.NewSubmitStatementStep(snowflake.SubmitStatementStepConfig[string]{
			StepType: "WrongConnection", ConnectionName: "another", Connection: connection, Annotations: annotations,
			MapToOperationInput: func(statement string) snowflake.SubmitStatementInput {
				return snowflake.SubmitStatementInput{Statement: statement}
			},
			Submitted: sdkgo.GoTo(submitTarget{}),
		})
	}, "ConnectionName must match the runtime connection")
}

func TestConnectionAndCredentialsNeverSerializeSecrets(t *testing.T) {
	connection := newFactoryConnection(t, "warehouse")
	_, err := connection.MarshalJSON()
	require.Error(t, err)
	require.Equal(t, "snowflake.Connection{[REDACTED]}", connection.String())
	credentials := tokenCredentials()
	require.NotContains(t, credentials.ProgrammaticAccessToken.String(), testAccessToken)
	require.EqualError(t, snowflake.Credentials{AuthMethodID: snowflake.KeyPairAuthMethodID, User: "DEX"}.Validate(), "credential private_key is required")
	require.EqualError(t, snowflake.Credentials{}.Validate(), "credential auth_method is invalid")
}
