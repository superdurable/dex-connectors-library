// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workspaceadmin_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	workspaceadmin "github.com/superdurable/dex-connectors-library/connectors/google/workspace-admin"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestGetUserReturnsABoundedAccountWithoutRecoveryDetails(t *testing.T) {
	directory := newFakeDirectory(t)
	seeded := directory.seedUser("ada@example.com", nil)
	client := newTestClient(t, directory.URL)

	result, err := sdkgo.RunQuery(newTestDexContext("get-user"), client.GetUser(), testConnection, workspaceadmin.GetUserInput{UserKey: " ada@example.com "})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.GetUserBranchFound, result.Branch)
	require.Equal(t, seeded["id"], result.Value.ID)
	require.Equal(t, "ada@example.com", result.Value.PrimaryEmail)
	require.Equal(t, workspaceadmin.UserName{GivenName: "Existing", FamilyName: "Person"}, result.Value.Name)
	require.Equal(t, seeded["id"], result.Receipt.ProviderObjectID)
	require.Equal(t, "google-request", result.Receipt.ProviderRequestID)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private@personal.example")
	require.NotContains(t, string(encoded), "+16505550100")
	require.Equal(t, "/admin/directory/v1/users/ada@example.com", directory.recorded()[0].path)

	byID, err := sdkgo.RunQuery(newTestDexContext("get-user-by-id"), client.GetUser(), testConnection, workspaceadmin.GetUserInput{UserKey: seeded["id"].(string)})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.GetUserBranchFound, byID.Branch)
}

func TestGetUserSelectsNotFoundAndRejectsInvalidKeysBeforeAnyRequest(t *testing.T) {
	directory := newFakeDirectory(t)
	client := newTestClient(t, directory.URL)

	missing, err := sdkgo.RunQuery(newTestDexContext("missing"), client.GetUser(), testConnection, workspaceadmin.GetUserInput{UserKey: "nobody@example.com"})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.GetUserBranchNotFound, missing.Branch)
	requireSafeFailure(t, missing.Failure)

	for _, userKey := range []string{"", "Ada <ada@example.com>", "ada@example.com/../groups", "not an address"} {
		result, err := sdkgo.RunQuery(newTestDexContext("invalid-key"), client.GetUser(), testConnection, workspaceadmin.GetUserInput{UserKey: userKey})
		require.NoError(t, err)
		require.Equal(t, workspaceadmin.GetUserBranchDefect, result.Branch, userKey)
	}
	require.Len(t, directory.recorded(), 1)
}

func TestGetUserSelectsInvalidResponseForAnAccountWithoutAnID(t *testing.T) {
	directory := newFakeDirectory(t)
	directory.setIntercept(func(recordedRequest, int) (int, any, bool) {
		return http.StatusOK, map[string]any{"primaryEmail": "ada@example.com"}, true
	})
	client := newTestClient(t, directory.URL)

	result, err := sdkgo.RunQuery(newTestDexContext("invalid-account"), client.GetUser(), testConnection, workspaceadmin.GetUserInput{UserKey: "ada@example.com"})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.GetUserBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
}

func TestListUsersDefaultsToTheAdministratorsCustomerAccountAndBoundsThePage(t *testing.T) {
	directory := newFakeDirectory(t)
	directory.seedUser("ada@example.com", nil)
	client, err := workspaceadmin.New(workspaceadmin.Config{Endpoint: directory.URL, ListUsersPageSize: 25}, staticCredentials())
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newTestDexContext("list-users"), client.ListUsers(), testConnection, workspaceadmin.ListUsersInput{
		Query: "orgUnitPath='/Sales' isSuspended=false", PageToken: "next-page_token==",
	})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.ListUsersBranchListed, result.Branch)
	require.Len(t, result.Value.Users, 1)
	query := directory.recorded()[0].query
	require.Equal(t, "my_customer", query.Get("customer"))
	require.Empty(t, query.Get("domain"))
	require.Equal(t, "25", query.Get("maxResults"))
	require.Equal(t, "email", query.Get("orderBy"))
	require.Equal(t, "orgUnitPath='/Sales' isSuspended=false", query.Get("query"))
	require.Equal(t, "next-page_token==", query.Get("pageToken"))
}

func TestListUsersAcceptsOneDomainOrCustomerAndReturnsTheNextPageToken(t *testing.T) {
	directory := newFakeDirectory(t)
	directory.setIntercept(func(recordedRequest, int) (int, any, bool) {
		return http.StatusOK, map[string]any{"users": []any{}, "nextPageToken": "page-2"}, true
	})
	client := newTestClient(t, directory.URL)

	byDomain, err := sdkgo.RunQuery(newTestDexContext("list-domain"), client.ListUsers(), testConnection, workspaceadmin.ListUsersInput{
		Domain: "sub.example.com", OrderBy: "familyName", PageSize: 500,
	})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.ListUsersBranchListed, byDomain.Branch)
	require.Empty(t, byDomain.Value.Users)
	require.NotNil(t, byDomain.Value.Users, "an empty page is an empty list, not null")
	require.Equal(t, "page-2", byDomain.Value.NextPageToken)
	byCustomer, err := sdkgo.RunQuery(newTestDexContext("list-customer"), client.ListUsers(), testConnection, workspaceadmin.ListUsersInput{Customer: "C03az79cb"})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.ListUsersBranchListed, byCustomer.Branch)

	requests := directory.recorded()
	require.Equal(t, "sub.example.com", requests[0].query.Get("domain"))
	require.Empty(t, requests[0].query.Get("customer"))
	require.Equal(t, "familyName", requests[0].query.Get("orderBy"))
	require.Equal(t, "500", requests[0].query.Get("maxResults"))
	require.Equal(t, "C03az79cb", requests[1].query.Get("customer"))
}

func TestListUsersRejectsInvalidSelectionsBeforeAnyRequest(t *testing.T) {
	directory := newFakeDirectory(t)
	client := newTestClient(t, directory.URL)
	for name, input := range map[string]workspaceadmin.ListUsersInput{
		"domain and customer": {Domain: "example.com", Customer: "C03az79cb"},
		"invalid domain":      {Domain: "example.com/users"},
		"invalid customer":    {Customer: "customer one"},
		"oversized page":      {PageSize: 501},
		"negative page":       {PageSize: -1},
		"unknown order":       {OrderBy: "lastLoginTime"},
		"control character":   {Query: "givenName:Ada\nisAdmin=true"},
		"invalid page token":  {PageToken: "token with spaces"},
	} {
		result, err := sdkgo.RunQuery(newTestDexContext("invalid-list"), client.ListUsers(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, workspaceadmin.ListUsersBranchDefect, result.Branch, name)
	}
	require.Empty(t, directory.recorded())
}

func TestListUsersSelectsProviderRejectedAndInvalidResponse(t *testing.T) {
	directory := newFakeDirectory(t)
	directory.setIntercept(func(_ recordedRequest, index int) (int, any, bool) {
		if index == 0 {
			return http.StatusBadRequest, googleError(http.StatusBadRequest, "invalid"), true
		}
		return http.StatusOK, map[string]any{"users": []any{map[string]any{"id": "not-numeric", "primaryEmail": "ada@example.com"}}}, true
	})
	client := newTestClient(t, directory.URL)

	rejected, err := sdkgo.RunQuery(newTestDexContext("list-rejected"), client.ListUsers(), testConnection, workspaceadmin.ListUsersInput{Domain: "unknown.example"})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.ListUsersBranchProviderRejected, rejected.Branch)
	require.Equal(t, sdkgo.FailureValidation, rejected.Failure.Kind)
	requireSafeFailure(t, rejected.Failure)
	invalid, err := sdkgo.RunQuery(newTestDexContext("list-invalid"), client.ListUsers(), testConnection, workspaceadmin.ListUsersInput{})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.ListUsersBranchInvalidResponse, invalid.Branch)
}
