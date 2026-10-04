// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workspaceadmin_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	workspaceadmin "github.com/superdurable/dex-connectors-library/connectors/google/workspace-admin"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type completeTarget[IN any] struct {
	dex.StepDefaultsNoWaitFor[IN]
}

func (completeTarget[IN]) Execute(dex.Context, IN) (*dex.StepDecision, error) {
	return dex.GracefulComplete(struct{}{}), nil
}

// GetStepType names the target: Dex gives a generic Step type no default name.
func (completeTarget[IN]) GetStepType() string { return "Complete" }

func TestFactoriesRequireOnlyTheHappyPathBranch(t *testing.T) {
	connection := newTestConnection(t)
	annotations := sdkgo.StepAnnotations{GroupID: "workspace", GroupLabel: "Google Workspace", Explanation: "Call the Directory API."}
	require.NotPanics(t, func() {
		workspaceadmin.NewGetUserStep(workspaceadmin.GetUserStepConfig[string]{
			StepType: "GetUser", Annotations: annotations, Connection: connection, ConnectionName: testConnection.Name,
			MapToOperationInput: func(userKey string) workspaceadmin.GetUserInput { return workspaceadmin.GetUserInput{UserKey: userKey} },
			Found:               sdkgo.GoTo(completeTarget[workspaceadmin.GetUserResult]{}),
		})
		workspaceadmin.NewListUsersStep(workspaceadmin.ListUsersStepConfig[string]{
			StepType: "ListUsers", Annotations: annotations, Connection: connection, ConnectionName: testConnection.Name,
			MapToOperationInput: func(query string) workspaceadmin.ListUsersInput { return workspaceadmin.ListUsersInput{Query: query} },
			Listed:              sdkgo.GoTo(completeTarget[workspaceadmin.ListUsersResult]{}),
		})
		workspaceadmin.NewCreateUserStep(workspaceadmin.CreateUserStepConfig[string]{
			StepType: "CreateUser", Annotations: annotations, Connection: connection, ConnectionName: testConnection.Name,
			MapToOperationInput: func(string) workspaceadmin.CreateUserInput { return validCreateUserInput() },
			Created:             sdkgo.GoTo(completeTarget[workspaceadmin.CreateUserResult]{}),
		})
		workspaceadmin.NewSuspendUserStep(workspaceadmin.SuspendUserStepConfig[string]{
			StepType: "SuspendUser", Annotations: annotations, Connection: connection, ConnectionName: testConnection.Name,
			MapToOperationInput: func(userKey string) workspaceadmin.SuspendUserInput {
				return workspaceadmin.SuspendUserInput{UserKey: userKey}
			},
			Suspended: sdkgo.GoTo(completeTarget[workspaceadmin.SuspendUserResult]{}),
		})
		workspaceadmin.NewUnsuspendUserStep(workspaceadmin.UnsuspendUserStepConfig[string]{
			StepType: "UnsuspendUser", Annotations: annotations, Connection: connection, ConnectionName: testConnection.Name,
			MapToOperationInput: func(userKey string) workspaceadmin.UnsuspendUserInput {
				return workspaceadmin.UnsuspendUserInput{UserKey: userKey}
			},
			Unsuspended: sdkgo.GoTo(completeTarget[workspaceadmin.UnsuspendUserResult]{}),
		})
		workspaceadmin.NewAddUserToGroupStep(workspaceadmin.AddUserToGroupStepConfig[string]{
			StepType: "AddUserToGroup", Annotations: annotations, Connection: connection, ConnectionName: testConnection.Name,
			MapToOperationInput: func(email string) workspaceadmin.AddUserToGroupInput {
				return workspaceadmin.AddUserToGroupInput{GroupKey: "staff@example.com", MemberEmail: email}
			},
			Added: sdkgo.GoTo(completeTarget[workspaceadmin.AddUserToGroupResult]{}),
		})
		workspaceadmin.NewRemoveUserFromGroupStep(workspaceadmin.RemoveUserFromGroupStepConfig[string]{
			StepType: "RemoveUserFromGroup", Annotations: annotations, Connection: connection, ConnectionName: testConnection.Name,
			MapToOperationInput: func(email string) workspaceadmin.RemoveUserFromGroupInput {
				return workspaceadmin.RemoveUserFromGroupInput{GroupKey: "staff@example.com", MemberKey: email}
			},
			Removed: sdkgo.GoTo(completeTarget[workspaceadmin.RemoveUserFromGroupResult]{}),
		})
	})
	require.Panics(t, func() {
		workspaceadmin.NewCreateUserStep(workspaceadmin.CreateUserStepConfig[string]{
			StepType: "CreateUser", Connection: connection, ConnectionName: testConnection.Name,
			MapToOperationInput: func(string) workspaceadmin.CreateUserInput { return validCreateUserInput() },
			AlreadyExists:       sdkgo.GoTo(completeTarget[workspaceadmin.CreateUserResult]{}),
		})
	}, "created is the required branch")
	require.Panics(t, func() {
		workspaceadmin.NewGetUserStep(workspaceadmin.GetUserStepConfig[string]{
			StepType: "GetUser", Connection: connection, ConnectionName: "another-connection",
			MapToOperationInput: func(userKey string) workspaceadmin.GetUserInput { return workspaceadmin.GetUserInput{UserKey: userKey} },
			Found:               sdkgo.GoTo(completeTarget[workspaceadmin.GetUserResult]{}),
		})
	}, "the static ConnectionName must match the runtime connection")
}

func TestDefinitionsDeclareOptionalBranchesAndAsyncDurability(t *testing.T) {
	require.Equal(t, map[sdkgo.BranchID]bool{"found": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(workspaceadmin.GetUserDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"listed": false, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(workspaceadmin.ListUsersDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"created": false, "alreadyExists": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(workspaceadmin.CreateUserDefinition.Branches), "the creation-key read-back makes every dispatch safe to repeat, so there is no uncertain branch")
	require.Equal(t, map[sdkgo.BranchID]bool{"suspended": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(workspaceadmin.SuspendUserDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"unsuspended": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(workspaceadmin.UnsuspendUserDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"added": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(workspaceadmin.AddUserToGroupDefinition.Branches))
	require.Equal(t, map[sdkgo.BranchID]bool{"removed": false, "notFound": true, "providerRejected": true, "invalidResponse": true, "defect": true},
		branchOptionality(workspaceadmin.RemoveUserFromGroupDefinition.Branches))

	for name, defaults := range map[string]sdkgo.StepDefaults{
		"getUser": workspaceadmin.GetUserDefinition.StepDefaults, "listUsers": workspaceadmin.ListUsersDefinition.StepDefaults,
		"createUser": workspaceadmin.CreateUserDefinition.StepDefaults, "suspendUser": workspaceadmin.SuspendUserDefinition.StepDefaults,
		"unsuspendUser": workspaceadmin.UnsuspendUserDefinition.StepDefaults, "addUserToGroup": workspaceadmin.AddUserToGroupDefinition.StepDefaults,
		"removeUserFromGroup": workspaceadmin.RemoveUserFromGroupDefinition.StepDefaults,
	} {
		require.Equal(t, dex.StepDurabilityAsync, defaults.ExecuteDurability, "%s: a duplicate dispatch converges, so async is safe", name)
		require.Equal(t, 30*time.Second, defaults.ExecuteMethodTimeout, name)
	}
	for name, defaults := range map[string]sdkgo.StepDefaults{
		"createUser": workspaceadmin.CreateUserDefinition.StepDefaults, "addUserToGroup": workspaceadmin.AddUserToGroupDefinition.StepDefaults,
	} {
		require.Equal(t, int32(8), defaults.ExecuteRetry.MaximumAttempts, name)
		require.Greater(t, retryScheduleBeforeLastAttempt(defaults.ExecuteRetry), time.Minute,
			"%s: the last attempt starts after the one-minute creation visibility window", name)
	}
}

func TestConnectionAndCredentialsNeverRevealSecrets(t *testing.T) {
	connection := newTestConnection(t)
	_, err := json.Marshal(connection)
	require.Error(t, err)
	require.NotContains(t, fmt.Sprintf("%v %#v", connection, connection), testAccessToken)
	credentials := workspaceadmin.Credentials{
		AuthMethodID: workspaceadmin.WorkspaceDomainDelegationAuthMethodID, ServiceAccountKey: sdkgo.NewSecretString("PRIVATE KEY material"),
		DelegatedUser: "admin@example.com", AccessToken: sdkgo.NewSecretString(testAccessToken),
	}
	rendered := fmt.Sprintf("%v %+v %#v", credentials, credentials, credentials)
	require.NotContains(t, rendered, testAccessToken)
	require.NotContains(t, rendered, "PRIVATE KEY material")
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

func newTestConnection(t *testing.T) workspaceadmin.Connection {
	t.Helper()
	client, err := workspaceadmin.New(workspaceadmin.Config{}, staticCredentials())
	require.NoError(t, err)
	connection, err := workspaceadmin.NewConnection(client, testConnection)
	require.NoError(t, err)
	return connection
}
