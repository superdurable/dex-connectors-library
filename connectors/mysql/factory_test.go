// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mysql_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/mysql"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type queryRowsTarget struct {
	dex.StepDefaultsNoWaitFor[mysql.QueryRowsResult]
}

func (queryRowsTarget) Execute(dex.Context, mysql.QueryRowsResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

type executeStatementTarget struct {
	dex.StepDefaultsNoWaitFor[mysql.ExecuteStatementResult]
}

func (executeStatementTarget) Execute(dex.Context, mysql.ExecuteStatementResult) (*dex.StepDecision, error) {
	return dex.GracefulComplete(nil), nil
}

func newFactoryConnection(t *testing.T, name string) mysql.Connection {
	t.Helper()
	client, err := mysql.New(mysql.Config{Host: "db.example.com", Database: "app", User: "dex_app"}, sdkgo.StaticCredentialProvider[mysql.Credentials]{})
	require.NoError(t, err)
	connection, err := mysql.NewConnection(client, sdkgo.ConnectionRef{Provider: "mysql", Name: name})
	require.NoError(t, err)
	return connection
}

func TestDefinitionsDeclareOneHappyPathAndTheStandardBranches(t *testing.T) {
	require.Equal(t, sdkgo.OperationRef{ConnectorID: "mysql", OperationID: "query"}, mysql.QueryRowsDefinition.Operation)
	require.Equal(t, sdkgo.OperationRef{ConnectorID: "mysql", OperationID: "execute"}, mysql.ExecuteStatementDefinition.Operation)
	requireOneRequiredBranch(t, mysql.QueryRowsDefinition.Branches, mysql.QueryRowsBranchCompleted)
	requireOneRequiredBranch(t, mysql.ExecuteStatementDefinition.Branches, mysql.ExecuteStatementBranchCompleted)
	require.Equal(t, sdkgo.UncertainBranchID, mysql.ExecuteStatementBranchUncertain)
	for _, defaults := range []sdkgo.StepDefaults{mysql.QueryRowsDefinition.StepDefaults, mysql.ExecuteStatementDefinition.StepDefaults} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability)
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout)
		require.Equal(t, int32(5), defaults.ExecuteRetry.MaximumAttempts)
		require.Equal(t, 2*time.Minute, defaults.ExecuteRetry.TotalDuration)
	}
	config := mysql.DefaultConfig()
	require.Less(t, config.ConnectTimeout+config.StatementTimeout+5*time.Second, mysql.ExecuteStatementDefinition.StepDefaults.ExecuteMethodTimeout,
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
	query := mysql.NewQueryRowsStep(mysql.QueryRowsStepConfig[string]{
		StepType: "FindRows", ConnectionName: "ledger", Connection: connection,
		Annotations:         sdkgo.StepAnnotations{GroupID: "mysql", GroupLabel: "MySQL", Explanation: "Find rows."},
		MapToOperationInput: func(value string) mysql.QueryRowsInput { return mysql.QueryRowsInput{Statement: value} },
		Completed:           sdkgo.GoTo(queryRowsTarget{}),
	})
	require.Equal(t, "FindRows", query.GetStepType())
	require.Equal(t, dex.StepDurabilityAsync, query.GetStepOptions().ExecuteDurability)

	write := mysql.NewExecuteStatementStep(mysql.ExecuteStatementStepConfig[string]{
		StepType: "WriteRow", Connection: connection,
		Annotations: sdkgo.StepAnnotations{GroupID: "mysql", GroupLabel: "MySQL", Explanation: "Write a row."},
		MapToOperationInput: func(value string) mysql.ExecuteStatementInput {
			return mysql.ExecuteStatementInput{Statement: value}
		},
		Completed: sdkgo.GoTo(executeStatementTarget{}),
		Uncertain: sdkgo.GoTo(executeStatementTarget{}),
	})
	require.Equal(t, "WriteRow", write.GetStepType())

	require.Panics(t, func() {
		mysql.NewExecuteStatementStep(mysql.ExecuteStatementStepConfig[string]{
			StepType: "MissingCompleted", Connection: connection,
			Annotations: sdkgo.StepAnnotations{GroupID: "mysql", GroupLabel: "MySQL", Explanation: "Write a row."},
			MapToOperationInput: func(value string) mysql.ExecuteStatementInput {
				return mysql.ExecuteStatementInput{Statement: value}
			},
		})
	}, "completed is required")
	require.Panics(t, func() {
		mysql.NewQueryRowsStep(mysql.QueryRowsStepConfig[string]{
			StepType: "WrongConnection", ConnectionName: "another", Connection: connection,
			Annotations:         sdkgo.StepAnnotations{GroupID: "mysql", GroupLabel: "MySQL", Explanation: "Find rows."},
			MapToOperationInput: func(value string) mysql.QueryRowsInput { return mysql.QueryRowsInput{Statement: value} },
			Completed:           sdkgo.GoTo(queryRowsTarget{}),
		})
	}, "ConnectionName must match the runtime connection")
}

func TestConnectionAndCredentialsNeverSerializeSecrets(t *testing.T) {
	connection := newFactoryConnection(t, "ledger")
	_, err := connection.MarshalJSON()
	require.Error(t, err)
	require.Equal(t, "mysql.Connection{[REDACTED]}", connection.String())
	credentials := mysql.Credentials{Password: sdkgo.NewSecretString("a-password")}
	require.NotContains(t, credentials.Password.String(), "a-password")
	require.EqualError(t, mysql.Credentials{}.Validate(), "credential password is required")
}
