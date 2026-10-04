// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postgresql_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/postgresql"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type queryRowsTarget struct {
	dex.StepDefaultsNoWaitFor[postgresql.QueryRowsResult]
}

func (queryRowsTarget) Execute(dex.Context, postgresql.QueryRowsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type executeStatementTarget struct {
	dex.StepDefaultsNoWaitFor[postgresql.ExecuteStatementResult]
}

func (executeStatementTarget) Execute(dex.Context, postgresql.ExecuteStatementResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func newFactoryConnection(t *testing.T, name string) postgresql.Connection {
	t.Helper()
	client, err := postgresql.New(postgresql.Config{Host: "db.example.com", Database: "app", User: "dex_app"}, sdkgo.StaticCredentialProvider[postgresql.Credentials]{})
	require.NoError(t, err)
	connection, err := postgresql.NewConnection(client, sdkgo.ConnectionRef{Provider: "postgresql", Name: name})
	require.NoError(t, err)
	return connection
}

func TestDefinitionsDeclareOneHappyPathAndTheStandardBranches(t *testing.T) {
	require.Equal(t, sdkgo.OperationRef{ConnectorID: "postgresql", OperationID: "query"}, postgresql.QueryRowsDefinition.Operation)
	require.Equal(t, sdkgo.OperationRef{ConnectorID: "postgresql", OperationID: "execute"}, postgresql.ExecuteStatementDefinition.Operation)
	requireOneRequiredBranch(t, postgresql.QueryRowsDefinition.Branches, postgresql.QueryRowsBranchCompleted)
	requireOneRequiredBranch(t, postgresql.ExecuteStatementDefinition.Branches, postgresql.ExecuteStatementBranchCompleted)
	require.Equal(t, sdkgo.UncertainBranchID, postgresql.ExecuteStatementBranchUncertain)
	for _, defaults := range []sdkgo.StepDefaults{postgresql.QueryRowsDefinition.StepDefaults, postgresql.ExecuteStatementDefinition.StepDefaults} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability)
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout)
		require.Equal(t, int32(5), defaults.ExecuteRetry.MaximumAttempts)
		require.Equal(t, 2*time.Minute, defaults.ExecuteRetry.TotalDuration)
	}
	config := postgresql.DefaultConfig()
	require.Less(t, config.ConnectTimeout+config.StatementTimeout+5*time.Second, postgresql.ExecuteStatementDefinition.StepDefaults.ExecuteMethodTimeout,
		"every call returns a classified attempt before Dex's Execute timeout")
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
	connection := newFactoryConnection(t, "ledger")
	query := postgresql.NewQueryRowsStep(postgresql.QueryRowsStepConfig[string]{
		StepType: "FindRows", ConnectionName: "ledger", Connection: connection,
		Annotations:         sdkgo.StepAnnotations{GroupID: "postgresql", GroupLabel: "PostgreSQL", Explanation: "Find rows."},
		MapToOperationInput: func(value string) postgresql.QueryRowsInput { return postgresql.QueryRowsInput{Statement: value} },
		Completed:           sdkgo.GoTo(queryRowsTarget{}),
	})
	require.Equal(t, "FindRows", query.GetStepType())
	require.Equal(t, dex.StepDurabilityAsync, query.GetStepOptions().ExecuteDurability)

	write := postgresql.NewExecuteStatementStep(postgresql.ExecuteStatementStepConfig[string]{
		StepType: "WriteRow", ConnectionName: "ledger", Connection: connection,
		Annotations: sdkgo.StepAnnotations{GroupID: "postgresql", GroupLabel: "PostgreSQL", Explanation: "Write a row."},
		MapToOperationInput: func(value string) postgresql.ExecuteStatementInput {
			return postgresql.ExecuteStatementInput{Statement: value}
		},
		Completed: sdkgo.GoTo(executeStatementTarget{}),
		Uncertain: sdkgo.GoTo(executeStatementTarget{}),
	})
	require.Equal(t, "WriteRow", write.GetStepType())

	require.Panics(t, func() {
		postgresql.NewExecuteStatementStep(postgresql.ExecuteStatementStepConfig[string]{
			StepType: "MissingCompleted", ConnectionName: "ledger", Connection: connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "postgresql", GroupLabel: "PostgreSQL", Explanation: "Write a row."},
			MapToOperationInput: func(value string) postgresql.ExecuteStatementInput {
				return postgresql.ExecuteStatementInput{Statement: value}
			},
		})
	}, "completed is required")
	require.Panics(t, func() {
		postgresql.NewQueryRowsStep(postgresql.QueryRowsStepConfig[string]{
			StepType: "WrongConnection", ConnectionName: "another", Connection: connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "postgresql", GroupLabel: "PostgreSQL", Explanation: "Find rows."},
			MapToOperationInput: func(value string) postgresql.QueryRowsInput { return postgresql.QueryRowsInput{Statement: value} },
			Completed:           sdkgo.GoTo(queryRowsTarget{}),
		})
	}, "ConnectionName must match the runtime connection")
}

func TestConnectionAndCredentialsNeverSerializeSecrets(t *testing.T) {
	connection := newFactoryConnection(t, "ledger")
	_, err := connection.MarshalJSON()
	require.Error(t, err)
	require.Equal(t, "postgresql.Connection{[REDACTED]}", connection.String())
	credentials := postgresql.Credentials{Password: sdkgo.NewSecretString("a-password")}
	require.NotContains(t, credentials.Password.String(), "a-password")
	require.EqualError(t, postgresql.Credentials{}.Validate(), "credential password is required")
}
