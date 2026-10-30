// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	entraid "github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestDisableAndEnableSetAnAbsoluteValueThatIsSafeToRepeat(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	userID := graph.SeedUser("ada@contoso.com", true, nil)
	client := newTestClient(t, graph, entraid.Config{})

	for range 2 {
		disabled, err := sdkgo.RunMutation(newTestDexContext("disable"), client.DisableUser(), testConnection, entraid.DisableUserInput{UserKey: userID})
		require.NoError(t, err)
		require.Equal(t, entraid.DisableUserBranchDisabled, disabled.Branch)
		require.False(t, disabled.Value.IsAccountEnabled)
		require.Equal(t, userID, disabled.Receipt.ProviderObjectID)
	}
	require.Equal(t, false, graph.User(userID)["accountEnabled"])
	patch := graph.Requests()[0]
	require.Equal(t, http.MethodPatch, patch.Method)
	require.Equal(t, map[string]any{"accountEnabled": false}, patch.Body, "no other property changes")

	enabled, err := sdkgo.RunMutation(newTestDexContext("enable"), client.EnableUser(), testConnection, entraid.EnableUserInput{UserKey: "ada@contoso.com"})
	require.NoError(t, err)
	require.Equal(t, entraid.EnableUserBranchEnabled, enabled.Branch)
	require.True(t, enabled.Value.IsAccountEnabled)
	require.Equal(t, true, graph.User(userID)["accountEnabled"])
}

func TestAccountStateRetriesAStaleReadBackAndThenRejects(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	userID := graph.SeedUser("ada@contoso.com", true, nil)
	graph.Intercept = func(request graphfake.Request, _ int) (graphfake.Answer, bool) {
		// Replication lag: the read-back still shows the account enabled.
		if request.Method == http.MethodGet {
			return graphfake.Answer{Status: http.StatusOK, Body: map[string]any{"id": userID, "userPrincipalName": "ada@contoso.com", "accountEnabled": true}}, true
		}
		return graphfake.Answer{}, false
	}
	client := newTestClient(t, graph, entraid.Config{})

	_, err := sdkgo.RunMutation(newTestDexContext("disable"), client.DisableUser(), testConnection, entraid.DisableUserInput{UserKey: userID})
	require.Equal(t, sdkgo.FailureAvailability, requireRetry(t, err).Kind)
	late, err := sdkgo.RunMutation(newLateTestDexContext("disable"), client.DisableUser(), testConnection, entraid.DisableUserInput{UserKey: userID})
	require.NoError(t, err)
	require.Equal(t, entraid.DisableUserBranchProviderRejected, late.Branch)
	require.True(t, late.Value.IsAccountEnabled, "the rejected Result carries the account Microsoft returned")
}

func TestAccountStateSelectsNotFoundAndProviderRejected(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	syncedID := graph.SeedUser("synced@contoso.com", true, nil)
	graph.Intercept = func(request graphfake.Request, _ int) (graphfake.Answer, bool) {
		if request.Path == "/v1.0/users/"+syncedID && request.Method == http.MethodPatch {
			return graphfake.Answer{Status: http.StatusBadRequest, Body: graphfake.GraphError("Request_BadRequest")}, true
		}
		return graphfake.Answer{}, false
	}
	client := newTestClient(t, graph, entraid.Config{})

	missing, err := sdkgo.RunMutation(newTestDexContext("disable"), client.DisableUser(), testConnection, entraid.DisableUserInput{UserKey: missingObjectID})
	require.NoError(t, err)
	require.Equal(t, entraid.DisableUserBranchNotFound, missing.Branch)
	synced, err := sdkgo.RunMutation(newTestDexContext("enable"), client.EnableUser(), testConnection, entraid.EnableUserInput{UserKey: syncedID})
	require.NoError(t, err)
	require.Equal(t, entraid.EnableUserBranchProviderRejected, synced.Branch)
	requireSafeFailure(t, synced.Failure)
	invalid, err := sdkgo.RunMutation(newTestDexContext("enable"), client.EnableUser(), testConnection, entraid.EnableUserInput{UserKey: "ada"})
	require.NoError(t, err)
	require.Equal(t, entraid.EnableUserBranchDefect, invalid.Branch)
	require.Len(t, graph.Requests(), 2)
}

func TestRevokeSignInSessionsIsSafeToRepeatAndSelectsNotFound(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	userID := graph.SeedUser("ada@contoso.com", false, nil)
	client := newTestClient(t, graph, entraid.Config{})

	for range 2 {
		revoked, err := sdkgo.RunMutation(newTestDexContext("revoke"), client.RevokeSignInSessions(), testConnection, entraid.RevokeSignInSessionsInput{UserKey: userID})
		require.NoError(t, err)
		require.Equal(t, entraid.RevokeSignInSessionsBranchRevoked, revoked.Branch)
		require.Equal(t, userID, revoked.Value.UserKey)
		require.Equal(t, userID, revoked.Receipt.ProviderObjectID)
	}
	require.Equal(t, 2, graph.Count(http.MethodPost, "/revokeSignInSessions"))
	require.Equal(t, "2026-10-01T10:00:00Z", graph.User(userID)["signInSessionsValidFromDateTime"])

	missing, err := sdkgo.RunMutation(newTestDexContext("revoke"), client.RevokeSignInSessions(), testConnection, entraid.RevokeSignInSessionsInput{UserKey: "nobody@contoso.com"})
	require.NoError(t, err)
	require.Equal(t, entraid.RevokeSignInSessionsBranchNotFound, missing.Branch)
}

func TestRevokeSignInSessionsHandlesEmptyFalseAndLostResponses(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	userID := graph.SeedUser("ada@contoso.com", true, nil)
	graph.Intercept = func(_ graphfake.Request, index int) (graphfake.Answer, bool) {
		switch index {
		case 0:
			return graphfake.Answer{Status: http.StatusNoContent}, true
		case 1:
			return graphfake.Answer{Status: http.StatusOK, Body: map[string]any{"value": false}}, true
		default:
			return graphfake.Answer{Status: http.StatusGatewayTimeout, Body: graphfake.GraphError("generalException")}, true
		}
	}
	client := newTestClient(t, graph, entraid.Config{})
	input := entraid.RevokeSignInSessionsInput{UserKey: userID}

	empty, err := sdkgo.RunMutation(newTestDexContext("revoke"), client.RevokeSignInSessions(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, entraid.RevokeSignInSessionsBranchRevoked, empty.Branch, "Microsoft documents any 2xx")
	refused, err := sdkgo.RunMutation(newTestDexContext("revoke"), client.RevokeSignInSessions(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, entraid.RevokeSignInSessionsBranchProviderRejected, refused.Branch)
	_, err = sdkgo.RunMutation(newTestDexContext("revoke"), client.RevokeSignInSessions(), testConnection, input)
	require.Equal(t, sdkgo.FailureAvailability, requireRetry(t, err).Kind)
}
