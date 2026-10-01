// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	entraid "github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type completeTarget[IN any] struct {
	dex.StepDefaultsNoWaitFor[IN]
}

func (completeTarget[IN]) Execute(dex.Context, IN) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection := newTestConnection(t)
	annotations := sdkgo.StepAnnotations{GroupID: "entra", GroupLabel: "Microsoft Entra ID", Explanation: "Call Microsoft Graph."}
	userKey := func(key string) string { return key }
	require.NotPanics(t, func() {
		entraid.NewGetUserStep(entraid.GetUserStepConfig[string]{StepType: "GetUser", Annotations: annotations, Connection: connection, ConnectionName: testConnection.Name,
			MapToOperationInput: func(key string) entraid.GetUserInput { return entraid.GetUserInput{UserKey: userKey(key)} },
			Found:               sdkgo.GoTo(completeTarget[entraid.GetUserResult]{})})
		entraid.NewListUsersStep(entraid.ListUsersStepConfig[string]{StepType: "ListUsers", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(filter string) entraid.ListUsersInput { return entraid.ListUsersInput{Filter: filter} },
			Listed:              sdkgo.GoTo(completeTarget[entraid.ListUsersResult]{})})
		entraid.NewCreateUserStep(entraid.CreateUserStepConfig[string]{StepType: "CreateUser", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(string) entraid.CreateUserInput { return validCreateUserInput() },
			Created:             sdkgo.GoTo(completeTarget[entraid.CreateUserResult]{})})
		entraid.NewDisableUserStep(entraid.DisableUserStepConfig[string]{StepType: "DisableUser", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(key string) entraid.DisableUserInput { return entraid.DisableUserInput{UserKey: key} },
			Disabled:            sdkgo.GoTo(completeTarget[entraid.DisableUserResult]{})})
		entraid.NewEnableUserStep(entraid.EnableUserStepConfig[string]{StepType: "EnableUser", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(key string) entraid.EnableUserInput { return entraid.EnableUserInput{UserKey: key} },
			Enabled:             sdkgo.GoTo(completeTarget[entraid.EnableUserResult]{})})
		entraid.NewRevokeSignInSessionsStep(entraid.RevokeSignInSessionsStepConfig[string]{StepType: "RevokeSessions", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(key string) entraid.RevokeSignInSessionsInput {
				return entraid.RevokeSignInSessionsInput{UserKey: key}
			},
			Revoked: sdkgo.GoTo(completeTarget[entraid.RevokeSignInSessionsResult]{})})
		entraid.NewAddUserToGroupStep(entraid.AddUserToGroupStepConfig[string]{StepType: "AddUserToGroup", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(id string) entraid.AddUserToGroupInput {
				return entraid.AddUserToGroupInput{GroupID: testGroupID, UserID: id}
			},
			Added: sdkgo.GoTo(completeTarget[entraid.AddUserToGroupResult]{})})
		entraid.NewRemoveUserFromGroupStep(entraid.RemoveUserFromGroupStepConfig[string]{StepType: "RemoveUserFromGroup", Annotations: annotations, Connection: connection,
			MapToOperationInput: func(id string) entraid.RemoveUserFromGroupInput {
				return entraid.RemoveUserFromGroupInput{GroupID: testGroupID, UserID: id}
			},
			Removed: sdkgo.GoTo(completeTarget[entraid.RemoveUserFromGroupResult]{})})
	})
	require.Panics(t, func() {
		entraid.NewCreateUserStep(entraid.CreateUserStepConfig[string]{StepType: "CreateUser", Connection: connection,
			MapToOperationInput: func(string) entraid.CreateUserInput { return validCreateUserInput() },
			AlreadyExists:       sdkgo.GoTo(completeTarget[entraid.CreateUserResult]{})})
	}, "created is the required branch")
	require.Panics(t, func() {
		entraid.NewGetUserStep(entraid.GetUserStepConfig[string]{StepType: "GetUser", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(key string) entraid.GetUserInput { return entraid.GetUserInput{UserKey: key} },
			Found:               sdkgo.GoTo(completeTarget[entraid.GetUserResult]{})})
	}, "the static ConnectionName must match the runtime connection")
}

func TestDefinitionsDeclareOptionalBranchesAndAsyncDurability(t *testing.T) {
	failureBranches := map[sdkgo.BranchID]bool{"providerRejected": true, "invalidResponse": true, "defect": true}
	expect := func(happyPath sdkgo.BranchID, extra ...sdkgo.BranchID) map[sdkgo.BranchID]bool {
		branches := map[sdkgo.BranchID]bool{happyPath: false}
		for id := range failureBranches {
			branches[id] = true
		}
		for _, id := range extra {
			branches[id] = true
		}
		return branches
	}
	require.Equal(t, expect("found", "notFound"), branchOptionality(entraid.GetUserDefinition.Branches))
	require.Equal(t, expect("listed"), branchOptionality(entraid.ListUsersDefinition.Branches))
	require.Equal(t, expect("created", "alreadyExists"), branchOptionality(entraid.CreateUserDefinition.Branches),
		"the creation-key read-back makes every dispatch safe to repeat, so there is no uncertain branch")
	require.Equal(t, expect("disabled", "notFound"), branchOptionality(entraid.DisableUserDefinition.Branches))
	require.Equal(t, expect("enabled", "notFound"), branchOptionality(entraid.EnableUserDefinition.Branches))
	require.Equal(t, expect("revoked", "notFound"), branchOptionality(entraid.RevokeSignInSessionsDefinition.Branches))
	require.Equal(t, expect("added", "notFound"), branchOptionality(entraid.AddUserToGroupDefinition.Branches))
	require.Equal(t, expect("removed", "notFound"), branchOptionality(entraid.RemoveUserFromGroupDefinition.Branches))

	propagationDefaults := map[string]sdkgo.StepDefaults{
		"createUser": entraid.CreateUserDefinition.StepDefaults, "disableUser": entraid.DisableUserDefinition.StepDefaults,
		"enableUser": entraid.EnableUserDefinition.StepDefaults, "addUserToGroup": entraid.AddUserToGroupDefinition.StepDefaults,
	}
	for name, defaults := range map[string]sdkgo.StepDefaults{
		"getUser": entraid.GetUserDefinition.StepDefaults, "listUsers": entraid.ListUsersDefinition.StepDefaults,
		"revokeSignInSessions": entraid.RevokeSignInSessionsDefinition.StepDefaults, "removeUserFromGroup": entraid.RemoveUserFromGroupDefinition.StepDefaults,
		"createUser": propagationDefaults["createUser"], "disableUser": propagationDefaults["disableUser"],
		"enableUser": propagationDefaults["enableUser"], "addUserToGroup": propagationDefaults["addUserToGroup"],
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability, "%s: a duplicate dispatch converges, so async is safe", name)
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout, name)
	}
	for name, defaults := range propagationDefaults {
		require.Equal(t, int32(8), defaults.ExecuteRetry.MaximumAttempts, name)
		require.Greater(t, retryScheduleBeforeLastAttempt(defaults.ExecuteRetry), time.Minute,
			"%s: the last attempt starts after the one-minute propagation window", name)
	}
}

func TestConnectionAndCredentialsNeverRevealSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, fmt.Sprintf("%v %#v", connection, connection), graphfake.AccessToken)
	credentials := entraid.Credentials{
		AuthMethodID: entraid.EntraAppOnlyAuthMethodID, TenantID: graphfake.TenantID, ClientID: graphfake.ClientID,
		ClientSecret: sdkgo.NewSecretString(graphfake.ClientSecret), AccessToken: sdkgo.NewSecretString(graphfake.AccessToken),
	}
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, graphfake.AccessToken)
	require.NotContains(t, rendered, graphfake.ClientSecret)
	_, err = json.Marshal(credentials)
	require.Error(t, err)
}

// retryScheduleBeforeLastAttempt sums the backoff intervals before the final attempt.
func retryScheduleBeforeLastAttempt(policy *dex.RetryPolicy) time.Duration {
	total, interval := time.Duration(0), policy.InitialInterval
	for attempt := int32(1); attempt < policy.MaximumAttempts; attempt++ {
		total += interval
		interval = min(time.Duration(float64(interval)*policy.BackoffCoefficient), policy.MaximumInterval)
	}
	return total
}

func branchOptionality(definitions []sdkgo.BranchDefinition) map[sdkgo.BranchID]bool {
	branches := map[sdkgo.BranchID]bool{}
	for _, branch := range definitions {
		branches[branch.ID] = branch.Optional
	}
	return branches
}

func newTestConnection(t *testing.T) entraid.Connection {
	t.Helper()
	client, err := entraid.New(entraid.Config{}, staticCredentials())
	require.NoError(t, err)
	connection, err := entraid.NewConnection(client, testConnection)
	require.NoError(t, err)
	return connection
}
