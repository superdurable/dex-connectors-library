// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package slack_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/slack"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

type failingOnceSlackCredentials struct {
	attempts atomic.Int32
}

type slackCredentialResolution struct {
	credentials slack.Credentials
	err         error
}

type sequencedSlackCredentials struct {
	resolutions  []slackCredentialResolution
	resolveCalls int
}

func (provider *sequencedSlackCredentials) Resolve(sdkgo.Call) (slack.Credentials, error) {
	index := provider.resolveCalls
	provider.resolveCalls++
	if index >= len(provider.resolutions) {
		index = len(provider.resolutions) - 1
	}
	resolution := provider.resolutions[index]
	return resolution.credentials, resolution.err
}

func (provider *sequencedSlackCredentials) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[slack.Credentials]) (slack.Credentials, error) {
	return provider.Resolve(call)
}

func (provider *failingOnceSlackCredentials) Resolve(call sdkgo.Call) (slack.Credentials, error) {
	if provider.attempts.Add(1) == 1 {
		return slack.Credentials{}, errors.New("credential store unavailable")
	}
	return sdkgo.StaticCredentialProvider[slack.Credentials]{slackConnection: {
		BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token"),
	}}.Resolve(call)
}

func (provider *failingOnceSlackCredentials) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[slack.Credentials]) (slack.Credentials, error) {
	return provider.Resolve(call)
}

type failingCanResendSlackContext struct {
	*singledispatchtest.Context
	canResendFailures int
}

func (ctx *failingCanResendSlackContext) RecordHeartbeat(value any) error {
	if marker, ok := value.(singledispatch.Marker); ok && marker.State == singledispatch.StateCanResend {
		ctx.canResendFailures++
		return errors.New("can-resend heartbeat unavailable")
	}
	return ctx.Context.RecordHeartbeat(value)
}

func TestSharedFenceRetries503ExactlyOnce(t *testing.T) {
	var posts atomic.Int32
	var messageIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		var payload map[string]string
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		messageIDs = append(messageIDs, payload["client_msg_id"])
		if posts.Add(1) == 1 {
			response.WriteHeader(http.StatusServiceUnavailable)
			_, _ = response.Write([]byte(`{"ok":false,"error":"service_unavailable"}`))
			return
		}
		_, _ = response.Write([]byte(`{"ok":true,"channel":"C123","ts":"2.0"}`))
	}))
	t.Cleanup(server.Close)
	client := newSlackClient(t, server.URL)
	ctx := singledispatchtest.NewContext("shared-fence-503", "post")
	input := slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "hello"}
	_, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
	require.Error(t, err)
	require.EqualValues(t, 1, posts.Load())
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
	require.NoError(t, err)
	require.Equal(t, slack.PostThreadReplyBranchSent, result.Branch)
	require.EqualValues(t, 2, posts.Load())
	require.Len(t, messageIDs, 2)
	require.NotEmpty(t, messageIDs[0])
	require.Equal(t, messageIDs[0], messageIDs[1])
}

func TestSharedFenceBodyErrorsDoNotAuthorizeNon2xxResend(t *testing.T) {
	for _, statusCode := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusGatewayTimeout, http.StatusBadRequest} {
		for _, providerError := range []string{"service_unavailable", "ratelimited"} {
			t.Run(strconv.Itoa(statusCode)+"-"+providerError, func(t *testing.T) {
				posts := 0
				client := newSlackClientWithTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
					posts++
					return slackTestResponse(statusCode, `{"ok":false,"error":"`+providerError+`"}`), nil
				}))
				ctx := singledispatchtest.NewContext("shared-fence-status", "post")
				input := slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "hello"}
				result, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
				require.NoError(t, err)
				if statusCode >= 500 {
					require.Equal(t, slack.PostThreadReplyBranchUncertain, result.Branch)
				} else {
					require.Equal(t, slack.PostThreadReplyBranchProviderRejected, result.Branch)
				}
				require.Equal(t, 1, posts)
				heartbeats := ctx.Heartbeats()
				require.NotEmpty(t, heartbeats)
				require.Contains(t, string(heartbeats[len(heartbeats)-1]), `"state":"claimed"`)
				ctx.AdvanceAttempt()
				result, err = sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
				require.NoError(t, err)
				require.Equal(t, slack.PostThreadReplyBranchUncertain, result.Branch)
				require.Equal(t, 1, posts)
			})
		}
	}
}

func TestSharedFenceCredentialBodyRequires2xxRefusal(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		statusCode int
		body       string
		branch     sdkgo.BranchID
	}{
		{name: "server_error", statusCode: http.StatusInternalServerError, body: `{"ok":false,"error":"token_expired"}`, branch: slack.PostChannelMessageBranchUncertain},
		{name: "client_error", statusCode: http.StatusBadRequest, body: `{"ok":false,"error":"token_expired"}`, branch: slack.PostChannelMessageBranchProviderRejected},
		{name: "ok_true", statusCode: http.StatusOK, body: `{"ok":true,"error":"invalid_auth","channel":"C123","ts":"2.0"}`, branch: slack.PostChannelMessageBranchSent},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			posts := 0
			provider := &rejectionRefreshingCredentialProvider{}
			client, err := slack.New(slack.Config{}, provider, slack.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				posts++
				return slackTestResponse(testCase.statusCode, testCase.body), nil
			})}))
			require.NoError(t, err)
			ctx := singledispatchtest.NewContext("shared-fence-credential-status", "post")
			result, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"})
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.Equal(t, 1, posts)
			require.Zero(t, provider.forcedRefreshes)
		})
	}
}

func TestSharedFenceMalformed2xxCredentialResponseNeverResends(t *testing.T) {
	for _, testCase := range []struct {
		name string
		body string
	}{
		{name: "token_expired_type_mismatch", body: `{"ok":false,"error":"token_expired","ts":123}`},
		{name: "invalid_auth_type_mismatch", body: `{"ok":"false","error":"invalid_auth"}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			posts := 0
			provider := &rejectionRefreshingCredentialProvider{}
			client, err := slack.New(slack.Config{}, provider, slack.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				posts++
				return slackTestResponse(http.StatusOK, testCase.body), nil
			})}))
			require.NoError(t, err)
			ctx := singledispatchtest.NewContext("shared-fence-malformed-credential-"+testCase.name, "post")
			input := slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"}
			result, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, input)
			require.NoError(t, err)
			require.Equal(t, slack.PostChannelMessageBranchUncertain, result.Branch)
			require.Equal(t, 1, posts)
			require.Zero(t, provider.forcedRefreshes)
			heartbeats := ctx.Heartbeats()
			require.NotEmpty(t, heartbeats)
			require.Contains(t, string(heartbeats[len(heartbeats)-1]), `"state":"claimed"`)
			ctx.AdvanceAttempt()
			result, err = sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, input)
			require.NoError(t, err)
			require.Equal(t, slack.PostChannelMessageBranchUncertain, result.Branch)
			require.Equal(t, 1, posts)
			require.Zero(t, provider.forcedRefreshes)
		})
	}
}

func TestSharedFenceCredentialRefreshCheckpointFailureNeverResends(t *testing.T) {
	posts := 0
	provider := &rejectionRefreshingCredentialProvider{}
	client, err := slack.New(slack.Config{}, provider, slack.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		posts++
		return slackTestResponse(http.StatusOK, `{"ok":false,"error":"token_expired"}`), nil
	})}))
	require.NoError(t, err)
	ctx := &failingCanResendSlackContext{Context: singledispatchtest.NewContext("shared-fence-refresh-checkpoint", "post")}
	input := slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"}
	_, err = sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, input)
	var retryError *sdkgo.RetryError
	require.True(t, errors.As(err, &retryError))
	require.Equal(t, sdkgo.FailureAvailability, retryError.Failure.Kind)
	require.Equal(t, 1, posts)
	require.Equal(t, 1, provider.forcedRefreshes)
	require.Equal(t, 1, ctx.canResendFailures)
	heartbeats := ctx.Heartbeats()
	require.NotEmpty(t, heartbeats)
	require.Contains(t, string(heartbeats[len(heartbeats)-1]), `"state":"claimed"`)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, input)
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchUncertain, result.Branch)
	require.Equal(t, 1, posts)
	require.Equal(t, 1, provider.forcedRefreshes)
}

func TestSharedFenceCredentialFailureRecordsNotSent(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		posts.Add(1)
		_, _ = response.Write([]byte(`{"ok":true,"channel":"C123","ts":"2.0"}`))
	}))
	t.Cleanup(server.Close)
	provider := &failingOnceSlackCredentials{}
	client, err := slack.New(slack.Config{Endpoint: server.URL}, provider)
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("shared-fence-credentials", "post")
	input := slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"}
	_, err = sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, input)
	require.Error(t, err)
	require.Zero(t, posts.Load())
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, input)
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchSent, result.Branch)
	require.EqualValues(t, 1, posts.Load())
}

func TestSharedFenceTerminalPreSendCredentialsSelectDefect(t *testing.T) {
	for _, operationName := range []string{"postChannelMessage", "postThreadReply"} {
		for _, credentialFailure := range []string{"reauthorization_required", "missing_bot_token"} {
			t.Run(operationName+"/"+credentialFailure, func(t *testing.T) {
				resolution := slackCredentialResolution{credentials: slack.Credentials{UserToken: sdkgo.NewSecretString("user-token")}}
				if credentialFailure == "reauthorization_required" {
					resolution.err = sdkgo.NewReauthorizationRequiredError(errors.New("authorization revoked"))
				}
				provider := &sequencedSlackCredentials{resolutions: []slackCredentialResolution{resolution}}
				posts := 0
				client, err := slack.New(slack.Config{}, provider, slack.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
					posts++
					return slackTestResponse(http.StatusOK, `{"ok":true,"channel":"C123","ts":"2.0"}`), nil
				})}))
				require.NoError(t, err)
				ctx := singledispatchtest.NewContext("terminal-presend-"+operationName+"-"+credentialFailure, "post")
				var result sdkgo.MutationResult[slack.PostMessageOutput]
				if operationName == "postChannelMessage" {
					result, err = sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"})
					require.Equal(t, slack.PostChannelMessageBranchDefect, result.Branch)
				} else {
					result, err = sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "hello"})
					require.Equal(t, slack.PostThreadReplyBranchDefect, result.Branch)
				}
				require.NoError(t, err)
				require.NotNil(t, result.Failure)
				require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
				require.Contains(t, result.Failure.Message, "reconnect the connection")
				require.Zero(t, posts)
				require.Equal(t, 1, provider.resolveCalls)
				heartbeats := ctx.Heartbeats()
				require.NotEmpty(t, heartbeats)
				require.Contains(t, string(heartbeats[len(heartbeats)-1]), `"state":"notSent"`)
			})
		}
	}
}

func TestSharedFenceReadBackCredentialClassificationNeverResends(t *testing.T) {
	for _, testCase := range []struct {
		name              string
		postBody          string
		credentialFailure error
		isNextAttempt     bool
		wantsRetry        bool
	}{
		{name: "inline_missing_user_token", postBody: `{"ok":true,"ts":"2.0"}`},
		{name: "inline_reauthorization_required", postBody: `{"ok":false,"error":"internal_error","ts":"2.0"}`, credentialFailure: sdkgo.NewReauthorizationRequiredError(errors.New("authorization revoked"))},
		{name: "next_attempt_missing_user_token", postBody: `{"ok":true,"ts":"2.0"}`, isNextAttempt: true},
		{name: "next_attempt_reauthorization_required", postBody: `{"ok":false,"error":"internal_error","ts":"2.0"}`, credentialFailure: sdkgo.ErrReauthorizationRequired, isNextAttempt: true},
		{name: "inline_transient_lookup", postBody: `{"ok":true,"ts":"2.0"}`, credentialFailure: errors.New("credential store unavailable"), wantsRetry: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			botCredentials := slack.Credentials{BotToken: sdkgo.NewSecretString("bot-token"), UserToken: sdkgo.NewSecretString("user-token")}
			readResolution := slackCredentialResolution{credentials: slack.Credentials{BotToken: sdkgo.NewSecretString("bot-token")}, err: testCase.credentialFailure}
			resolutions := []slackCredentialResolution{{credentials: botCredentials}}
			if testCase.isNextAttempt {
				resolutions = append(resolutions, slackCredentialResolution{err: errors.New("credential store unavailable")})
			}
			resolutions = append(resolutions, readResolution)
			provider := &sequencedSlackCredentials{resolutions: resolutions}
			posts := 0
			reads := 0
			client, err := slack.New(slack.Config{}, provider, slack.WithHTTPClient(&http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.Method == http.MethodPost {
					posts++
					return slackTestResponse(http.StatusOK, testCase.postBody), nil
				}
				reads++
				return slackTestResponse(http.StatusOK, `{"ok":true,"messages":[]}`), nil
			})}))
			require.NoError(t, err)
			ctx := singledispatchtest.NewContext("read-credential-"+testCase.name, "post")
			input := slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "hello"}
			if testCase.isNextAttempt {
				_, err = sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
				var retryError *sdkgo.RetryError
				require.ErrorAs(t, err, &retryError)
				ctx.AdvanceAttempt()
			}
			result, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
			if testCase.wantsRetry {
				var retryError *sdkgo.RetryError
				require.ErrorAs(t, err, &retryError)
				require.Equal(t, sdkgo.FailureAvailability, retryError.Failure.Kind)
			} else {
				require.NoError(t, err)
				require.Equal(t, slack.PostThreadReplyBranchUncertain, result.Branch)
				require.NotNil(t, result.Failure)
				require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
				require.Contains(t, result.Failure.Message, "reconnect the connection")
				require.Equal(t, "2.0", result.Receipt.ProviderObjectID)
			}
			require.Equal(t, 1, posts)
			require.Zero(t, reads)
		})
	}
}

func TestSharedFenceLostClaimNeverResends(t *testing.T) {
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		posts.Add(1)
		response.WriteHeader(http.StatusInternalServerError)
		_, _ = response.Write([]byte(`{"ok":false,"error":"fatal_error"}`))
	}))
	t.Cleanup(server.Close)
	client := newSlackClient(t, server.URL)
	ctx := singledispatchtest.NewContext("shared-fence-lost-claim", "post")
	ctx.LoseNextHeartbeat()
	input := slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"}
	result, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, input)
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchUncertain, result.Branch)
	ctx.AdvanceAttempt()
	result, err = sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, input)
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchUncertain, result.Branch)
	require.EqualValues(t, 1, posts.Load())
}

func TestSharedFenceAcceptedPostReadsBackWithoutResend(t *testing.T) {
	var posts atomic.Int32
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			posts.Add(1)
			_, _ = response.Write([]byte(`{"ok":true,"channel":"C123","ts":"2.0"}`))
			return
		}
		reads.Add(1)
		require.Equal(t, "2.0", request.URL.Query().Get("oldest"))
		_, _ = response.Write([]byte(`{"ok":true,"messages":[{"ts":"2.0","thread_ts":"1.0","user":"UBOT","text":"hello"}]}`))
	}))
	t.Cleanup(server.Close)
	client := newSlackClient(t, server.URL)
	ctx := singledispatchtest.NewContext("shared-fence-readback", "post")
	input := slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "hello"}
	result, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
	require.NoError(t, err)
	require.Equal(t, slack.PostThreadReplyBranchSent, result.Branch)
	ctx.AdvanceAttempt()
	result, err = sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
	require.NoError(t, err)
	require.Equal(t, slack.PostThreadReplyBranchSent, result.Branch)
	require.EqualValues(t, 1, posts.Load())
	require.EqualValues(t, 1, reads.Load())
}

func TestSharedFenceInternalErrorReadBackRetriesWithoutResend(t *testing.T) {
	posts := 0
	reads := 0
	client := newSlackClientWithTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.Method == http.MethodPost {
			posts++
			return slackTestResponse(http.StatusOK, `{"ok":false,"error":"internal_error","ts":"2.0"}`), nil
		}
		reads++
		if reads == 1 {
			return slackTestResponse(http.StatusServiceUnavailable, `{"ok":false,"error":"service_unavailable"}`), nil
		}
		return slackTestResponse(http.StatusOK, `{"ok":true,"messages":[{"ts":"2.0","thread_ts":"1.0","text":"hello"}]}`), nil
	}))
	ctx := singledispatchtest.NewContext("shared-fence-internal-readback", "post")
	input := slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "hello"}
	_, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
	require.Error(t, err)
	require.Equal(t, 1, posts)
	require.Equal(t, 1, reads)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, input)
	require.NoError(t, err)
	require.Equal(t, slack.PostThreadReplyBranchSent, result.Branch)
	require.Equal(t, 1, posts)
	require.Equal(t, 2, reads)
}

func TestSharedFenceReadBackFailureClassificationNeverResends(t *testing.T) {
	for _, testCase := range []struct {
		name              string
		postBody          string
		readStatus        int
		readBody          string
		readError         error
		withoutUserToken  bool
		wantsRetry        bool
		wantsSent         bool
		wantsReadRequests int
	}{
		{name: "missing_read_credential", postBody: `{"ok":true,"ts":"2.0"}`, withoutUserToken: true},
		{name: "unusable_read_response", postBody: `{"ok":true,"ts":"2.0"}`, readStatus: http.StatusOK, readBody: `{"ok":`, wantsReadRequests: 1},
		{name: "read_transport_error", postBody: `{"ok":true,"ts":"2.0"}`, readError: errors.New("connection reset"), wantsRetry: true, wantsReadRequests: 1},
		{name: "read_rate_limited", postBody: `{"ok":true,"ts":"2.0"}`, readStatus: http.StatusTooManyRequests, readBody: `{"ok":false,"error":"ratelimited"}`, wantsRetry: true, wantsReadRequests: 1},
		{name: "read_server_error", postBody: `{"ok":true,"ts":"2.0"}`, readStatus: http.StatusInternalServerError, readBody: `{"ok":false,"error":"internal_error"}`, wantsRetry: true, wantsReadRequests: 1},
		{name: "read_client_error", postBody: `{"ok":true,"ts":"2.0"}`, readStatus: http.StatusBadRequest, readBody: `{"ok":false,"error":"invalid_arguments"}`, wantsReadRequests: 1},
		{name: "read_ok_false", postBody: `{"ok":true,"ts":"2.0"}`, readStatus: http.StatusOK, readBody: `{"ok":false,"error":"channel_not_found"}`, wantsReadRequests: 1},
		{name: "read_not_found", postBody: `{"ok":true,"ts":"2.0"}`, readStatus: http.StatusOK, readBody: `{"ok":true,"messages":[]}`, wantsReadRequests: 1},
		{name: "read_confirmed", postBody: `{"ok":true,"ts":"2.0"}`, readStatus: http.StatusOK, readBody: `{"ok":true,"messages":[{"ts":"2.0","thread_ts":"1.0","text":"hello"}]}`, wantsSent: true, wantsReadRequests: 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			posts := 0
			reads := 0
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.Method == http.MethodPost {
					posts++
					return slackTestResponse(http.StatusOK, testCase.postBody), nil
				}
				reads++
				if testCase.readError != nil {
					return nil, testCase.readError
				}
				return slackTestResponse(testCase.readStatus, testCase.readBody), nil
			})
			var client *slack.Client
			if testCase.withoutUserToken {
				var err error
				client, err = slack.New(slack.Config{}, sdkgo.StaticCredentialProvider[slack.Credentials]{slackConnection: {
					BotToken: sdkgo.NewSecretString("bot-token"),
				}}, slack.WithHTTPClient(&http.Client{Transport: transport}))
				require.NoError(t, err)
			} else {
				client = newSlackClientWithTransport(t, transport)
			}
			ctx := singledispatchtest.NewContext("shared-fence-read-"+testCase.name, "post")
			result, err := sdkgo.RunMutation(ctx, client.PostThreadReply(), slackConnection, slack.PostThreadReplyInput{ChannelID: "C123", ThreadTimestamp: "1.0", Text: "hello"})
			if testCase.wantsRetry {
				require.True(t, errors.As(err, new(*sdkgo.RetryError)))
			} else {
				require.NoError(t, err)
				if testCase.wantsSent {
					require.Equal(t, slack.PostThreadReplyBranchSent, result.Branch)
				} else {
					require.Equal(t, slack.PostThreadReplyBranchUncertain, result.Branch)
				}
			}
			require.Equal(t, 1, posts)
			require.Equal(t, testCase.wantsReadRequests, reads)
		})
	}
}

func slackTestResponse(statusCode int, body string) *http.Response {
	return &http.Response{StatusCode: statusCode, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

func TestSharedFencePreparesSingleUseHTTPWrite(t *testing.T) {
	requests := 0
	client := newSlackClientWithTransport(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		require.Nil(t, request.GetBody)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"ok":true,"channel":"C123","ts":"2.0"}`)),
		}, nil
	}))
	ctx := singledispatchtest.NewContext("shared-fence-http", "post")
	result, err := sdkgo.RunMutation(ctx, client.PostChannelMessage(), slackConnection, slack.PostChannelMessageInput{ChannelID: "C123", Text: "hello"})
	require.NoError(t, err)
	require.Equal(t, slack.PostChannelMessageBranchSent, result.Branch)
	require.Equal(t, 1, requests)
}
