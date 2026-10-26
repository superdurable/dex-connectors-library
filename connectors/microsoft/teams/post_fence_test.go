// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/teams"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestFencedPostRetries503OnceWithTheSameCallID(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			response.Header().Set("Retry-After", "2")
			writeJSON(t, response, http.StatusServiceUnavailable, graphErrorJSON("ServiceNotAvailable"))
			return
		}
		writeJSON(t, response, http.StatusCreated, chatMessageJSON(t, "1616990032035", "", testIncidentSubject, "html", testIncidentHTML, testCreatedAt))
	})
	client := newTeamsClient(t, provider.endpoint())
	stepContext := singledispatchtest.NewContext("teams-flow", "post-unavailable")

	_, err := sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	stepContext.AdvanceAttempt()
	result, err := sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.Equal(t, teams.PostChannelMessageBranchSent, result.Branch)
	require.Equal(t, 2, provider.countRequests(http.MethodPost))
	require.NotEmpty(t, provider.request(0).clientRequestID)
	require.Equal(t, provider.request(0).clientRequestID, provider.request(1).clientRequestID)
}

func TestFencedPostDoesNotResendFromAStaleCanResendMarker(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusServiceUnavailable, graphErrorJSON("ServiceNotAvailable"))
			return
		}
		writeJSON(t, response, http.StatusOK, collectionJSON(""))
	})
	client := newTeamsClient(t, provider.endpoint())
	stepContext := singledispatchtest.NewContext("teams-flow", "post-stale-marker")
	_, err := sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	stepContext.AdvanceAttempt()
	stepContext.AdvanceAttempt()
	result, err := sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Equal(t, 1, provider.countRequests(http.MethodPost))
}

func TestFencedPostLostClaimAndUnusableAcceptedBodyNeverResends(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusCreated, `{}`)
			return
		}
		writeJSON(t, response, http.StatusOK, collectionJSON(""))
	})
	client := newTeamsClient(t, provider.endpoint())
	stepContext := singledispatchtest.NewContext("teams-flow", "post-lost-claim")
	stepContext.LoseNextHeartbeat()

	_, err := sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	requireRetry(t, err, sdkgo.FailureProtocol)
	stepContext.AdvanceAttempt()
	result, err := sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Equal(t, 1, provider.countRequests(http.MethodPost))
	require.Equal(t, 1, provider.countRequests(http.MethodGet))
}

func TestFencedAcceptedPostReadsKnownIDOnLaterAttempt(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		message := chatMessageJSON(t, "1616990032035", "", testIncidentSubject, "html", testIncidentHTML, testCreatedAt)
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusCreated, message)
			return
		}
		require.Equal(t, testChannelPath+"/1616990032035", request.URL.Path)
		writeJSON(t, response, http.StatusOK, message)
	})
	client := newTeamsClient(t, provider.endpoint())
	stepContext := singledispatchtest.NewContext("teams-flow", "post-accepted")
	result, err := sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.Equal(t, teams.PostChannelMessageBranchSent, result.Branch)
	stepContext.AdvanceAttempt()
	result, err = sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.True(t, result.Value.IsConfirmedByReadBack)
	require.Equal(t, 1, provider.countRequests(http.MethodPost))
}

type intermittentTeamsCredentials struct {
	mutex    sync.Mutex
	attempts int
}

func (provider *intermittentTeamsCredentials) Resolve(sdkgo.Call) (teams.Credentials, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.attempts++
	if provider.attempts == 1 {
		return teams.Credentials{}, errors.New("credential store unavailable")
	}
	return teams.Credentials{AccessToken: sdkgo.NewSecretString(testAccessToken)}, nil
}

func (provider *intermittentTeamsCredentials) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[teams.Credentials]) (teams.Credentials, error) {
	return provider.Resolve(call)
}

func TestFencedCredentialFailureRecordsNotSentBeforeRetry(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, chatMessageJSON(t, "1616990032035", "", testIncidentSubject, "html", testIncidentHTML, testCreatedAt))
	})
	credentials := &intermittentTeamsCredentials{}
	client, err := teams.New(teams.Config{Endpoint: provider.endpoint()}, credentials)
	require.NoError(t, err)
	stepContext := singledispatchtest.NewContext("teams-flow", "post-credentials")
	_, err = sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	requireRetry(t, err, sdkgo.FailureAuthentication)
	require.Zero(t, provider.requestCount())
	stepContext.AdvanceAttempt()
	result, err := sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.Equal(t, teams.PostChannelMessageBranchSent, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestFencedPostCutsOffTheRequestBeforeItsAttemptDeadline(t *testing.T) {
	provider := newRecordingGraph(t, func(_ http.ResponseWriter, request *http.Request, _ int) {
		<-request.Context().Done()
	})
	client := newTeamsClient(t, provider.endpoint())
	stepContext := singledispatchtest.NewContext("teams-flow", "post-deadline")
	cancel := stepContext.SetDeadline(time.Now().Add(3 * time.Second))
	defer cancel()
	startedAt := time.Now()
	_, err := sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	require.Less(t, time.Since(startedAt), 2800*time.Millisecond)
	require.Equal(t, 1, provider.countRequests(http.MethodPost))
}

type replayProbeTransport struct {
	t    *testing.T
	base http.RoundTripper
}

func (transport replayProbeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost {
		require.Nil(transport.t, request.GetBody)
		request.Header.Set("Idempotency-Key", "probe-replay-prevention")
	}
	return transport.base.RoundTrip(request)
}

func TestFencedPostOnReusedConnectionIsNotTransparentlyReplayed(t *testing.T) {
	var mutex sync.Mutex
	postCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			response.WriteHeader(http.StatusOK)
			return
		}
		if _, err := io.Copy(io.Discard, request.Body); err != nil {
			t.Errorf("read request: %v", err)
		}
		mutex.Lock()
		postCount++
		mutex.Unlock()
		connection, _, err := response.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack response: %v", err)
			return
		}
		_ = connection.Close()
	}))
	defer server.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: replayProbeTransport{t: t, base: transport}}
	primeResponse, err := httpClient.Get(server.URL + "/prime")
	require.NoError(t, err)
	require.NoError(t, primeResponse.Body.Close())
	client, err := teams.New(teams.Config{Endpoint: server.URL + "/v1.0"}, staticTeamsCredentials(), teams.WithHTTPClient(httpClient))
	require.NoError(t, err)
	stepContext := singledispatchtest.NewContext("teams-flow", "post-reused-connection")
	_, err = sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	mutex.Lock()
	defer mutex.Unlock()
	require.Equal(t, 1, postCount)
}
