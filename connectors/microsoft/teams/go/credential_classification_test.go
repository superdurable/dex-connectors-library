// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/teams/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

// laterAttemptFailingTeamsCredentials resolves on attempt 1 and fails with err from attempt 2.
type laterAttemptFailingTeamsCredentials struct{ err error }

func (provider laterAttemptFailingTeamsCredentials) Resolve(call sdkgo.Call) (teams.Credentials, error) {
	if call.Context.Attempt() > 1 {
		return teams.Credentials{}, provider.err
	}
	return teams.Credentials{AccessToken: sdkgo.NewSecretString(testAccessToken)}, nil
}

func (provider laterAttemptFailingTeamsCredentials) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[teams.Credentials]) (teams.Credentials, error) {
	return provider.Resolve(call)
}

// alwaysFailingTeamsCredentials fails with err on every attempt.
type alwaysFailingTeamsCredentials struct{ err error }

func (provider alwaysFailingTeamsCredentials) Resolve(sdkgo.Call) (teams.Credentials, error) {
	return teams.Credentials{}, provider.err
}

func (provider alwaysFailingTeamsCredentials) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[teams.Credentials]) (teams.Credentials, error) {
	return provider.Resolve(call)
}

func TestFencedPostClassifiesEveryCredentialFailure(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		isTerminal bool
	}{
		{name: "sdk reauthorization", err: sdkgo.NewReauthorizationRequiredError(errors.New("revoked")), isTerminal: true},
		{name: "project reauthorization", err: projectconfig.ErrReauthorizationRequired, isTerminal: true},
		{name: "object not found", err: projectconfig.ErrObjectNotFound, isTerminal: true},
		{name: "connection not selected", err: projectconfig.ErrConnectionNotSelected, isTerminal: true},
		{name: "broker rejected", err: projectconfig.ErrCredentialBrokerRejected, isTerminal: true},
		{name: "broker unavailable", err: projectconfig.ErrCredentialBrokerUnavailable},
		{name: "rejected credential not refreshed", err: projectconfig.ErrRejectedCredentialNotRefreshed},
		{name: "static connection not configured", err: errors.New("connection is not configured")},
		{name: "deadline", err: context.DeadlineExceeded},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			idle := newRecordingGraph(t, func(http.ResponseWriter, *http.Request, int) { t.Error("no request is expected") })
			client, err := teams.New(teams.Config{Endpoint: idle.endpoint()}, alwaysFailingTeamsCredentials{err: testCase.err})
			require.NoError(t, err)
			result, err := sdkgo.RunMutation(singledispatchtest.NewContext("teams-flow", "credential-first"), client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
			if testCase.isTerminal {
				require.NoError(t, err)
				require.Equal(t, teams.PostChannelMessageBranchDefect, result.Branch)
			} else {
				requireRetry(t, err, sdkgo.FailureAuthentication)
			}

			unknown := newRecordingGraph(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusBadGateway, graphErrorJSON("BadGateway"))
			})
			client, err = teams.New(teams.Config{Endpoint: unknown.endpoint()}, laterAttemptFailingTeamsCredentials{err: testCase.err})
			require.NoError(t, err)
			stepContext := singledispatchtest.NewContext("teams-flow", "credential-after-send")
			_, err = sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
			requireRetry(t, err, sdkgo.FailureAvailability)
			stepContext.AdvanceAttempt()
			result, err = sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
			if testCase.isTerminal {
				require.NoError(t, err)
				require.Equal(t, teams.PostChannelMessageBranchUncertain, result.Branch)
			} else {
				requireRetry(t, err, sdkgo.FailureAuthentication)
			}
			require.Equal(t, 1, unknown.requestCount(), "a credential failure never sends")
		})
	}
}
