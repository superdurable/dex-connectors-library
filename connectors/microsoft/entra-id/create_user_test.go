// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	entraid "github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var generatedPasswordPattern = regexp.MustCompile(`^[A-Za-z0-9!#%*+\-=?@^_~]{44}$`)

func validCreateUserInput() entraid.CreateUserInput {
	return entraid.CreateUserInput{
		UserPrincipalName: "ada.lovelace@contoso.com", GivenName: " Ada ", Surname: "Lovelace",
		JobTitle: "Analyst", Department: "Engineering", UsageLocation: "GB",
	}
}

func TestCreateUserSendsAGeneratedPasswordThatNeverLeavesTheRequest(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	client := newTestClient(t, graph, entraid.Config{})

	result, err := sdkgo.RunMutation(newTestDexContext("create"), client.CreateUser(), testConnection, validCreateUserInput())
	require.NoError(t, err)
	require.Equal(t, entraid.CreateUserBranchCreated, result.Branch)
	require.False(t, result.Value.WasAlreadyCreated)
	require.Equal(t, "ada.lovelace@contoso.com", result.Value.User.UserPrincipalName)
	require.Equal(t, "Ada Lovelace", result.Value.User.DisplayName, "a blank display name joins the given name and surname")
	require.True(t, result.Value.User.IsAccountEnabled)
	require.Equal(t, "ada.lovelace", result.Value.User.MailNickname, "a blank mail alias uses the name before the @")
	require.Equal(t, result.Value.User.ID, result.Receipt.ProviderObjectID)

	requests := graph.Requests()
	require.Len(t, requests, 1)
	body := requests[0].Body
	profile := body["passwordProfile"].(map[string]any)
	password := profile["password"].(string)
	require.Regexp(t, generatedPasswordPattern, password)
	for _, characterClass := range []string{"abcdefghijklmnopqrstuvwxyz", "ABCDEFGHIJKLMNOPQRSTUVWXYZ", "0123456789", "!#%*+-=?@^_~"} {
		require.True(t, strings.ContainsAny(password, characterClass), "every Microsoft Entra character class appears")
	}
	require.Equal(t, true, profile["forceChangePasswordNextSignIn"])
	require.Equal(t, true, body["accountEnabled"])
	require.Equal(t, "Ada", body["givenName"])
	require.Equal(t, "GB", body["usageLocation"])
	require.Equal(t, map[string]any{"extensionAttribute15": "dex-creation-key:" + string(result.Receipt.IdempotencyKey)}, body["onPremisesExtensionAttributes"])
	require.Equal(t, string(result.Receipt.CallID), string(result.Receipt.IdempotencyKey), "a blank provisioning key uses the Call ID")

	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), password)

	second, err := sdkgo.RunMutation(newTestDexContext("create-another"), client.CreateUser(), testConnection,
		entraid.CreateUserInput{UserPrincipalName: "grace@contoso.com", DisplayName: "Grace Hopper", MailNickname: "ghopper"})
	require.NoError(t, err)
	require.Equal(t, entraid.CreateUserBranchCreated, second.Branch)
	secondRequest := graph.Requests()[1].Body
	require.NotEqual(t, password, secondRequest["passwordProfile"].(map[string]any)["password"], "every create generates a new password")
	require.Equal(t, "ghopper", secondRequest["mailNickname"])
	require.NotContains(t, secondRequest, "usageLocation")
}

func TestCreateUserRecordsTheKeyInTheConfiguredExtensionAttribute(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	client := newTestClient(t, graph, entraid.Config{CreationKeyAttribute: entraid.CreationKeyAttributeExtensionAttribute3})
	input := validCreateUserInput()
	input.ProvisioningKey = "hire-2026-0042"

	result, err := sdkgo.RunMutation(newTestDexContext("create"), client.CreateUser(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, entraid.CreateUserBranchCreated, result.Branch)
	require.Equal(t, map[string]any{"extensionAttribute3": "dex-creation-key:hire-2026-0042"}, graph.Requests()[0].Body["onPremisesExtensionAttributes"])
}

func TestCreateUserRepeatedStepConvergesOnTheAccountItCreated(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	client := newTestClient(t, graph, entraid.Config{})
	first, err := sdkgo.RunMutation(newTestDexContext("create"), client.CreateUser(), testConnection, validCreateUserInput())
	require.NoError(t, err)

	repeated, err := sdkgo.RunMutation(newTestDexContext("create"), client.CreateUser(), testConnection, validCreateUserInput())
	require.NoError(t, err)
	require.Equal(t, entraid.CreateUserBranchCreated, repeated.Branch)
	require.True(t, repeated.Value.WasAlreadyCreated)
	require.Equal(t, first.Value.User.ID, repeated.Value.User.ID)
	require.Equal(t, 1, graph.UserCount())
	readBack := graph.Requests()[2]
	require.Equal(t, http.MethodGet, readBack.Method)
	require.Equal(t, "/v1.0/users/ada.lovelace@contoso.com", readBack.Path)
	require.Contains(t, readBack.Query.Get("$select"), "onPremisesExtensionAttributes")
}

func TestCreateUserLostResponseIsRetriedAndTheRetryFindsTheAccount(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.AfterApply = func(request graphfake.Request, index int, answer graphfake.Answer) graphfake.Answer {
		if index == 0 {
			return graphfake.Answer{Status: http.StatusServiceUnavailable, Body: graphfake.GraphError("serviceNotAvailable")}
		}
		return answer
	}
	client := newTestClient(t, graph, entraid.Config{})
	ctx := newTestDexContext("create")

	_, err := sdkgo.RunMutation(ctx, client.CreateUser(), testConnection, validCreateUserInput())
	require.Equal(t, sdkgo.FailureAvailability, requireRetry(t, err).Kind)
	retried, err := sdkgo.RunMutation(ctx, client.CreateUser(), testConnection, validCreateUserInput())
	require.NoError(t, err)
	require.Equal(t, entraid.CreateUserBranchCreated, retried.Branch)
	require.True(t, retried.Value.WasAlreadyCreated)
	require.True(t, retried.Value.User.IsAccountEnabled)
	require.Equal(t, 1, graph.UserCount())
}

func TestCreateUserReportsANameThisStepDidNotCreate(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	existingID := graph.SeedUser("ada.lovelace@contoso.com", false, map[string]any{"extensionAttribute15": "dex-creation-key:someone-else"})
	client := newTestClient(t, graph, entraid.Config{})

	result, err := sdkgo.RunMutation(newTestDexContext("create"), client.CreateUser(), testConnection, validCreateUserInput())
	require.NoError(t, err)
	require.Equal(t, entraid.CreateUserBranchAlreadyExists, result.Branch)
	require.Equal(t, existingID, result.Value.User.ID)
	require.False(t, result.Value.User.IsAccountEnabled)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	requireSafeFailure(t, result.Failure)
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "someone-else", "extension attributes never reach a Result")
	require.NotContains(t, string(encoded), "650", "unselected phone numbers never reach a Result")
}

func TestCreateUserProvisioningKeyConvergesAcrossStepExecutions(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	client := newTestClient(t, graph, entraid.Config{})
	input := validCreateUserInput()
	input.ProvisioningKey = "hire-2026-0042"
	first, err := sdkgo.RunMutation(newTestDexContext("create-run-1"), client.CreateUser(), testConnection, input)
	require.NoError(t, err)

	second, err := sdkgo.RunMutation(newTestDexContext("create-run-2"), client.CreateUser(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, entraid.CreateUserBranchCreated, second.Branch)
	require.True(t, second.Value.WasAlreadyCreated)
	require.Equal(t, first.Value.User.ID, second.Value.User.ID)

	withoutKey, err := sdkgo.RunMutation(newTestDexContext("create-run-3"), client.CreateUser(), testConnection, validCreateUserInput())
	require.NoError(t, err)
	require.Equal(t, entraid.CreateUserBranchAlreadyExists, withoutKey.Branch, "a new Step execution without the key did not create the account")
}

// Microsoft documents replication delay after a create; a concurrent attempt can see the 400 before the account is readable.
func TestCreateUserWaitsForAnUnreadableNameBeforeConcluding(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.SeedUser("ada.lovelace@contoso.com", true, nil)
	graph.Intercept = func(request graphfake.Request, _ int) (graphfake.Answer, bool) {
		if request.Method == http.MethodGet {
			return graphfake.Answer{Status: http.StatusNotFound, Body: graphfake.GraphError("Request_ResourceNotFound")}, true
		}
		return graphfake.Answer{}, false
	}
	client := newTestClient(t, graph, entraid.Config{})

	_, err := sdkgo.RunMutation(newTestDexContext("create"), client.CreateUser(), testConnection, validCreateUserInput())
	require.Equal(t, sdkgo.FailureAvailability, requireRetry(t, err).Kind)

	late, err := sdkgo.RunMutation(newLateTestDexContext("create"), client.CreateUser(), testConnection, validCreateUserInput())
	require.NoError(t, err)
	require.Equal(t, entraid.CreateUserBranchAlreadyExists, late.Branch, "an ObjectConflict detail without a readable account is a deleted or non-user object")
	requireSafeFailure(t, late.Failure)
}

func TestCreateUserRejectsA400WithoutAConflictOnceTheWindowEnds(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.Intercept = func(request graphfake.Request, _ int) (graphfake.Answer, bool) {
		if request.Method == http.MethodPost {
			return graphfake.Answer{Status: http.StatusBadRequest, Body: graphfake.GraphError("Request_BadRequest")}, true
		}
		return graphfake.Answer{}, false
	}
	client := newTestClient(t, graph, entraid.Config{})

	_, err := sdkgo.RunMutation(newTestDexContext("create"), client.CreateUser(), testConnection, validCreateUserInput())
	requireRetry(t, err)
	late, err := sdkgo.RunMutation(newLateTestDexContext("create"), client.CreateUser(), testConnection, validCreateUserInput())
	require.NoError(t, err)
	require.Equal(t, entraid.CreateUserBranchProviderRejected, late.Branch)
	require.Equal(t, sdkgo.FailureValidation, late.Failure.Kind)
	require.Contains(t, late.Failure.Message, "Request_BadRequest")
	requireSafeFailure(t, late.Failure)
}

func TestCreateUserRetriesAnUnusableSuccessAndRejectsConclusiveErrors(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.Intercept = func(request graphfake.Request, index int) (graphfake.Answer, bool) {
		if index == 0 {
			return graphfake.Answer{Status: http.StatusCreated, Body: map[string]any{"id": "not-a-guid"}}, true
		}
		return graphfake.Answer{Status: http.StatusForbidden, Body: graphfake.GraphError("Authorization_RequestDenied")}, true
	}
	client := newTestClient(t, graph, entraid.Config{})

	_, err := sdkgo.RunMutation(newTestDexContext("create"), client.CreateUser(), testConnection, validCreateUserInput())
	require.Equal(t, sdkgo.FailureProtocol, requireRetry(t, err).Kind)
	rejected, err := sdkgo.RunMutation(newTestDexContext("create"), client.CreateUser(), testConnection, validCreateUserInput())
	require.NoError(t, err)
	require.Equal(t, entraid.CreateUserBranchProviderRejected, rejected.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, rejected.Failure.Kind)
	require.Contains(t, rejected.Failure.Message, "Authorization_RequestDenied")
	requireSafeFailure(t, rejected.Failure)
}

func TestCreateUserRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	client := newTestClient(t, graph, entraid.Config{})
	for name, input := range map[string]entraid.CreateUserInput{
		"no domain":             {UserPrincipalName: "ada", DisplayName: "Ada"},
		"accent":                {UserPrincipalName: "adá@contoso.com", DisplayName: "Ada"},
		"period before at":      {UserPrincipalName: "ada.@contoso.com", DisplayName: "Ada"},
		"two at signs":          {UserPrincipalName: "ada@x@contoso.com", DisplayName: "Ada"},
		"local part too long":   {UserPrincipalName: strings.Repeat("a", 65) + "@contoso.com", DisplayName: "Ada"},
		"no names":              {UserPrincipalName: "ada@contoso.com"},
		"display name too long": {UserPrincipalName: "ada@contoso.com", DisplayName: strings.Repeat("A", 257)},
		"control character":     {UserPrincipalName: "ada@contoso.com", DisplayName: "Ada\nLovelace"},
		"mail alias with at":    {UserPrincipalName: "ada@contoso.com", DisplayName: "Ada", MailNickname: "ada@x"},
		"lowercase location":    {UserPrincipalName: "ada@contoso.com", DisplayName: "Ada", UsageLocation: "gb"},
		"invalid provisioning":  {UserPrincipalName: "ada@contoso.com", DisplayName: "Ada", ProvisioningKey: "hire 42"},
		"surname too long":      {UserPrincipalName: "ada@contoso.com", Surname: strings.Repeat("L", 65)},
		"department too long":   {UserPrincipalName: "ada@contoso.com", DisplayName: "Ada", Department: strings.Repeat("D", 65)},
	} {
		result, err := sdkgo.RunMutation(newTestDexContext("create"), client.CreateUser(), testConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, entraid.CreateUserBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}
	require.Empty(t, graph.Requests())
}
