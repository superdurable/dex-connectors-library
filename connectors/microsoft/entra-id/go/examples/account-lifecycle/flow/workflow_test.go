// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountlifecycle

import (
	"testing"

	"github.com/stretchr/testify/require"
	entraid "github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const testGroupID = "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"

func TestBuildAccountRequestValidatesEveryAction(t *testing.T) {
	onboarding, err := BuildAccountRequest(Input{
		Action: AccountActionOnboard, UserPrincipalName: " ada.lovelace@contoso.com ", GivenName: " Ada ", Surname: "Lovelace",
		UsageLocation: "gb", GroupID: " AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE ", ProvisioningKey: " hire-42 ",
	})
	require.NoError(t, err)
	require.Equal(t, AccountRequest{
		Action: AccountActionOnboard, UserPrincipalName: "ada.lovelace@contoso.com", GivenName: "Ada", Surname: "Lovelace",
		UsageLocation: "GB", GroupID: testGroupID, ProvisioningKey: "hire-42",
	}, onboarding)

	offboarding, err := BuildAccountRequest(Input{Action: AccountActionOffboard, UserPrincipalName: "ada@contoso.com", GroupID: testGroupID})
	require.NoError(t, err)
	require.Equal(t, AccountActionOffboard, offboarding.Action)

	for name, input := range map[string]Input{
		"unknown action":     {Action: "delete", UserPrincipalName: "ada@contoso.com", GroupID: testGroupID},
		"missing names":      {Action: AccountActionOnboard, UserPrincipalName: "ada@contoso.com", GroupID: testGroupID},
		"display-name email": {Action: AccountActionReinstate, UserPrincipalName: "Ada <ada@contoso.com>", GroupID: testGroupID},
		"group email":        {Action: AccountActionOffboard, UserPrincipalName: "ada@contoso.com", GroupID: "staff@contoso.com"},
		"missing group":      {Action: AccountActionOffboard, UserPrincipalName: "ada@contoso.com"},
	} {
		_, err := BuildAccountRequest(input)
		require.Error(t, err, name)
	}
}

func TestOperationMappersCarryTheRecordedRequest(t *testing.T) {
	request := AccountRequest{
		Action: AccountActionOnboard, UserPrincipalName: "ada@contoso.com", DisplayName: "Ada Lovelace", GivenName: "Ada",
		Surname: "Lovelace", JobTitle: "Analyst", Department: "Engineering", UsageLocation: "GB", GroupID: testGroupID, ProvisioningKey: "hire-42",
	}
	require.Equal(t, entraid.CreateUserInput{
		UserPrincipalName: "ada@contoso.com", DisplayName: "Ada Lovelace", GivenName: "Ada", Surname: "Lovelace",
		JobTitle: "Analyst", Department: "Engineering", UsageLocation: "GB", ProvisioningKey: "hire-42",
	}, MapToCreateUserInput(request))
	require.Equal(t, entraid.EnableUserInput{UserKey: "ada@contoso.com"}, MapToEnableUserInput(request))
	require.Equal(t, entraid.GetUserInput{UserKey: "ada@contoso.com"}, MapToGetUserInput(request))

	account := entraid.User{ID: "00000000-0000-4000-8000-000000000001", UserPrincipalName: "ada@contoso.com"}
	require.Equal(t, entraid.DisableUserInput{UserKey: account.ID}, MapToDisableUserInput(entraid.GetUserResult{Value: account}))
	require.Equal(t, entraid.RevokeSignInSessionsInput{UserKey: account.ID}, MapToRevokeSignInSessionsInput(account))
	membership := BuildTeamGroupMembership(request, account)
	require.Equal(t, TeamGroupMembership{GroupID: testGroupID, UserID: account.ID}, membership)
	require.Equal(t, entraid.AddUserToGroupInput{GroupID: testGroupID, UserID: account.ID}, MapToAddUserToGroupInput(membership))
	require.Equal(t, entraid.RemoveUserFromGroupInput{GroupID: testGroupID, UserID: account.ID}, MapToRemoveUserFromGroupInput(membership))
}

func TestConnectionNameIsTheStaticDexWebConnection(t *testing.T) {
	require.Equal(t, "microsoft-entra-id", ConnectionName)
	require.NoError(t, sdkgo.ConnectionRef{Provider: "microsoft", Name: ConnectionName}.Validate())
}
