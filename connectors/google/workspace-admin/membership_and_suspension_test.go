// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workspaceadmin_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	workspaceadmin "github.com/superdurable/dex-connectors-library/connectors/google/workspace-admin"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestSuspendAndUnsuspendSetAnAbsoluteFlagThatIsSafeToRepeat(t *testing.T) {
	directory := newFakeDirectory(t)
	seeded := directory.seedUser("ada@example.com", nil)
	client := newTestClient(t, directory.URL)
	ctx := newTestDexContext("suspend")

	for range 2 {
		result, err := sdkgo.RunMutation(ctx, client.SuspendUser(), testConnection, workspaceadmin.SuspendUserInput{UserKey: "ada@example.com"})
		require.NoError(t, err)
		require.Equal(t, workspaceadmin.SuspendUserBranchSuspended, result.Branch)
		require.True(t, result.Value.IsSuspended)
		require.Equal(t, "ADMIN", result.Value.SuspensionReason)
		require.Equal(t, seeded["id"], result.Receipt.ProviderObjectID)
	}
	restored, err := sdkgo.RunMutation(newTestDexContext("unsuspend"), client.UnsuspendUser(), testConnection, workspaceadmin.UnsuspendUserInput{UserKey: seeded["id"].(string)})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.UnsuspendUserBranchUnsuspended, restored.Branch)
	require.False(t, restored.Value.IsSuspended)

	for index, request := range directory.recorded() {
		require.Equal(t, http.MethodPut, request.method)
		require.Equal(t, map[string]any{"suspended": index < 2}, request.body, "only the suspended flag is sent")
	}
}

func TestSuspensionSelectsNotFoundAndRejectsAnUnchangedAccount(t *testing.T) {
	directory := newFakeDirectory(t)
	client := newTestClient(t, directory.URL)

	missing, err := sdkgo.RunMutation(newTestDexContext("suspend-missing"), client.SuspendUser(), testConnection, workspaceadmin.SuspendUserInput{UserKey: "gone@example.com"})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.SuspendUserBranchNotFound, missing.Branch)

	directory.seedUser("abuse@example.com", nil)
	directory.setIntercept(func(recordedRequest, int) (int, any, bool) {
		return http.StatusOK, map[string]any{"id": "123", "primaryEmail": "abuse@example.com", "suspended": true, "suspensionReason": "ABUSE"}, true
	})
	kept, err := sdkgo.RunMutation(newTestDexContext("unsuspend-kept"), client.UnsuspendUser(), testConnection, workspaceadmin.UnsuspendUserInput{UserKey: "abuse@example.com"})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.UnsuspendUserBranchProviderRejected, kept.Branch)
	require.True(t, kept.Value.IsSuspended)
	require.Equal(t, sdkgo.FailureProviderRejection, kept.Failure.Kind)

	invalid, err := sdkgo.RunMutation(newTestDexContext("suspend-invalid"), client.SuspendUser(), testConnection, workspaceadmin.SuspendUserInput{})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.SuspendUserBranchDefect, invalid.Branch)
}

func TestAddUserToGroupConvergesOnOneMembershipWithoutChangingItsRole(t *testing.T) {
	directory := newFakeDirectory(t)
	user := directory.seedUser("ada@example.com", nil)
	directory.seedGroup("engineering@example.com")
	client := newTestClient(t, directory.URL)
	ctx := newTestDexContext("add-member")
	input := workspaceadmin.AddUserToGroupInput{GroupKey: "engineering@example.com", MemberEmail: "ada@example.com"}

	added, err := sdkgo.RunMutation(ctx, client.AddUserToGroup(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.AddUserToGroupBranchAdded, added.Branch)
	require.False(t, added.Value.WasAlreadyMember)
	require.Equal(t, workspaceadmin.GroupMembership{
		GroupKey: "engineering@example.com", MemberID: user["id"].(string), Email: "ada@example.com",
		Role: workspaceadmin.GroupRoleMember, Type: "USER", Status: "ACTIVE",
	}, added.Value.Membership)
	repeated, err := sdkgo.RunMutation(ctx, client.AddUserToGroup(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.AddUserToGroupBranchAdded, repeated.Branch)
	require.True(t, repeated.Value.WasAlreadyMember)
	require.Equal(t, 1, directory.memberCount("engineering@example.com"))

	directory.seedUser("grace@example.com", nil)
	directory.seedMember("engineering@example.com", "grace@example.com", "OWNER")
	owner, err := sdkgo.RunMutation(newTestDexContext("add-owner"), client.AddUserToGroup(), testConnection,
		workspaceadmin.AddUserToGroupInput{GroupKey: "engineering@example.com", MemberEmail: "grace@example.com", Role: workspaceadmin.GroupRoleMember})
	require.NoError(t, err)
	require.True(t, owner.Value.WasAlreadyMember)
	require.Equal(t, workspaceadmin.GroupRoleOwner, owner.Value.Membership.Role, "an existing role is reported, not downgraded")
	require.Equal(t, map[string]any{"email": "ada@example.com", "role": "MEMBER"}, directory.recorded()[0].body)
}

func TestAddUserToGroupRetriesAnUnreadableConflictAndSelectsNotFound(t *testing.T) {
	directory := newFakeDirectory(t)
	directory.seedGroup("engineering@example.com")
	client := newTestClient(t, directory.URL)

	missingUser, err := sdkgo.RunMutation(newTestDexContext("missing-user"), client.AddUserToGroup(), testConnection,
		workspaceadmin.AddUserToGroupInput{GroupKey: "engineering@example.com", MemberEmail: "nobody@example.com"})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.AddUserToGroupBranchNotFound, missingUser.Branch)
	missingGroup, err := sdkgo.RunMutation(newTestDexContext("missing-group"), client.AddUserToGroup(), testConnection,
		workspaceadmin.AddUserToGroupInput{GroupKey: "nowhere@example.com", MemberEmail: "nobody@example.com"})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.AddUserToGroupBranchNotFound, missingGroup.Branch)

	directory.setIntercept(func(request recordedRequest, _ int) (int, any, bool) {
		if request.method == http.MethodPost {
			return http.StatusConflict, googleError(http.StatusConflict, "duplicate"), true
		}
		return 0, nil, false
	})
	_, err = sdkgo.RunMutation(newTestDexContext("unreadable-conflict"), client.AddUserToGroup(), testConnection,
		workspaceadmin.AddUserToGroupInput{GroupKey: "engineering@example.com", MemberEmail: "nobody@example.com"})
	require.Equal(t, sdkgo.FailureAvailability, requireRetry(t, err).Kind)
}

func TestAddUserToGroupRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	directory := newFakeDirectory(t)
	client := newTestClient(t, directory.URL)
	for name, input := range map[string]workspaceadmin.AddUserToGroupInput{
		"missing group":  {MemberEmail: "ada@example.com"},
		"member ID":      {GroupKey: "engineering@example.com", MemberEmail: "1234567890"},
		"unknown role":   {GroupKey: "engineering@example.com", MemberEmail: "ada@example.com", Role: "ADMIN"},
		"lowercase role": {GroupKey: "engineering@example.com", MemberEmail: "ada@example.com", Role: "member"},
	} {
		result, err := sdkgo.RunMutation(newTestDexContext("invalid-add"), client.AddUserToGroup(), testConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, workspaceadmin.AddUserToGroupBranchDefect, result.Branch, name)
	}
	require.Empty(t, directory.recorded())
}

func TestRemoveUserFromGroupReportsAnAlreadyRemovedMembershipAndAMissingGroup(t *testing.T) {
	directory := newFakeDirectory(t)
	directory.seedUser("ada@example.com", nil)
	directory.seedGroup("engineering@example.com")
	directory.seedMember("engineering@example.com", "ada@example.com", "MEMBER")
	client := newTestClient(t, directory.URL)
	ctx := newTestDexContext("remove-member")
	input := workspaceadmin.RemoveUserFromGroupInput{GroupKey: "engineering@example.com", MemberKey: "ada@example.com"}

	removed, err := sdkgo.RunMutation(ctx, client.RemoveUserFromGroup(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.RemoveUserFromGroupBranchRemoved, removed.Branch)
	require.False(t, removed.Value.WasAlreadyRemoved)
	repeated, err := sdkgo.RunMutation(ctx, client.RemoveUserFromGroup(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.RemoveUserFromGroupBranchRemoved, repeated.Branch)
	require.True(t, repeated.Value.WasAlreadyRemoved)
	require.Zero(t, directory.memberCount("engineering@example.com"))
	groupCheck := directory.recorded()[2]
	require.Equal(t, http.MethodGet, groupCheck.method)
	require.Equal(t, "/admin/directory/v1/groups/engineering@example.com/members", groupCheck.path)
	require.Equal(t, "1", groupCheck.query.Get("maxResults"))

	missingGroup, err := sdkgo.RunMutation(newTestDexContext("remove-missing-group"), client.RemoveUserFromGroup(), testConnection,
		workspaceadmin.RemoveUserFromGroupInput{GroupKey: "nowhere@example.com", MemberKey: "ada@example.com"})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.RemoveUserFromGroupBranchNotFound, missingGroup.Branch)
	invalid, err := sdkgo.RunMutation(newTestDexContext("remove-invalid"), client.RemoveUserFromGroup(), testConnection,
		workspaceadmin.RemoveUserFromGroupInput{GroupKey: "engineering@example.com", MemberKey: "Ada Lovelace"})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.RemoveUserFromGroupBranchDefect, invalid.Branch)
}

func TestRemoveUserFromGroupRetriesUnavailableResponses(t *testing.T) {
	directory := newFakeDirectory(t)
	directory.setIntercept(func(recordedRequest, int) (int, any, bool) {
		return http.StatusInternalServerError, googleError(http.StatusInternalServerError, "backendError"), true
	})
	client := newTestClient(t, directory.URL)

	_, err := sdkgo.RunMutation(newTestDexContext("remove-unavailable"), client.RemoveUserFromGroup(), testConnection,
		workspaceadmin.RemoveUserFromGroupInput{GroupKey: "engineering@example.com", MemberKey: "ada@example.com"})
	require.Equal(t, sdkgo.FailureAvailability, requireRetry(t, err).Kind)
}
