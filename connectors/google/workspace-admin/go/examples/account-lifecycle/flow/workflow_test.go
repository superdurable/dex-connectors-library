// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountlifecycle

import (
	"testing"

	"github.com/stretchr/testify/require"
	workspaceadmin "github.com/superdurable/dex-connectors-library/connectors/google/workspace-admin"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestBuildAccountRequestValidatesEveryAction(t *testing.T) {
	onboarding, err := BuildAccountRequest(Input{
		Action: AccountActionOnboard, PrimaryEmail: " Ada.Lovelace@Example.com ", GivenName: " Ada ", FamilyName: "Lovelace",
		OrgUnitPath: " /Engineering ", GroupEmail: "Engineering@Example.com", ProvisioningKey: " hire-42 ",
	})
	require.NoError(t, err)
	require.Equal(t, AccountRequest{
		Action: AccountActionOnboard, PrimaryEmail: "ada.lovelace@example.com", GivenName: "Ada", FamilyName: "Lovelace",
		OrgUnitPath: "/Engineering", GroupEmail: "engineering@example.com", GroupRole: workspaceadmin.GroupRoleMember, ProvisioningKey: "hire-42",
	}, onboarding)

	offboarding, err := BuildAccountRequest(Input{Action: AccountActionOffboard, PrimaryEmail: "ada@example.com", GroupEmail: "staff@example.com"})
	require.NoError(t, err)
	require.Equal(t, AccountActionOffboard, offboarding.Action)

	for name, input := range map[string]Input{
		"unknown action":      {Action: "delete", PrimaryEmail: "ada@example.com", GroupEmail: "staff@example.com"},
		"missing names":       {Action: AccountActionOnboard, PrimaryEmail: "ada@example.com", GroupEmail: "staff@example.com"},
		"display-name email":  {Action: AccountActionReinstate, PrimaryEmail: "Ada <ada@example.com>", GroupEmail: "staff@example.com"},
		"missing group email": {Action: AccountActionOffboard, PrimaryEmail: "ada@example.com"},
	} {
		_, err := BuildAccountRequest(input)
		require.Error(t, err, name)
	}
}

func TestOperationMappersCarryTheRecordedRequest(t *testing.T) {
	request := AccountRequest{
		Action: AccountActionOnboard, PrimaryEmail: "ada@example.com", GivenName: "Ada", FamilyName: "Lovelace",
		OrgUnitPath: "/Engineering", RecoveryEmail: "ada@personal.example", GroupEmail: "engineering@example.com",
		GroupRole: workspaceadmin.GroupRoleManager, ProvisioningKey: "hire-42",
	}
	require.Equal(t, workspaceadmin.CreateUserInput{
		PrimaryEmail: "ada@example.com", GivenName: "Ada", FamilyName: "Lovelace", OrgUnitPath: "/Engineering",
		RecoveryEmail: "ada@personal.example", ProvisioningKey: "hire-42",
	}, MapToCreateUserInput(request))
	require.Equal(t, workspaceadmin.UnsuspendUserInput{UserKey: "ada@example.com"}, MapToUnsuspendUserInput(request))
	require.Equal(t, workspaceadmin.SuspendUserInput{UserKey: "ada@example.com"}, MapToSuspendUserInput(request))

	membership := BuildTeamGroupMembership(request, workspaceadmin.User{ID: "1001", PrimaryEmail: "ada@example.com"})
	require.Equal(t, TeamGroupMembership{GroupEmail: "engineering@example.com", MemberEmail: "ada@example.com", Role: workspaceadmin.GroupRoleManager}, membership)
	require.Equal(t, workspaceadmin.AddUserToGroupInput{GroupKey: "engineering@example.com", MemberEmail: "ada@example.com", Role: workspaceadmin.GroupRoleManager},
		MapToAddUserToGroupInput(membership))
	require.Equal(t, workspaceadmin.GetUserInput{UserKey: "1001"}, MapToGetUserInput(workspaceadmin.AddUserToGroupResult{
		Value: workspaceadmin.AddUserToGroupOutput{Membership: workspaceadmin.GroupMembership{MemberID: "1001"}},
	}))
	require.Equal(t, workspaceadmin.RemoveUserFromGroupInput{GroupKey: "engineering@example.com", MemberKey: "1001"},
		MapToRemoveUserFromGroupInput(TeamGroupRemoval{GroupEmail: "engineering@example.com", MemberID: "1001"}))
}

func TestConnectionNameIsTheStaticDexWebConnection(t *testing.T) {
	require.Equal(t, "google-workspace-admin", ConnectionName)
	require.NoError(t, sdkgo.ConnectionRef{Provider: "google", Name: ConnectionName}.Validate())
}
