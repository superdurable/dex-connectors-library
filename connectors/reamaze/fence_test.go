// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze_test

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestCreateConversationFenceRetries503OnceWithTheSameKey(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
			return
		}
		writeValue(t, response, http.StatusCreated, conversationJSON("double-charge", 0, nil))
	})
	ctx := singledispatchtest.NewContext("reamaze-flow", "create-fence-503")
	client := newReamazeClient(t, provider.URL)
	_, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.NoError(t, err)
	require.Equal(t, reamaze.CreateConversationBranchCreated, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	var first, second map[string]any
	require.NoError(t, json.Unmarshal([]byte(provider.request(0).body), &first))
	require.NoError(t, json.Unmarshal([]byte(provider.request(1).body), &second))
	require.Equal(t, first["conversation"].(map[string]any)["data"], second["conversation"].(map[string]any)["data"])
}

func TestCreateConversationFenceDoesNotUseAStaleResendMarker(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
	})
	ctx := singledispatchtest.NewContext("reamaze-flow", "create-stale-marker")
	client := newReamazeClient(t, provider.URL)
	_, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	ctx.AdvanceAttempt()
	ctx.AdvanceAttempt()
	_, err = sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(1).method)
}

func TestCreateConversationFenceRecordsNotSentAfterCredentialFailure(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusCreated, conversationJSON("double-charge", 0, nil))
	})
	client, credentials := newSwitchingReamazeClient(t, provider.URL)
	credentials.resolveErr = errors.New("credential storage unavailable")
	ctx := singledispatchtest.NewContext("reamaze-flow", "create-credential-retry")
	_, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	require.Zero(t, provider.requestCount())
	credentials.resolveErr = nil
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.NoError(t, err)
	require.Equal(t, reamaze.CreateConversationBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestFencedWritesRejectUnusableCredentialsBeforeSending(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
	}{
		{name: "reauthorization required", err: sdkgo.ErrReauthorizationRequired},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusCreated, `{}`)
			})
			client, credentials := newSwitchingReamazeClient(t, provider.URL)
			credentials.resolveErr = test.err
			ctx := singledispatchtest.NewContext("reamaze-flow", "unusable-credentials")
			create, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
			require.NoError(t, err)
			require.Equal(t, reamaze.CreateConversationBranchDefect, create.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, create.Failure.Kind)
			require.Contains(t, create.Failure.Message, "reconnect")
			reply, err := sdkgo.RunMutation(singledispatchtest.NewContext("reamaze-flow", "unusable-reply"), client.ReplyToConversation(), reamazeConnection,
				reamaze.ReplyToConversationInput{ConversationID: "double-charge", Text: "Note."})
			require.NoError(t, err)
			require.Equal(t, reamaze.ReplyToConversationBranchDefect, reply.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, reply.Failure.Kind)
			require.Zero(t, provider.requestCount())
		})
	}
}

func TestFencedWritesRejectInvalidStoredCredentialsBeforeSending(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{}`)
	})
	credentials := sdkgo.StaticCredentialProvider[reamaze.Credentials]{reamazeConnection: {
		Email: testEmail, APIToken: sdkgo.NewSecretString("invalid token with spaces"),
	}}
	client, err := reamaze.New(reamaze.Config{Brand: testBrand}, credentials, reamaze.WithAPIBaseURL(provider.URL+"/api/v1"))
	require.NoError(t, err)
	create, err := sdkgo.RunMutation(singledispatchtest.NewContext("reamaze-flow", "invalid-create-credentials"), client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.NoError(t, err)
	require.Equal(t, reamaze.CreateConversationBranchDefect, create.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, create.Failure.Kind)
	require.Contains(t, create.Failure.Message, "reconnect")
	reply, err := sdkgo.RunMutation(singledispatchtest.NewContext("reamaze-flow", "invalid-reply-credentials"), client.ReplyToConversation(), reamazeConnection,
		reamaze.ReplyToConversationInput{ConversationID: "double-charge", Text: "Note."})
	require.NoError(t, err)
	require.Equal(t, reamaze.ReplyToConversationBranchDefect, reply.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, reply.Failure.Kind)
	require.Zero(t, provider.requestCount())
}

func TestCreateConversationReadsBackUnusableAcceptedResponsesInTheSameAttempt(t *testing.T) {
	for _, test := range []struct {
		name        string
		body        string
		isTruncated bool
		readStatus  int
		isFound     bool
		wantBranch  sdkgo.BranchID
		wantRetry   bool
	}{
		{name: "malformed confirmed", body: `{"subject":"accepted"}`, isFound: true, wantBranch: reamaze.CreateConversationBranchCreated},
		{name: "oversized confirmed", body: strings.Repeat("x", 4096), isFound: true, wantBranch: reamaze.CreateConversationBranchCreated},
		{name: "credential reflected confirmed", body: `{"echo":"` + testAPIToken + `"}`, isFound: true, wantBranch: reamaze.CreateConversationBranchCreated},
		{name: "truncated confirmed", isTruncated: true, isFound: true, wantBranch: reamaze.CreateConversationBranchCreated},
		{name: "malformed not found", body: `{"subject":"accepted"}`, wantBranch: reamaze.CreateConversationBranchUncertain},
		{name: "malformed transient read", body: `{"subject":"accepted"}`, readStatus: http.StatusServiceUnavailable, wantRetry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var key string
			provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodPost {
					var payload struct {
						Conversation struct {
							Data map[string]string `json:"data"`
						} `json:"conversation"`
					}
					require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
					key = payload.Conversation.Data[reamaze.DispatchKeyDataAttribute]
					if test.isTruncated {
						connection, _, err := response.(http.Hijacker).Hijack()
						require.NoError(t, err)
						_, err = io.WriteString(connection, "HTTP/1.1 201 Created\r\nContent-Length: 50\r\n\r\n{\"slug\":\"short\"}")
						require.NoError(t, err)
						require.NoError(t, connection.Close())
						return
					}
					writeJSON(t, response, http.StatusCreated, test.body)
					return
				}
				if test.readStatus != 0 {
					writeJSON(t, response, test.readStatus, `{}`)
					return
				}
				conversations := []any{}
				if test.isFound {
					conversation := conversationJSON("double-charge", 0, nil)
					conversation["data"] = map[string]string{reamaze.DispatchKeyDataAttribute: key}
					conversations = append(conversations, conversation)
				}
				writeValue(t, response, http.StatusOK, pageJSON("conversations", conversations, 1))
			})
			client, err := reamaze.New(reamaze.Config{Brand: testBrand, MaxResponseBytes: 1024}, testCredentialProvider(),
				reamaze.WithAPIBaseURL(provider.URL+"/api/v1"))
			require.NoError(t, err)
			ctx := singledispatchtest.NewContext("reamaze-flow", "accepted-unusable")
			result, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
			if test.wantRetry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				ctx.AdvanceAttempt()
				_, err = sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 3, provider.requestCount())
				require.Equal(t, http.MethodGet, provider.request(2).method)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.wantBranch, result.Branch)
				require.Equal(t, 2, provider.requestCount())
			}
			require.Equal(t, http.MethodGet, provider.request(1).method)
		})
	}
}

func TestReplyReadsBackUnusableAcceptedResponsesInTheSameAttempt(t *testing.T) {
	for _, test := range []struct {
		name        string
		body        string
		isTruncated bool
		readStatus  int
		isFound     bool
		wantBranch  sdkgo.BranchID
		wantRetry   bool
	}{
		{name: "malformed confirmed", body: `{"body":"accepted"}`, isFound: true, wantBranch: reamaze.ReplyToConversationBranchReplied},
		{name: "oversized confirmed", body: strings.Repeat("x", 4096), isFound: true, wantBranch: reamaze.ReplyToConversationBranchReplied},
		{name: "credential reflected confirmed", body: `{"echo":"` + testAPIToken + `"}`, isFound: true, wantBranch: reamaze.ReplyToConversationBranchReplied},
		{name: "truncated confirmed", isTruncated: true, isFound: true, wantBranch: reamaze.ReplyToConversationBranchReplied},
		{name: "malformed not found", body: `{"body":"accepted"}`, wantBranch: reamaze.ReplyToConversationBranchUncertain},
		{name: "malformed transient read", body: `{"body":"accepted"}`, readStatus: http.StatusServiceUnavailable, wantRetry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var originID string
			provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodPost {
					var payload struct {
						Message struct {
							OriginID string `json:"origin_id"`
						} `json:"message"`
					}
					require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
					originID = payload.Message.OriginID
					if test.isTruncated {
						connection, _, err := response.(http.Hijacker).Hijack()
						require.NoError(t, err)
						_, err = io.WriteString(connection, "HTTP/1.1 201 Created\r\nContent-Length: 50\r\n\r\n{\"body\":\"short\"}")
						require.NoError(t, err)
						require.NoError(t, connection.Close())
						return
					}
					writeJSON(t, response, http.StatusCreated, test.body)
					return
				}
				if test.readStatus != 0 {
					writeJSON(t, response, test.readStatus, `{}`)
					return
				}
				messages := []any{}
				if test.isFound {
					messages = append(messages, messageJSON("Note.", 1, originID))
				}
				writeValue(t, response, http.StatusOK, pageJSON("messages", messages, 1))
			})
			client, err := reamaze.New(reamaze.Config{Brand: testBrand, MaxResponseBytes: 1024}, testCredentialProvider(),
				reamaze.WithAPIBaseURL(provider.URL+"/api/v1"))
			require.NoError(t, err)
			ctx := singledispatchtest.NewContext("reamaze-flow", "accepted-reply-unusable")
			input := reamaze.ReplyToConversationInput{ConversationID: "double-charge", Text: "Note.", IsInternalNote: true}
			result, err := sdkgo.RunMutation(ctx, client.ReplyToConversation(), reamazeConnection, input)
			if test.wantRetry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				ctx.AdvanceAttempt()
				_, err = sdkgo.RunMutation(ctx, client.ReplyToConversation(), reamazeConnection, input)
				require.ErrorAs(t, err, &retry)
				require.Equal(t, 3, provider.requestCount())
				require.Equal(t, http.MethodGet, provider.request(2).method)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.wantBranch, result.Branch)
				require.Equal(t, 2, provider.requestCount())
			}
			require.Equal(t, http.MethodGet, provider.request(1).method)
		})
	}
}

func TestCustomTransportDialErrorAfterSendingDoesNotPermitResend(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			writeValue(t, response, http.StatusCreated, conversationJSON("double-charge", 0, nil))
			return
		}
		writeValue(t, response, http.StatusOK, pageJSON("conversations", []any{}, 0))
	})
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		response, err := http.DefaultTransport.RoundTrip(request)
		if request.Method == http.MethodPost && err == nil {
			require.NoError(t, response.Body.Close())
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("later dial failed")}
		}
		return response, err
	})
	client, err := reamaze.New(reamaze.Config{Brand: testBrand}, testCredentialProvider(),
		reamaze.WithAPIBaseURL(provider.URL+"/api/v1"), reamaze.WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("reamaze-flow", "dial-after-send")
	_, err = sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.NoError(t, err)
	require.Equal(t, reamaze.CreateConversationBranchUncertain, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(1).method)
}

func TestCreateConversationFenceReadsBackAcceptedWriteWithoutResending(t *testing.T) {
	var key string
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			var payload struct {
				Conversation struct {
					Data map[string]string `json:"data"`
				} `json:"conversation"`
			}
			require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
			key = payload.Conversation.Data[reamaze.DispatchKeyDataAttribute]
			writeValue(t, response, http.StatusCreated, conversationJSON("double-charge", 0, nil))
			return
		}
		conversation := conversationJSON("double-charge", 0, nil)
		conversation["data"] = map[string]string{reamaze.DispatchKeyDataAttribute: key}
		writeValue(t, response, http.StatusOK, conversation)
	})
	ctx := singledispatchtest.NewContext("reamaze-flow", "create-accepted")
	client := newReamazeClient(t, provider.URL)
	_, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.NoError(t, err)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.NoError(t, err)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(1).method)
}

func TestCreateConversationFenceStopsBeforeAnExpiredAttemptDeadline(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusCreated, conversationJSON("double-charge", 0, nil))
	})
	ctx := singledispatchtest.NewContext("reamaze-flow", "create-expired-deadline")
	cancel := ctx.SetDeadline(time.Now().Add(-time.Second))
	defer cancel()
	_, err := sdkgo.RunMutation(ctx, newReamazeClient(t, provider.URL).CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	require.Zero(t, provider.requestCount())
}

func TestReplyFenceRetries503WithTheSameOriginID(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
			return
		}
		var payload struct {
			Message struct {
				OriginID string `json:"origin_id"`
			} `json:"message"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		writeValue(t, response, http.StatusCreated, messageJSON("Dex triage note.", 1, payload.Message.OriginID))
	})
	ctx := singledispatchtest.NewContext("reamaze-flow", "reply-fence-503")
	client := newReamazeClient(t, provider.URL)
	input := reamaze.ReplyToConversationInput{ConversationID: "double-charge", Text: "Dex triage note.", IsInternalNote: true}
	_, err := sdkgo.RunMutation(ctx, client.ReplyToConversation(), reamazeConnection, input)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.ReplyToConversation(), reamazeConnection, input)
	require.NoError(t, err)
	require.Equal(t, reamaze.ReplyToConversationBranchReplied, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	var first, second map[string]any
	require.NoError(t, json.Unmarshal([]byte(provider.request(0).body), &first))
	require.NoError(t, json.Unmarshal([]byte(provider.request(1).body), &second))
	require.Equal(t, first["message"].(map[string]any)["origin_id"], second["message"].(map[string]any)["origin_id"])
}

func TestReplyFenceLostClaimFailsClosedWithoutAnotherPost(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			dropConnection(t, response)
			return
		}
		writeValue(t, response, http.StatusOK, pageJSON("messages", []any{}, 0))
	})
	ctx := singledispatchtest.NewContext("reamaze-flow", "reply-lost-claim")
	ctx.LoseNextHeartbeat()
	client := newReamazeClient(t, provider.URL)
	input := reamaze.ReplyToConversationInput{ConversationID: "double-charge", Text: "Dex triage note."}
	_, err := sdkgo.RunMutation(ctx, client.ReplyToConversation(), reamazeConnection, input)
	require.Error(t, err)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.ReplyToConversation(), reamazeConnection, input)
	require.NoError(t, err)
	require.Equal(t, reamaze.ReplyToConversationBranchUncertain, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(1).method)
}

func TestCreateConversationFenceCutsOffAMidRequestDeadline(t *testing.T) {
	provider := newRecordingReamaze(t, func(_ http.ResponseWriter, request *http.Request, _ int) {
		<-request.Context().Done()
	})
	ctx := singledispatchtest.NewContext("reamaze-flow", "create-mid-request-deadline")
	cancel := ctx.SetDeadline(time.Now().Add(400 * time.Millisecond))
	defer cancel()
	startedAt := time.Now()
	_, err := sdkgo.RunMutation(ctx, newReamazeClient(t, provider.URL).CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	require.Less(t, time.Since(startedAt), 400*time.Millisecond)
	require.Equal(t, 1, provider.requestCount())
}

func TestFencedWriteIsNotReplayedAfterAReusedConnectionLosesItsReply(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeValue(t, response, http.StatusOK, pageJSON("contacts", []any{}, 0))
			return
		}
		dropConnection(t, response)
	})
	transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			require.Nil(t, request.GetBody)
			request.Header.Set("Idempotency-Key", "replay-probe")
		}
		return http.DefaultTransport.RoundTrip(request)
	})
	client, err := reamaze.New(reamaze.Config{Brand: testBrand}, testCredentialProvider(),
		reamaze.WithAPIBaseURL(provider.URL+"/api/v1"), reamaze.WithHTTPClient(&http.Client{Transport: transport}))
	require.NoError(t, err)
	_, err = sdkgo.RunQuery(newReamazeDexContext("prime-connection"), client.FindContactByEmail(), reamazeConnection,
		reamaze.FindContactByEmailInput{Email: "jane@acme.example.com"})
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(singledispatchtest.NewContext("reamaze-flow", "create-reused-connection"), client.CreateConversation(), reamazeConnection, validCreateConversationInput())
	require.Error(t, err)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodPost, provider.request(1).method)
}
