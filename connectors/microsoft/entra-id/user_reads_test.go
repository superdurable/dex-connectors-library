// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	entraid "github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetUserReturnsABoundedAccountByObjectIDOrName(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	userID := graph.SeedUser("ada.lovelace@contoso.com", false, map[string]any{"extensionAttribute15": "dex-creation-key:hire-42"})
	client := newTestClient(t, graph, entraid.Config{})

	byID, err := sdkgo.RunQuery(newTestDexContext("get"), client.GetUser(), testConnection, entraid.GetUserInput{UserKey: " " + strings.ToUpper(userID) + " "})
	require.NoError(t, err)
	require.Equal(t, entraid.GetUserBranchFound, byID.Branch)
	require.Equal(t, userID, byID.Value.ID, "object IDs are returned in lowercase")
	require.False(t, byID.Value.IsAccountEnabled)
	require.Equal(t, "Member", byID.Value.UserType)
	require.Equal(t, userID, byID.Receipt.ProviderObjectID)
	request := graph.Requests()[0]
	require.Equal(t, "/v1.0/users/"+userID, request.Path)
	require.Contains(t, request.Query.Get("$select"), "accountEnabled")
	require.NotContains(t, request.Query.Get("$select"), "onPremisesExtensionAttributes")
	encoded, err := json.Marshal(byID)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "650")
	require.NotContains(t, string(encoded), "hire-42")

	byName, err := sdkgo.RunQuery(newTestDexContext("get"), client.GetUser(), testConnection, entraid.GetUserInput{UserKey: "Ada.Lovelace@contoso.com"})
	require.NoError(t, err)
	require.Equal(t, userID, byName.Value.ID)
}

func TestGetUserAddressesGuestAndDollarNamesAsMicrosoftDocuments(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	guestID := graph.SeedUser("ada_fabrikam.com#EXT#@contoso.onmicrosoft.com", true, nil)
	dollarID := graph.SeedUser("$ada@contoso.com", true, nil)
	client := newTestClient(t, graph, entraid.Config{})

	guest, err := sdkgo.RunQuery(newTestDexContext("get"), client.GetUser(), testConnection, entraid.GetUserInput{UserKey: "ada_fabrikam.com#EXT#@contoso.onmicrosoft.com"})
	require.NoError(t, err)
	require.Equal(t, guestID, guest.Value.ID)
	dollar, err := sdkgo.RunQuery(newTestDexContext("get"), client.GetUser(), testConnection, entraid.GetUserInput{UserKey: "$ada@contoso.com"})
	require.NoError(t, err)
	require.Equal(t, dollarID, dollar.Value.ID)
	require.Equal(t, "/v1.0/users('$ada@contoso.com')", graph.Requests()[1].Path)
}

func TestGetUserSelectsNotFoundAndRejectsInvalidKeysBeforeAnyRequest(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	client := newTestClient(t, graph, entraid.Config{})

	missing, err := sdkgo.RunQuery(newTestDexContext("get"), client.GetUser(), testConnection, entraid.GetUserInput{UserKey: missingObjectID})
	require.NoError(t, err)
	require.Equal(t, entraid.GetUserBranchNotFound, missing.Branch)
	require.Equal(t, sdkgo.FailureNotFound, missing.Failure.Kind)
	requireSafeFailure(t, missing.Failure)

	for _, userKey := range []string{"", "ada", "Ada <ada@contoso.com>", "ada@contoso.com/../groups", "ada@contoso.com?x=1", "ada@@contoso.com"} {
		invalid, err := sdkgo.RunQuery(newTestDexContext("get"), client.GetUser(), testConnection, entraid.GetUserInput{UserKey: userKey})
		require.NoError(t, err)
		require.Equal(t, entraid.GetUserBranchDefect, invalid.Branch, userKey)
	}
	require.Len(t, graph.Requests(), 1)
}

func TestGetUserSelectsInvalidResponseForAnAccountWithoutAccountEnabled(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.Intercept = func(graphfake.Request, int) (graphfake.Answer, bool) {
		return graphfake.Answer{Status: http.StatusOK, Body: map[string]any{"id": graphfake.ObjectID(1), "userPrincipalName": "ada@contoso.com"}}, true
	}
	client := newTestClient(t, graph, entraid.Config{})

	result, err := sdkgo.RunQuery(newTestDexContext("get"), client.GetUser(), testConnection, entraid.GetUserInput{UserKey: "ada@contoso.com"})
	require.NoError(t, err)
	require.Equal(t, entraid.GetUserBranchInvalidResponse, result.Branch)
	require.Contains(t, result.Failure.Message, "User.Read.All")
}

func TestListUsersBoundsThePageAndFollowsOnlyAGraphNextLink(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	for _, name := range []string{"ada@contoso.com", "grace@contoso.com", "katherine@contoso.com"} {
		graph.SeedUser(name, true, nil)
	}
	client := newTestClient(t, graph, entraid.Config{ListUsersPageSize: 2})

	first, err := sdkgo.RunQuery(newTestDexContext("list"), client.ListUsers(), testConnection, entraid.ListUsersInput{})
	require.NoError(t, err)
	require.Equal(t, entraid.ListUsersBranchListed, first.Branch)
	require.Len(t, first.Value.Users, 2)
	require.True(t, strings.HasPrefix(first.Value.NextPageToken, "https://graph.microsoft.com/v1.0/users?"))
	request := graph.Requests()[0]
	require.Equal(t, "2", request.Query.Get("$top"))
	require.Contains(t, request.Query.Get("$select"), "accountEnabled")
	require.Empty(t, request.ConsistencyLevel)

	second, err := sdkgo.RunQuery(newTestDexContext("list"), client.ListUsers(), testConnection, entraid.ListUsersInput{PageToken: first.Value.NextPageToken})
	require.NoError(t, err)
	require.Len(t, second.Value.Users, 1)
	require.Equal(t, "katherine@contoso.com", second.Value.Users[0].UserPrincipalName)
	require.Empty(t, second.Value.NextPageToken)

	for _, pageToken := range []string{
		"https://evil.example/v1.0/users?$skiptoken=1", "http://graph.microsoft.com/v1.0/users?$skiptoken=1",
		"https://graph.microsoft.com/v1.0/groups?$skiptoken=1", "https://user:secret@graph.microsoft.com/v1.0/users?$skiptoken=1",
	} {
		rejected, err := sdkgo.RunQuery(newTestDexContext("list"), client.ListUsers(), testConnection, entraid.ListUsersInput{PageToken: pageToken})
		require.NoError(t, err)
		require.Equal(t, entraid.ListUsersBranchDefect, rejected.Branch, pageToken)
	}
	require.Len(t, graph.Requests(), 2)
}

func TestListUsersSendsEventualConsistencyOnlyForSearchAndAdvancedQueries(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	client := newTestClient(t, graph, entraid.Config{})
	for _, input := range []entraid.ListUsersInput{
		{Filter: "accountEnabled eq false"},
		{Search: `"displayName:Ada"`},
		{Filter: "endsWith(userPrincipalName,'@contoso.com')", UsesAdvancedQuery: true, PageSize: 999},
	} {
		result, err := sdkgo.RunQuery(newTestDexContext("list"), client.ListUsers(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, entraid.ListUsersBranchListed, result.Branch)
	}
	requests := graph.Requests()
	require.Equal(t, "accountEnabled eq false", requests[0].Query.Get("$filter"))
	require.Empty(t, requests[0].ConsistencyLevel)
	require.Equal(t, `"displayName:Ada"`, requests[1].Query.Get("$search"))
	require.Equal(t, "eventual", requests[1].ConsistencyLevel)
	require.Equal(t, "eventual", requests[2].ConsistencyLevel)
	require.Equal(t, "true", requests[2].Query.Get("$count"))
	require.Equal(t, "999", requests[2].Query.Get("$top"))
}

func TestListUsersRejectsInvalidSelectionsBeforeAnyRequest(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	client := newTestClient(t, graph, entraid.Config{})
	for name, input := range map[string]entraid.ListUsersInput{
		"page too large":           {PageSize: 1000},
		"negative page":            {PageSize: -1},
		"unquoted search":          {Search: "displayName:Ada"},
		"control character":        {Filter: "accountEnabled eq\x00false"},
		"filter too long":          {Filter: strings.Repeat("a", 2049)},
		"page token with filter":   {Filter: "accountEnabled eq true", PageToken: "https://graph.microsoft.com/v1.0/users?$skiptoken=1"},
		"page token with size":     {PageSize: 5, PageToken: "https://graph.microsoft.com/v1.0/users?$skiptoken=1"},
		"page token without query": {PageToken: "https://graph.microsoft.com/v1.0/users"},
	} {
		result, err := sdkgo.RunQuery(newTestDexContext("list"), client.ListUsers(), testConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, entraid.ListUsersBranchDefect, result.Branch, name)
	}
	require.Empty(t, graph.Requests())
}

func TestListUsersSelectsProviderRejectedAndInvalidResponse(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.Intercept = func(_ graphfake.Request, index int) (graphfake.Answer, bool) {
		switch index {
		case 0:
			return graphfake.Answer{Status: http.StatusBadRequest, Body: graphfake.GraphError("Request_UnsupportedQuery")}, true
		case 1:
			return graphfake.Answer{Status: http.StatusOK, Body: map[string]any{"value": []any{map[string]any{"id": "x"}}}}, true
		default:
			return graphfake.Answer{Status: http.StatusOK, Body: map[string]any{"value": []any{}, "@odata.nextLink": "https://evil.example/v1.0/users?" + url.Values{"$skiptoken": {"2"}}.Encode()}}, true
		}
	}
	client := newTestClient(t, graph, entraid.Config{})
	for _, expected := range []sdkgo.BranchID{entraid.ListUsersBranchProviderRejected, entraid.ListUsersBranchInvalidResponse, entraid.ListUsersBranchInvalidResponse} {
		result, err := sdkgo.RunQuery(newTestDexContext("list"), client.ListUsers(), testConnection, entraid.ListUsersInput{})
		require.NoError(t, err)
		require.Equal(t, expected, result.Branch)
		requireSafeFailure(t, result.Failure)
	}
}
