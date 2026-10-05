// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package clickup_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/clickup"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const authorizedWorkspacesJSON = `{"teams":[
	{"id":"9014999999","name":"Other","members":[{"user":{"id":7,"username":"Ada Elsewhere","email":"ada@acme.example.com"}}]},
	{"id":"` + testWorkspaceID + `","name":"Acme","members":[
		{"user":{"id":183,"username":"Ada Lovelace","email":"Ada@Acme.example.com","role":2}},
		{"user":{"id":"184","username":"Ben","email":"ben@acme.example.com","role":3}}]}]}`

func TestFindMemberByEmailMatchesTheWorkspaceMemberWithoutCase(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, authorizedWorkspacesJSON)
	})
	client := newClickUpClient(t, provider.URL)
	result, err := sdkgo.RunQuery(newTestDexContext("member"), client.FindMemberByEmail(), clickupConnection,
		clickup.FindMemberByEmailInput{WorkspaceID: testWorkspaceID, Email: "ada@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, clickup.FindMemberByEmailBranchFound, result.Branch)
	require.Equal(t, clickup.Member{ID: 183, Username: "Ada Lovelace", Email: "Ada@Acme.example.com", Role: clickup.MemberRoleAdmin, WorkspaceID: testWorkspaceID},
		result.Value, "the member of the requested Workspace, not the same email in another")
	require.Equal(t, "183", result.Receipt.ProviderObjectID)
	require.Equal(t, http.MethodGet, provider.request(0).method)
	require.Equal(t, "/team", provider.request(0).path)

	result, err = sdkgo.RunQuery(newTestDexContext("member-string-id"), client.FindMemberByEmail(), clickupConnection,
		clickup.FindMemberByEmailInput{WorkspaceID: testWorkspaceID, Email: "ben@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, int64(184), result.Value.ID, "a user ID sent as a string is accepted")
}

func TestFindMemberByEmailNotFound(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, authorizedWorkspacesJSON)
	})
	client := newClickUpClient(t, provider.URL)
	for input, message := range map[clickup.FindMemberByEmailInput]string{
		{WorkspaceID: testWorkspaceID, Email: "contractor@vendor.example.com"}: "no member of the Workspace has the email address",
		{WorkspaceID: "9014000000", Email: "ada@acme.example.com"}:             "the token's user is not a member of the Workspace",
	} {
		result, err := sdkgo.RunQuery(newTestDexContext("member-missing-"+input.WorkspaceID), client.FindMemberByEmail(), clickupConnection, input)
		require.NoError(t, err)
		require.Equal(t, clickup.FindMemberByEmailBranchNotFound, result.Branch)
		require.Equal(t, message, result.Failure.Message)
	}
}

func TestFindMemberByEmailRequiresOneBareAddress(t *testing.T) {
	provider := newRecordingClickUp(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, authorizedWorkspacesJSON)
	})
	for _, email := range []string{"", "Ada <ada@acme.example.com>", "ada@", "ada@acme.example.com, ben@acme.example.com"} {
		result, err := sdkgo.RunQuery(newTestDexContext("member-invalid"), newClickUpClient(t, provider.URL).FindMemberByEmail(), clickupConnection,
			clickup.FindMemberByEmailInput{WorkspaceID: testWorkspaceID, Email: email})
		require.NoError(t, err)
		require.Equal(t, clickup.FindMemberByEmailBranchDefect, result.Branch, email)
	}
	require.Zero(t, provider.requestCount())
}
