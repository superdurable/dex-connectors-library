// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	entraid "github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestAddUserToGroupConvergesOnOneMembership(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.SeedGroup(testGroupID)
	userID := graph.SeedUser("ada@contoso.com", true, nil)
	client := newTestClient(t, graph, entraid.Config{})
	input := entraid.AddUserToGroupInput{GroupID: strings.ToUpper(testGroupID), UserID: userID}

	added, err := sdkgo.RunMutation(newTestDexContext("add"), client.AddUserToGroup(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, entraid.AddUserToGroupBranchAdded, added.Branch)
	require.Equal(t, entraid.AddUserToGroupOutput{GroupID: testGroupID, UserID: userID}, added.Value)
	add := graph.Requests()[0]
	require.Equal(t, "/v1.0/groups/"+testGroupID+"/members/$ref", add.Path)
	require.Equal(t, map[string]any{"@odata.id": "https://graph.microsoft.com/v1.0/directoryObjects/" + userID}, add.Body)

	repeated, err := sdkgo.RunMutation(newTestDexContext("add"), client.AddUserToGroup(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, entraid.AddUserToGroupBranchAdded, repeated.Branch)
	require.True(t, repeated.Value.WasAlreadyMember, "a 400 for an existing member is confirmed from state, not message text")
	require.Equal(t, 1, graph.MemberCount(testGroupID))
	check := graph.Requests()[2]
	require.Equal(t, "eventual", check.ConsistencyLevel)
	require.Equal(t, "id eq '"+userID+"'", check.Query.Get("$filter"))
	require.Equal(t, "true", check.Query.Get("$count"))
}

func TestAddUserToGroupRetriesReplicationLagThenSelectsNotFoundOrRejected(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.SeedGroup(testGroupID)
	client := newTestClient(t, graph, entraid.Config{})
	missingUser := entraid.AddUserToGroupInput{GroupID: testGroupID, UserID: missingObjectID}

	_, err := sdkgo.RunMutation(newTestDexContext("add"), client.AddUserToGroup(), testConnection, missingUser)
	require.Equal(t, sdkgo.FailureAvailability, requireRetry(t, err).Kind, "a just-created account may not have replicated yet")
	late, err := sdkgo.RunMutation(newLateTestDexContext("add"), client.AddUserToGroup(), testConnection, missingUser)
	require.NoError(t, err)
	require.Equal(t, entraid.AddUserToGroupBranchNotFound, late.Branch)

	missingGroup, err := sdkgo.RunMutation(newTestDexContext("add"), client.AddUserToGroup(), testConnection,
		entraid.AddUserToGroupInput{GroupID: missingObjectID, UserID: missingObjectID})
	require.NoError(t, err)
	require.Equal(t, entraid.AddUserToGroupBranchNotFound, missingGroup.Branch, "a missing group is not retried")

	userID := graph.SeedUser("ada@contoso.com", true, nil)
	graph.Intercept = func(request graphfake.Request, _ int) (graphfake.Answer, bool) {
		if request.Method == http.MethodPost {
			return graphfake.Answer{Status: http.StatusBadRequest, Body: graphfake.GraphError("Request_BadRequest")}, true
		}
		return graphfake.Answer{}, false
	}
	unsupported := entraid.AddUserToGroupInput{GroupID: testGroupID, UserID: userID}
	_, err = sdkgo.RunMutation(newTestDexContext("add"), client.AddUserToGroup(), testConnection, unsupported)
	requireRetry(t, err)
	rejected, err := sdkgo.RunMutation(newLateTestDexContext("add"), client.AddUserToGroup(), testConnection, unsupported)
	require.NoError(t, err)
	require.Equal(t, entraid.AddUserToGroupBranchProviderRejected, rejected.Branch)
	requireSafeFailure(t, rejected.Failure)
}

func TestAddUserToGroupSelectsProviderRejectedForAGroupGraphCannotManage(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.Intercept = func(graphfake.Request, int) (graphfake.Answer, bool) {
		return graphfake.Answer{Status: http.StatusForbidden, Body: graphfake.GraphError("Authorization_RequestDenied")}, true
	}
	client := newTestClient(t, graph, entraid.Config{})

	result, err := sdkgo.RunMutation(newTestDexContext("add"), client.AddUserToGroup(), testConnection, entraid.AddUserToGroupInput{GroupID: testGroupID, UserID: missingObjectID})
	require.NoError(t, err)
	require.Equal(t, entraid.AddUserToGroupBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "Authorization_RequestDenied")
}

func TestMembershipOperationsRejectInvalidIDsBeforeAnyRequest(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	client := newTestClient(t, graph, entraid.Config{})
	for _, input := range []entraid.AddUserToGroupInput{
		{GroupID: "engineering@contoso.com", UserID: missingObjectID}, {GroupID: testGroupID, UserID: "ada@contoso.com"}, {},
	} {
		added, err := sdkgo.RunMutation(newTestDexContext("add"), client.AddUserToGroup(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, entraid.AddUserToGroupBranchDefect, added.Branch)
		removed, err := sdkgo.RunMutation(newTestDexContext("remove"), client.RemoveUserFromGroup(), testConnection,
			entraid.RemoveUserFromGroupInput{GroupID: input.GroupID, UserID: input.UserID})
		require.NoError(t, err)
		require.Equal(t, entraid.RemoveUserFromGroupBranchDefect, removed.Branch)
	}
	require.Empty(t, graph.Requests())
}

func TestRemoveUserFromGroupDeletesOnlyTheReferenceAndConverges(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.SeedGroup(testGroupID)
	userID := graph.SeedUser("ada@contoso.com", true, nil)
	graph.SeedMember(testGroupID, userID)
	client := newTestClient(t, graph, entraid.Config{})
	input := entraid.RemoveUserFromGroupInput{GroupID: testGroupID, UserID: userID}

	removed, err := sdkgo.RunMutation(newTestDexContext("remove"), client.RemoveUserFromGroup(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, entraid.RemoveUserFromGroupBranchRemoved, removed.Branch)
	require.False(t, removed.Value.WasAlreadyRemoved)
	require.Equal(t, "/v1.0/groups/"+testGroupID+"/members/"+userID+"/$ref", graph.Requests()[0].Path, "the path ends in /$ref so the account itself is never deleted")
	require.NotNil(t, graph.User(userID))
	require.Nil(t, graph.User(userID)["deleted"])

	repeated, err := sdkgo.RunMutation(newTestDexContext("remove"), client.RemoveUserFromGroup(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, entraid.RemoveUserFromGroupBranchRemoved, repeated.Branch)
	require.True(t, repeated.Value.WasAlreadyRemoved)
	require.Equal(t, "/v1.0/groups/"+testGroupID, graph.Requests()[2].Path)

	missingGroup, err := sdkgo.RunMutation(newTestDexContext("remove"), client.RemoveUserFromGroup(), testConnection,
		entraid.RemoveUserFromGroupInput{GroupID: missingObjectID, UserID: userID})
	require.NoError(t, err)
	require.Equal(t, entraid.RemoveUserFromGroupBranchNotFound, missingGroup.Branch)
}

func TestRemoveUserFromGroupRetriesUnavailableResponsesAndRejectsDynamicGroups(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.Intercept = func(_ graphfake.Request, index int) (graphfake.Answer, bool) {
		if index == 0 {
			return graphfake.Answer{Status: http.StatusServiceUnavailable, Body: graphfake.GraphError("serviceNotAvailable")}, true
		}
		return graphfake.Answer{Status: http.StatusBadRequest, Body: graphfake.GraphError("Request_BadRequest")}, true
	}
	graph.RetryAfter = "7"
	client := newTestClient(t, graph, entraid.Config{})
	input := entraid.RemoveUserFromGroupInput{GroupID: testGroupID, UserID: missingObjectID}

	_, err := sdkgo.RunMutation(newTestDexContext("remove"), client.RemoveUserFromGroup(), testConnection, input)
	require.Equal(t, sdkgo.FailureAvailability, requireRetry(t, err).Kind)
	dynamic, err := sdkgo.RunMutation(newTestDexContext("remove"), client.RemoveUserFromGroup(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, entraid.RemoveUserFromGroupBranchProviderRejected, dynamic.Branch)
}
