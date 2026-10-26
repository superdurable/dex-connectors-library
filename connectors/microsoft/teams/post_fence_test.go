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

func TestFencedChatPostRetries503ExactlyOnce(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusServiceUnavailable, graphErrorJSON("ServiceNotAvailable"))
			return
		}
		writeJSON(t, response, http.StatusCreated, chatMessageJSON(t, "1616990032050", "", "", "text", testEscalation, testCreatedAt))
	})
	client := newTeamsClient(t, provider.endpoint())
	stepContext := singledispatchtest.NewContext("teams-flow", "chat-unavailable")
	_, err := sdkgo.RunMutation(stepContext, client.PostChatMessage(), teamsConnection, validChatMessageInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	stepContext.AdvanceAttempt()
	result, err := sdkgo.RunMutation(stepContext, client.PostChatMessage(), teamsConnection, validChatMessageInput())
	require.NoError(t, err)
	require.Equal(t, teams.PostChatMessageBranchSent, result.Branch)
	require.Equal(t, 2, provider.countRequests(http.MethodPost))
	require.Zero(t, provider.countRequests(http.MethodGet))
	require.Equal(t, provider.request(0).clientRequestID, provider.request(1).clientRequestID)
}

func TestFencedPostFailsClosedWhenHeartbeatReadFails(t *testing.T) {
	provider := newRecordingGraph(t, func(http.ResponseWriter, *http.Request, int) {
		t.Error("heartbeat read failure must prevent every provider request")
	})
	credentials := &intermittentTeamsCredentials{}
	client, err := teams.New(teams.Config{Endpoint: provider.endpoint()}, credentials)
	require.NoError(t, err)
	stepContext := singledispatchtest.NewContext("teams-flow", "heartbeat-read-failure")
	stepContext.SetReadError(errors.New("heartbeat unavailable"))
	result, err := sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Zero(t, provider.requestCount())
	require.Zero(t, credentials.attempts)
}

func TestFencedPostDoesNotResendWhenCanResendCheckpointFails(t *testing.T) {
	stepContext := singledispatchtest.NewContext("teams-flow", "can-resend-write-failure")
	provider := newRecordingGraph(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			stepContext.SetWriteError(errors.New("heartbeat write unavailable"))
			writeJSON(t, response, http.StatusServiceUnavailable, graphErrorJSON("ServiceNotAvailable"))
			return
		}
		writeJSON(t, response, http.StatusOK, collectionJSON(""))
	})
	client := newTeamsClient(t, provider.endpoint())
	_, err := sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	retry := requireRetry(t, err, sdkgo.FailureAvailability)
	require.Contains(t, retry.Failure.Message, "safe-resend checkpoint failed")
	stepContext.SetWriteError(nil)
	stepContext.AdvanceAttempt()
	result, err := sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Equal(t, 1, provider.countRequests(http.MethodPost))
	require.Equal(t, 1, provider.countRequests(http.MethodGet))
}

func TestFencedPostKnownIDReadBackFailuresNeverResend(t *testing.T) {
	for _, test := range []struct {
		name      string
		isReply   bool
		status    int
		readPath  string
		messageID string
		subject   string
		content   string
		replyToID string
	}{
		{name: "channel 404", status: http.StatusNotFound, readPath: testChannelPath + "/1616990032035", messageID: "1616990032035", subject: testIncidentSubject, content: testIncidentHTML},
		{name: "channel 503", status: http.StatusServiceUnavailable, readPath: testChannelPath + "/1616990032035", messageID: "1616990032035", subject: testIncidentSubject, content: testIncidentHTML},
		{name: "reply 404", isReply: true, status: http.StatusNotFound, readPath: testRepliesPath + "/1616990032036", messageID: "1616990032036", content: testStatusReply, replyToID: testRootID},
		{name: "reply 503", isReply: true, status: http.StatusServiceUnavailable, readPath: testRepliesPath + "/1616990032036", messageID: "1616990032036", content: testStatusReply, replyToID: testRootID},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingGraph(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodPost {
					writeJSON(t, response, http.StatusCreated, chatMessageJSON(t, test.messageID, test.replyToID, test.subject,
						map[bool]string{true: "text", false: "html"}[test.isReply], test.content, testCreatedAt))
					return
				}
				require.Equal(t, test.readPath, request.URL.Path)
				writeJSON(t, response, test.status, graphErrorJSON("ReadFailure"))
			})
			client := newTeamsClient(t, provider.endpoint())
			stepContext := singledispatchtest.NewContext("teams-flow", test.name)
			runPost := func() (sdkgo.MutationResult[teams.PostMessageOutput], error) {
				if test.isReply {
					return sdkgo.RunMutation(stepContext, client.PostThreadReply(), teamsConnection, validThreadReplyInput())
				}
				return sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
			}
			first, err := runPost()
			require.NoError(t, err)
			require.Equal(t, sdkgo.BranchID("sent"), first.Branch)
			stepContext.AdvanceAttempt()
			second, err := runPost()
			if test.status == http.StatusServiceUnavailable {
				requireRetry(t, err, sdkgo.FailureAvailability)
			} else {
				require.NoError(t, err)
				require.Equal(t, sdkgo.UncertainBranchID, second.Branch)
			}
			require.Equal(t, 1, provider.countRequests(http.MethodPost))
			require.Equal(t, 1, provider.countRequests(http.MethodGet))
		})
	}
}

func TestFencedPostConflictReadsBackWithoutResending(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusConflict, graphErrorJSON("Conflict"))
			return
		}
		require.Equal(t, testChannelPath, request.URL.Path)
		writeJSON(t, response, http.StatusOK, collectionJSON(""))
	})
	client := newTeamsClient(t, provider.endpoint())
	stepContext := singledispatchtest.NewContext("teams-flow", "post-conflict")
	_, err := sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	stepContext.AdvanceAttempt()
	result, err := sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Equal(t, 1, provider.countRequests(http.MethodPost))
	require.Equal(t, 1, provider.countRequests(http.MethodGet))
}

func TestFencedChatPostConflictIsUncertainWithoutResending(t *testing.T) {
	provider := newRecordingGraph(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusConflict, graphErrorJSON("Conflict"))
	})
	client := newTeamsClient(t, provider.endpoint())
	stepContext := singledispatchtest.NewContext("teams-flow", "chat-conflict")
	result, err := sdkgo.RunMutation(stepContext, client.PostChatMessage(), teamsConnection, validChatMessageInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	stepContext.AdvanceAttempt()
	result, err = sdkgo.RunMutation(stepContext, client.PostChatMessage(), teamsConnection, validChatMessageInput())
	require.NoError(t, err)
	require.Equal(t, sdkgo.UncertainBranchID, result.Branch)
	require.Equal(t, 1, provider.countRequests(http.MethodPost))
	require.Zero(t, provider.countRequests(http.MethodGet))
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

type reauthorizationRequiredTeamsCredentials struct{}

func (reauthorizationRequiredTeamsCredentials) Resolve(sdkgo.Call) (teams.Credentials, error) {
	return teams.Credentials{}, sdkgo.NewReauthorizationRequiredError(errors.New("authorization expired"))
}

func (provider reauthorizationRequiredTeamsCredentials) ResolveWithRefresh(
	_ context.Context,
	call sdkgo.Call,
	_ sdkgo.CredentialRefreshDriver[teams.Credentials],
) (teams.Credentials, error) {
	return provider.Resolve(call)
}

func TestFencedPostReauthorizationFailureRecordsNotSentAndSelectsDefect(t *testing.T) {
	provider := newRecordingGraph(t, func(http.ResponseWriter, *http.Request, int) {
		t.Error("reauthorization failure must prevent every provider request")
	})
	client, err := teams.New(teams.Config{Endpoint: provider.endpoint()}, reauthorizationRequiredTeamsCredentials{})
	require.NoError(t, err)
	stepContext := singledispatchtest.NewContext("teams-flow", "reauthorization-required")
	result, err := sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	require.NoError(t, err)
	require.Equal(t, teams.PostChannelMessageBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Contains(t, result.Failure.Message, "reconnect the connection")
	require.NotEmpty(t, stepContext.Heartbeats(), "the fence records notSent before selecting defect")
	require.Zero(t, provider.requestCount())
}

type reauthorizationAfterDispatchTeamsCredentials struct {
	mutex    sync.Mutex
	attempts int
}

func (provider *reauthorizationAfterDispatchTeamsCredentials) Resolve(sdkgo.Call) (teams.Credentials, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.attempts++
	if provider.attempts > 1 {
		return teams.Credentials{}, sdkgo.NewReauthorizationRequiredError(errors.New("authorization expired"))
	}
	return teams.Credentials{AccessToken: sdkgo.NewSecretString(testAccessToken)}, nil
}

func (provider *reauthorizationAfterDispatchTeamsCredentials) ResolveWithRefresh(
	_ context.Context,
	call sdkgo.Call,
	_ sdkgo.CredentialRefreshDriver[teams.Credentials],
) (teams.Credentials, error) {
	return provider.Resolve(call)
}

func TestFencedPostReauthorizationAfterAcceptedWriteIsUncertainWithoutReadBack(t *testing.T) {
	for _, operation := range []string{"channel", "reply", "chat"} {
		t.Run(operation, func(t *testing.T) {
			provider := newRecordingGraph(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				require.Equal(t, http.MethodPost, request.Method)
				writeJSON(t, response, http.StatusCreated, `{}`)
			})
			credentials := &reauthorizationAfterDispatchTeamsCredentials{}
			client, err := teams.New(teams.Config{Endpoint: provider.endpoint()}, credentials)
			require.NoError(t, err)
			stepContext := singledispatchtest.NewContext("teams-flow", "reauthorization-after-"+operation)
			first, err := runPostForReauthorizationTest(stepContext, client, operation)
			if operation == "chat" {
				require.NoError(t, err)
				require.Equal(t, sdkgo.UncertainBranchID, first.Branch)
			} else {
				requireRetry(t, err, sdkgo.FailureProtocol)
			}
			stepContext.AdvanceAttempt()
			second, err := runPostForReauthorizationTest(stepContext, client, operation)
			require.NoError(t, err)
			require.Equal(t, sdkgo.UncertainBranchID, second.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, second.Failure.Kind)
			require.Contains(t, second.Failure.Message, "reconnect the connection")
			require.Equal(t, 1, provider.countRequests(http.MethodPost))
			require.Zero(t, provider.countRequests(http.MethodGet))
		})
	}
}

func runPostForReauthorizationTest(stepContext *singledispatchtest.Context, client *teams.Client, operation string) (sdkgo.MutationResult[teams.PostMessageOutput], error) {
	switch operation {
	case "reply":
		return sdkgo.RunMutation(stepContext, client.PostThreadReply(), teamsConnection, validThreadReplyInput())
	case "chat":
		return sdkgo.RunMutation(stepContext, client.PostChatMessage(), teamsConnection, validChatMessageInput())
	default:
		return sdkgo.RunMutation(stepContext, client.PostChannelMessage(), teamsConnection, validChannelMessageInput())
	}
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
