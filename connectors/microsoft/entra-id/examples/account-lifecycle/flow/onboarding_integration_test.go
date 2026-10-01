//go:build integration

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package accountlifecycle

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id/internal/graphfake"
)

// slowFirst delays the first stateful answer to method and path by nine seconds, after Microsoft applied it.
func slowFirst(method string, path string) func(graphfake.Request, int, graphfake.Answer) graphfake.Answer {
	isDelayed := false
	return func(request graphfake.Request, _ int, answer graphfake.Answer) graphfake.Answer {
		if !isDelayed && request.Method == method && request.Path == path {
			isDelayed = true
			answer.Delay = 9 * time.Second
		}
		return answer
	}
}

// Dex dispatches a backup attempt for an async Execute that runs past about seven seconds.
func TestOnboardingSlowCreateBackupAttemptCreatesOneAccountWithRealDex(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.SeedGroup(testGroupID)
	graph.AfterApply = slowFirst(http.MethodPost, "/v1.0/users")
	flow, harness := newAccountLifecycleHarness(t, graph)
	ctx := integrationContext(t)

	flowID := startAccountLifecycle(t, ctx, harness.client, flow, "slow-create", onboardingInput())
	handOff := waitForCompletedHandOff(t, ctx, harness.client, flowID)

	require.Equal(t, HandOffOnboarded, handOff.Status)
	require.Equal(t, 1, graph.UserCount(), "every concurrent attempt converges on one account")
	require.True(t, graph.IsMember(testGroupID, handOff.UserID))
	creates := graph.Count(http.MethodPost, "/v1.0/users")
	require.LessOrEqual(t, creates, 2)
	require.Equal(t, creates-1, graph.Count(http.MethodGet, "/v1.0/users/ada.lovelace@contoso.com"), "a backup attempt hits the 400 and reads the name back")
	requireNoPasswordInFlow(t, ctx, harness.client, flow, flowID, graph)
	t.Logf("slow create: creates=%d readBacks=%d wasAlreadyCreated=%v", creates, creates-1, handOff.WasAlreadyCreated)
}

func TestOnboardingSlowMembershipBackupAttemptAddsOneMembershipWithRealDex(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.SeedGroup(testGroupID)
	graph.AfterApply = slowFirst(http.MethodPost, "/v1.0/groups/"+testGroupID+"/members/$ref")
	flow, harness := newAccountLifecycleHarness(t, graph)
	ctx := integrationContext(t)

	flowID := startAccountLifecycle(t, ctx, harness.client, flow, "slow-membership", onboardingInput())
	handOff := waitForCompletedHandOff(t, ctx, harness.client, flowID)

	require.Equal(t, HandOffOnboarded, handOff.Status)
	require.Equal(t, 1, graph.MemberCount(testGroupID), "every concurrent attempt converges on one membership")
	adds := graph.Count(http.MethodPost, "/members/$ref")
	require.LessOrEqual(t, adds, 2)
	require.Equal(t, adds-1, graph.Count(http.MethodGet, "/v1.0/groups/"+testGroupID+"/members"), "a backup attempt confirms the existing membership")
	requireNoPasswordInFlow(t, ctx, harness.client, flow, flowID, graph)
	t.Logf("slow membership: adds=%d wasAlreadyMember=%v", adds, handOff.WasAlreadyMember)
}

func TestOnboardingSurvivesALostCreateResponseAndReplicationLagWithRealDex(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.SeedGroup(testGroupID)
	isCreateLost := false
	graph.AfterApply = func(request graphfake.Request, _ int, answer graphfake.Answer) graphfake.Answer {
		if !isCreateLost && request.Method == http.MethodPost && request.Path == "/v1.0/users" {
			isCreateLost = true
			return graphfake.Answer{Status: http.StatusServiceUnavailable, Body: graphfake.GraphError("serviceNotAvailable")}
		}
		return answer
	}
	unreplicatedAdds := 0
	graph.Intercept = func(request graphfake.Request, _ int) (graphfake.Answer, bool) {
		if request.Method == http.MethodPost && strings.HasSuffix(request.Path, "/members/$ref") && unreplicatedAdds < 2 {
			unreplicatedAdds++
			return graphfake.Answer{Status: http.StatusNotFound, Body: graphfake.GraphError("Request_ResourceNotFound")}, true
		}
		return graphfake.Answer{}, false
	}
	flow, harness := newAccountLifecycleHarness(t, graph)
	ctx := integrationContext(t)

	flowID := startAccountLifecycle(t, ctx, harness.client, flow, "lost-create", onboardingInput())
	handOff := waitForCompletedHandOff(t, ctx, harness.client, flowID)

	require.Equal(t, HandOffOnboarded, handOff.Status)
	require.True(t, handOff.WasAlreadyCreated, "the retried Step found the account its first attempt created")
	require.True(t, handOff.IsAccountEnabled)
	require.Equal(t, newAccountSignInHandOff, handOff.NextStep)
	require.Equal(t, 1, graph.UserCount())
	require.Equal(t, 2, graph.Count(http.MethodPost, "/v1.0/users"))
	require.Equal(t, 3, graph.Count(http.MethodPost, "/members/$ref"), "two adds raced account replication and were retried")
	require.Equal(t, 2, graph.Count(http.MethodGet, "/v1.0/groups/"+testGroupID), "each 404 checked that the group exists")
	require.True(t, graph.IsMember(testGroupID, handOff.UserID))
	stored := graph.User(handOff.UserID)
	require.Equal(t, "Engineering", stored["department"])
	require.Equal(t, true, stored["passwordProfile"].(map[string]any)["forceChangePasswordNextSignIn"])
	requireNoPasswordInFlow(t, ctx, harness.client, flow, flowID, graph)
}

func TestOnboardingHandsOffANameThatBelongsToAnotherAccountWithRealDex(t *testing.T) {
	graph := graphfake.New(t.Cleanup)
	graph.SeedGroup(testGroupID)
	existingID := graph.SeedUser("ada.lovelace@contoso.com", false, nil)
	flow, harness := newAccountLifecycleHarness(t, graph)
	ctx := integrationContext(t)

	flowID := startAccountLifecycle(t, ctx, harness.client, flow, "address-taken", onboardingInput())
	handOff := waitForCompletedHandOff(t, ctx, harness.client, flowID)

	require.Equal(t, HandOffAddressTaken, handOff.Status)
	require.Equal(t, existingID, handOff.UserID)
	require.False(t, handOff.IsAccountEnabled)
	require.Equal(t, addressTakenHandOff, handOff.NextStep)
	require.Equal(t, 1, graph.UserCount())
	require.Zero(t, graph.Count(http.MethodPost, "/members/$ref"), "nothing is built for a name this Flow did not create")
}
