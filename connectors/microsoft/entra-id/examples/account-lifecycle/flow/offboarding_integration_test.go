//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountlifecycle

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id/internal/graphfake"
)

func TestReinstatingEnablesTheExistingAccountWithoutCreatingOneWithRealDex(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.SeedGroup(testGroupID)
	existingID := graph.SeedUser("ada.lovelace@contoso.com", false, nil)
	graph.SeedMember(testGroupID, existingID)
	flow, harness := newAccountLifecycleHarness(t, graph)
	ctx := integrationContext(t)

	flowID := startAccountLifecycle(t, ctx, harness.client, flow, "reinstate", Input{
		Action: AccountActionReinstate, UserPrincipalName: "ada.lovelace@contoso.com", GroupID: testGroupID,
	})
	handOff := waitForCompletedHandOff(t, ctx, harness.client, flowID)

	require.Equal(t, HandOffReinstated, handOff.Status)
	require.Equal(t, existingID, handOff.UserID)
	require.True(t, handOff.IsAccountEnabled)
	require.True(t, handOff.WasAlreadyMember)
	require.Equal(t, reinstatedAccountSignInHandOff, handOff.NextStep)
	require.Zero(t, graph.Count(http.MethodPost, "/v1.0/users"))
	require.Equal(t, 1, graph.Count(http.MethodPatch, "/v1.0/users/ada.lovelace@contoso.com"))
	require.Equal(t, 1, graph.MemberCount(testGroupID))
}

func TestOffboardingDisablesRevokesAndRemovesAfterALostResponseWithRealDex(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.SeedGroup(testGroupID)
	existingID := graph.SeedUser("ada.lovelace@contoso.com", true, nil)
	graph.SeedMember(testGroupID, existingID)
	isRemovalLost := false
	graph.AfterApply = func(request graphfake.Request, _ int, answer graphfake.Answer) graphfake.Answer {
		if !isRemovalLost && request.Method == http.MethodDelete {
			isRemovalLost = true
			return graphfake.Answer{Status: http.StatusServiceUnavailable, Body: graphfake.GraphError("serviceNotAvailable")}
		}
		return answer
	}
	flow, harness := newAccountLifecycleHarness(t, graph)
	ctx := integrationContext(t)

	flowID := startAccountLifecycle(t, ctx, harness.client, flow, "offboard", Input{
		Action: AccountActionOffboard, UserPrincipalName: "ada.lovelace@contoso.com", GroupID: testGroupID,
	})
	handOff := waitForCompletedHandOff(t, ctx, harness.client, flowID)

	require.Equal(t, HandOffOffboarded, handOff.Status)
	require.Equal(t, existingID, handOff.UserID)
	require.False(t, handOff.IsAccountEnabled)
	require.True(t, handOff.AreSessionsRevoked)
	require.True(t, handOff.WasAlreadyRemoved, "the retried removal found its own earlier delete")
	require.Equal(t, offboardedAccountHandOff, handOff.NextStep)
	stored := graph.User(existingID)
	require.Equal(t, false, stored["accountEnabled"])
	require.Equal(t, "2026-10-01T10:00:00Z", stored["signInSessionsValidFromDateTime"])
	require.Nil(t, stored["deleted"], "the account itself is never deleted")
	require.Zero(t, graph.MemberCount(testGroupID))
	require.Equal(t, 1, graph.Count(http.MethodPatch, "/v1.0/users/"+existingID), "offboarding disables by object ID")
	require.Equal(t, 1, graph.Count(http.MethodPost, "/v1.0/users/"+existingID+"/revokeSignInSessions"))
	require.Equal(t, 2, graph.Count(http.MethodDelete, "/v1.0/groups/"+testGroupID+"/members/"+existingID+"/$ref"))
}

func TestOffboardingAnUnknownAccountFailsOnTheUnwiredNotFoundBranchWithRealDex(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.SeedGroup(testGroupID)
	flow, harness := newAccountLifecycleHarness(t, graph)
	ctx := integrationContext(t)

	flowID := startAccountLifecycle(t, ctx, harness.client, flow, "offboard-unknown", Input{
		Action: AccountActionOffboard, UserPrincipalName: "nobody@contoso.com", GroupID: testGroupID,
	})
	waitForFailedFlow(t, ctx, harness.client, flowID)
	require.Equal(t, 1, graph.Count(http.MethodGet, "/v1.0/users/nobody@contoso.com"))
	require.Zero(t, graph.Count(http.MethodPatch, ""))
	require.Zero(t, graph.Count(http.MethodDelete, ""))
}

func TestAnInvalidRequestFailsBeforeAnyProviderRequestWithRealDex(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	flow, harness := newAccountLifecycleHarness(t, graph)
	ctx := integrationContext(t)
	input := onboardingInput()
	input.GroupID = "engineering@contoso.com"

	flowID := startAccountLifecycle(t, ctx, harness.client, flow, "invalid", input)
	waitForFailedFlow(t, ctx, harness.client, flowID)
	require.Empty(t, graph.Requests())
}
