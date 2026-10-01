// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package workspaceadmin_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	workspaceadmin "github.com/superdurable/dex-connectors-library/connectors/google/workspace-admin"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var generatedPasswordPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func validCreateUserInput() workspaceadmin.CreateUserInput {
	return workspaceadmin.CreateUserInput{
		PrimaryEmail: "ada.lovelace@example.com", GivenName: " Ada ", FamilyName: "Lovelace",
		OrgUnitPath: "/Engineering", RecoveryEmail: "ada@personal.example", RecoveryPhone: "+16506661212",
	}
}

func TestCreateUserSendsAGeneratedPasswordThatNeverLeavesTheRequest(t *testing.T) {
	directory := newFakeDirectory(t)
	client := newTestClient(t, directory.URL)

	result, err := sdkgo.RunMutation(newTestDexContext("create"), client.CreateUser(), testConnection, validCreateUserInput())
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.CreateUserBranchCreated, result.Branch)
	require.False(t, result.Value.WasAlreadyCreated)
	require.Equal(t, "ada.lovelace@example.com", result.Value.User.PrimaryEmail)
	require.Equal(t, "/Engineering", result.Value.User.OrgUnitPath)
	require.True(t, result.Value.User.ChangePasswordAtNextLogin)
	require.Equal(t, result.Value.User.ID, result.Receipt.ProviderObjectID)

	requests := directory.recorded()
	require.Len(t, requests, 1)
	body := requests[0].body
	password, ok := body["password"].(string)
	require.True(t, ok)
	require.Regexp(t, generatedPasswordPattern, password)
	require.NotContains(t, body, "hashFunction")
	require.Equal(t, true, body["changePasswordAtNextLogin"])
	require.Equal(t, map[string]any{"givenName": "Ada", "familyName": "Lovelace"}, body["name"])
	require.Equal(t, "ada@personal.example", body["recoveryEmail"])
	require.Equal(t, "+16506661212", body["recoveryPhone"])
	require.Equal(t, []any{map[string]any{"type": "custom", "customType": "dexIdempotencyKey", "value": string(result.Receipt.IdempotencyKey)}}, body["externalIds"])
	require.Equal(t, string(result.Receipt.CallID), string(result.Receipt.IdempotencyKey), "a blank provisioning key uses the Call ID")

	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), password)

	second, err := sdkgo.RunMutation(newTestDexContext("create-another"), client.CreateUser(), testConnection,
		workspaceadmin.CreateUserInput{PrimaryEmail: "grace@example.com", GivenName: "Grace", FamilyName: "Hopper"})
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.CreateUserBranchCreated, second.Branch)
	secondRequest := directory.recorded()[1].body
	require.Equal(t, "/", secondRequest["orgUnitPath"], "a blank organizational unit uses the top level")
	require.NotEqual(t, password, secondRequest["password"], "every insert generates a new password")
	require.NotContains(t, secondRequest, "recoveryEmail")
}

func TestCreateUserRepeatedStepConvergesOnTheAccountItCreated(t *testing.T) {
	directory := newFakeDirectory(t)
	client := newTestClient(t, directory.URL)
	ctx := newTestDexContext("repeated-create")

	first, err := sdkgo.RunMutation(ctx, client.CreateUser(), testConnection, validCreateUserInput())
	require.NoError(t, err)
	repeated, err := sdkgo.RunMutation(ctx, client.CreateUser(), testConnection, validCreateUserInput())
	require.NoError(t, err)

	require.Equal(t, workspaceadmin.CreateUserBranchCreated, repeated.Branch)
	require.True(t, repeated.Value.WasAlreadyCreated)
	require.Equal(t, first.Value.User.ID, repeated.Value.User.ID)
	require.Equal(t, first.Receipt.IdempotencyKey, repeated.Receipt.IdempotencyKey)
	require.Equal(t, 1, directory.userCount())
	methods := []string{}
	for _, request := range directory.recorded() {
		methods = append(methods, request.method)
	}
	require.Equal(t, []string{http.MethodPost, http.MethodPost, http.MethodGet}, methods, "the second insert hits 409 and reads the account back")
}

func TestCreateUserLostResponseIsRetriedAndTheRetryFindsTheAccount(t *testing.T) {
	directory := newFakeDirectory(t)
	directory.setIntercept(func(request recordedRequest, index int) (int, any, bool) {
		if index != 0 {
			return 0, nil, false
		}
		// Google stores the account, then the response is lost behind a 503.
		directory.answer(request)
		return http.StatusServiceUnavailable, googleError(http.StatusServiceUnavailable, "backendError"), true
	})
	client := newTestClient(t, directory.URL)
	ctx := newTestDexContext("lost-response")

	_, err := sdkgo.RunMutation(ctx, client.CreateUser(), testConnection, validCreateUserInput())
	requireRetry(t, err)
	retried, err := sdkgo.RunMutation(ctx, client.CreateUser(), testConnection, validCreateUserInput())
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.CreateUserBranchCreated, retried.Branch)
	require.True(t, retried.Value.WasAlreadyCreated)
	require.Equal(t, 1, directory.userCount())
}

func TestCreateUserReportsAnAddressThisStepDidNotCreate(t *testing.T) {
	directory := newFakeDirectory(t)
	existing := directory.seedUser("ada.lovelace@example.com", []any{map[string]any{"type": "custom", "customType": "dexIdempotencyKey", "value": "another-step"}})
	client := newTestClient(t, directory.URL)

	result, err := sdkgo.RunMutation(newTestDexContext("existing"), client.CreateUser(), testConnection, validCreateUserInput())
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.CreateUserBranchAlreadyExists, result.Branch)
	require.Equal(t, existing["id"], result.Value.User.ID)
	require.False(t, result.Value.WasAlreadyCreated)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	requireSafeFailure(t, result.Failure)
	require.Equal(t, 1, directory.userCount())
}

func TestCreateUserWaitsForAConflictingAddressBeforeConcludingItIsNotAUser(t *testing.T) {
	directory := newFakeDirectory(t)
	directory.setIntercept(func(request recordedRequest, _ int) (int, any, bool) {
		if request.method == http.MethodPost {
			return http.StatusConflict, googleError(http.StatusConflict, "duplicate"), true
		}
		return 0, nil, false
	})
	client := newTestClient(t, directory.URL)

	_, err := sdkgo.RunMutation(newTestDexContext("conflict-early"), client.CreateUser(), testConnection, validCreateUserInput())
	require.Equal(t, sdkgo.FailureAvailability, requireRetry(t, err).Kind, "a just-created account may not be readable yet")

	lateAttempt := newTestDexContext("conflict-late")
	lateAttempt.firstAttemptAt = time.Now().Add(-2 * time.Minute)
	result, err := sdkgo.RunMutation(lateAttempt, client.CreateUser(), testConnection, validCreateUserInput())
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.CreateUserBranchAlreadyExists, result.Branch)
	require.Empty(t, result.Value.User.ID)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
}

func TestCreateUserProvisioningKeyConvergesAcrossStepExecutions(t *testing.T) {
	directory := newFakeDirectory(t)
	client := newTestClient(t, directory.URL)
	input := validCreateUserInput()
	input.ProvisioningKey = "hire-2026-0042"

	first, err := sdkgo.RunMutation(newTestDexContext("execution-1"), client.CreateUser(), testConnection, input)
	require.NoError(t, err)
	second, err := sdkgo.RunMutation(newTestDexContext("execution-2"), client.CreateUser(), testConnection, input)
	require.NoError(t, err)

	require.Equal(t, sdkgo.IdempotencyKey("hire-2026-0042"), first.Receipt.IdempotencyKey)
	require.Equal(t, workspaceadmin.CreateUserBranchCreated, second.Branch)
	require.True(t, second.Value.WasAlreadyCreated)
	require.Equal(t, first.Value.User.ID, second.Value.User.ID)
	require.Equal(t, 1, directory.userCount())
}

func TestCreateUserRetriesAnUnusableSuccessAndRejectsConclusiveErrors(t *testing.T) {
	directory := newFakeDirectory(t)
	directory.setIntercept(func(_ recordedRequest, index int) (int, any, bool) {
		if index == 0 {
			return http.StatusOK, map[string]any{"primaryEmail": "someone-else@example.com", "id": "1"}, true
		}
		return http.StatusPreconditionFailed, googleError(http.StatusPreconditionFailed, "conditionNotMet"), true
	})
	client := newTestClient(t, directory.URL)

	_, err := sdkgo.RunMutation(newTestDexContext("unusable"), client.CreateUser(), testConnection, validCreateUserInput())
	require.Equal(t, sdkgo.FailureProtocol, requireRetry(t, err).Kind)
	rejected, err := sdkgo.RunMutation(newTestDexContext("rejected"), client.CreateUser(), testConnection, validCreateUserInput())
	require.NoError(t, err)
	require.Equal(t, workspaceadmin.CreateUserBranchProviderRejected, rejected.Branch)
	require.Equal(t, sdkgo.FailureConflict, rejected.Failure.Kind)
	require.Equal(t, "provider rejected the request with HTTP 412 (conditionNotMet)", rejected.Failure.Message)
}

func TestCreateUserRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	directory := newFakeDirectory(t)
	client := newTestClient(t, directory.URL)
	for name, mutate := range map[string]func(*workspaceadmin.CreateUserInput){
		"missing address":       func(input *workspaceadmin.CreateUserInput) { input.PrimaryEmail = "" },
		"display-name address":  func(input *workspaceadmin.CreateUserInput) { input.PrimaryEmail = "Ada <ada@example.com>" },
		"missing given name":    func(input *workspaceadmin.CreateUserInput) { input.GivenName = " " },
		"long family name":      func(input *workspaceadmin.CreateUserInput) { input.FamilyName = strings.Repeat("x", 61) },
		"control character":     func(input *workspaceadmin.CreateUserInput) { input.FamilyName = "Love\u0007lace" },
		"relative org unit":     func(input *workspaceadmin.CreateUserInput) { input.OrgUnitPath = "Engineering" },
		"invalid recovery mail": func(input *workspaceadmin.CreateUserInput) { input.RecoveryEmail = "not-an-address" },
		"non-E.164 phone":       func(input *workspaceadmin.CreateUserInput) { input.RecoveryPhone = "650-666-1212" },
		"invalid key":           func(input *workspaceadmin.CreateUserInput) { input.ProvisioningKey = "hire 42" },
	} {
		input := validCreateUserInput()
		mutate(&input)
		result, err := sdkgo.RunMutation(newTestDexContext("invalid-create"), client.CreateUser(), testConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, workspaceadmin.CreateUserBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}
	require.Empty(t, directory.recorded())
}
