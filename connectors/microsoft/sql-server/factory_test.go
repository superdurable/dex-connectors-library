// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/sql-server"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type queryRowsTarget struct {
	dex.StepDefaultsNoWaitFor[sqlserver.QueryRowsResult]
}

func (queryRowsTarget) Execute(dex.Context, sqlserver.QueryRowsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type executeStatementTarget struct {
	dex.StepDefaultsNoWaitFor[sqlserver.ExecuteStatementResult]
}

func (executeStatementTarget) Execute(dex.Context, sqlserver.ExecuteStatementResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func newFactoryConnection(t *testing.T, name string) sqlserver.Connection {
	t.Helper()
	client, err := sqlserver.New(sqlserver.Config{Host: "db.example.com", Database: "app", User: "dex_app"}, sdkgo.StaticCredentialProvider[sqlserver.Credentials]{})
	require.NoError(t, err)
	connection, err := sqlserver.NewConnection(client, sdkgo.ConnectionRef{Provider: "microsoft-sql-server", Name: name})
	require.NoError(t, err)
	return connection
}

func TestDefinitionsDeclareOneHappyPathAndTheStandardBranches(t *testing.T) {
	require.Equal(t, sdkgo.OperationRef{ConnectorID: "microsoft-sql-server", OperationID: "query"}, sqlserver.QueryRowsDefinition.Operation)
	require.Equal(t, sdkgo.OperationRef{ConnectorID: "microsoft-sql-server", OperationID: "execute"}, sqlserver.ExecuteStatementDefinition.Operation)
	requireOneRequiredBranch(t, sqlserver.QueryRowsDefinition.Branches, sqlserver.QueryRowsBranchCompleted)
	requireOneRequiredBranch(t, sqlserver.ExecuteStatementDefinition.Branches, sqlserver.ExecuteStatementBranchCompleted)
	require.Equal(t, sdkgo.UncertainBranchID, sqlserver.ExecuteStatementBranchUncertain)
	for _, defaults := range []sdkgo.StepDefaults{sqlserver.QueryRowsDefinition.StepDefaults, sqlserver.ExecuteStatementDefinition.StepDefaults} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability)
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout)
		require.Equal(t, int32(5), defaults.ExecuteRetry.MaximumAttempts)
		require.Equal(t, 2*time.Minute, defaults.ExecuteRetry.TotalDuration)
	}
	config := sqlserver.DefaultConfig()
	require.Less(t, config.ConnectTimeout+config.StatementTimeout+5*time.Second, sqlserver.ExecuteStatementDefinition.StepDefaults.ExecuteMethodTimeout,
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
	query := sqlserver.NewQueryRowsStep(sqlserver.QueryRowsStepConfig[string]{
		StepType: "FindRows", ConnectionName: "ledger", Connection: connection,
		Annotations:         sdkgo.StepAnnotations{GroupID: "sql-server", GroupLabel: "SQL Server", Explanation: "Find rows."},
		MapToOperationInput: func(value string) sqlserver.QueryRowsInput { return sqlserver.QueryRowsInput{Statement: value} },
		Completed:           sdkgo.GoTo(queryRowsTarget{}),
	})
	require.Equal(t, "FindRows", query.GetStepType())
	require.Equal(t, dex.StepDurabilityAsync, query.GetStepOptions().ExecuteDurability)

	write := sqlserver.NewExecuteStatementStep(sqlserver.ExecuteStatementStepConfig[string]{
		StepType: "WriteRow", Connection: connection,
		Annotations: sdkgo.StepAnnotations{GroupID: "sql-server", GroupLabel: "SQL Server", Explanation: "Write a row."},
		MapToOperationInput: func(value string) sqlserver.ExecuteStatementInput {
			return sqlserver.ExecuteStatementInput{Statement: value}
		},
		Completed: sdkgo.GoTo(executeStatementTarget{}),
		Uncertain: sdkgo.GoTo(executeStatementTarget{}),
	})
	require.Equal(t, "WriteRow", write.GetStepType())

	require.Panics(t, func() {
		sqlserver.NewExecuteStatementStep(sqlserver.ExecuteStatementStepConfig[string]{
			StepType: "MissingCompleted", Connection: connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "sql-server", GroupLabel: "SQL Server", Explanation: "Write a row."},
			MapToOperationInput: func(value string) sqlserver.ExecuteStatementInput {
				return sqlserver.ExecuteStatementInput{Statement: value}
			},
		})
	}, "completed is required")
	require.Panics(t, func() {
		sqlserver.NewQueryRowsStep(sqlserver.QueryRowsStepConfig[string]{
			StepType: "WrongConnection", ConnectionName: "another", Connection: connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "sql-server", GroupLabel: "SQL Server", Explanation: "Find rows."},
			MapToOperationInput: func(value string) sqlserver.QueryRowsInput { return sqlserver.QueryRowsInput{Statement: value} },
			Completed:           sdkgo.GoTo(queryRowsTarget{}),
		})
	}, "ConnectionName must match the runtime connection")
}

func TestConnectionAndCredentialsNeverSerializeSecrets(t *testing.T) {
	connection := newFactoryConnection(t, "ledger")
	_, err := connection.MarshalJSON()
	require.Error(t, err)
	require.Equal(t, "sqlserver.Connection{[REDACTED]}", connection.String())
	credentials := sqlserver.Credentials{Password: sdkgo.NewSecretString("a-password")}
	require.NotContains(t, credentials.Password.String(), "a-password")
	require.EqualError(t, sqlserver.Credentials{}.Validate(), "credential password is required")
}
