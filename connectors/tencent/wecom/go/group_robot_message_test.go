// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wecom

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

var testRobotText = SendGroupRobotMessageInput{
	MessageType: GroupRobotMessageText, Content: "Deployment 42 finished",
	MentionedUserIDs: []string{"zhangsan", "@all"}, MentionedMobileNumbers: []string{"13800001111"},
}

func TestSendGroupRobotMessagePostsOnceThenConfirmsFromTheCheckpoint(t *testing.T) {
	var requestPath, requestKey, requestBody, contentType string
	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		require.Equal(t, http.MethodPost, r.Method)
		requestPath, requestKey, contentType = r.URL.Path, r.URL.Query().Get("key"), r.Header.Get("Content-Type")
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		requestBody = string(body)
		_, err = w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("robot-flow", "post-step")
	sent, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
	require.NoError(t, err)
	require.Equal(t, SendGroupRobotMessageBranchSent, sent.Branch)
	require.Equal(t, SentGroupRobotMessage{MessageType: GroupRobotMessageText, ContentBytes: len("Deployment 42 finished")}, sent.Value)
	require.Equal(t, "/cgi-bin/webhook/send", requestPath)
	require.Equal(t, testWebhookKey, requestKey)
	require.Equal(t, "application/json", contentType)
	require.JSONEq(t, `{"msgtype":"text","text":{"content":"Deployment 42 finished","mentioned_list":["zhangsan","@all"],"mentioned_mobile_list":["13800001111"]}}`, requestBody)

	// A later attempt of the same call reads the accepted checkpoint and never posts again.
	ctx.AdvanceAttempt()
	repeated, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
	require.NoError(t, err)
	require.Equal(t, SendGroupRobotMessageBranchSent, repeated.Branch)
	require.Equal(t, sent.Value, repeated.Value)
	require.Equal(t, 1, posts)
}

func TestSendGroupRobotMessageEncodesMarkdown(t *testing.T) {
	fake := newFakeWeCom(t)
	client := newTestClient(t, fake.server.URL)
	input := SendGroupRobotMessageInput{MessageType: GroupRobotMessageMarkdown, Content: "**Deploy** <font color=\"info\">done</font>"}
	sent, err := sdkgo.RunMutation(singledispatchtest.NewContext("markdown-flow", "post-step"), client.SendGroupRobotMessage(), testRobotConnection, input)
	require.NoError(t, err)
	require.Equal(t, SendGroupRobotMessageBranchSent, sent.Branch)
	require.JSONEq(t, `{"msgtype":"markdown","markdown":{"content":"**Deploy** <font color=\"info\">done</font>"}}`, fake.sendBodies[0])
}

// Retryable codes from https://developer.work.weixin.qq.com/document/path/90313: -1 system busy,
// 45009 frequency limit (a robot accepts 20 posts per minute), and 45033 concurrency limit.
func TestSendGroupRobotMessageRetriesWhatWeComDidNotProcess(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		reply      fakeReply
		kind       sdkgo.FailureKind
		retryAfter time.Duration
	}{
		{name: "system busy", reply: fakeReply{body: `{"errcode":-1,"errmsg":"system busy"}`}, kind: sdkgo.FailureAvailability},
		{name: "frequency limit", reply: fakeReply{body: `{"errcode":45009,"errmsg":"api freq out of limit"}`}, kind: sdkgo.FailureRateLimit, retryAfter: time.Minute},
		{name: "concurrency limit", reply: fakeReply{body: `{"errcode":45033,"errmsg":"api concurrent out of limit"}`}, kind: sdkgo.FailureRateLimit},
		{name: "HTTP 429", reply: fakeReply{status: http.StatusTooManyRequests}, kind: sdkgo.FailureRateLimit},
		{name: "HTTP 503", reply: fakeReply{status: http.StatusServiceUnavailable}, kind: sdkgo.FailureAvailability},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fake := newFakeWeCom(t)
			fake.sendReplies = []fakeReply{testCase.reply, {body: `{"errcode":0,"errmsg":"ok"}`}}
			client := newTestClient(t, fake.server.URL)
			ctx := singledispatchtest.NewContext("retry-flow-"+testCase.name, "post-step")
			_, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
			failure, retryAfter := requireRetry(t, err)
			require.Equal(t, testCase.kind, failure.Kind)
			require.Equal(t, testCase.retryAfter, retryAfter)
			requireSecretFree(t, failure.Message, err.Error())
			ctx.AdvanceAttempt()
			sent, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
			require.NoError(t, err)
			require.Equal(t, SendGroupRobotMessageBranchSent, sent.Branch)
			require.Equal(t, 2, fake.sendCount())
			require.Equal(t, fake.sendBodies[0], fake.sendBodies[1])
		})
	}
}

func TestSendGroupRobotMessageRetriesAnUndispatchedTransportFailure(t *testing.T) {
	client := newTestClient(t, unreachableBaseURL(t))
	ctx := singledispatchtest.NewContext("unreachable-flow", "post-step")
	_, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
	failure, _ := requireRetry(t, err)
	require.Equal(t, sdkgo.FailureTransport, failure.Kind)
	requireSecretFree(t, failure.Message, err.Error())

	fake := newFakeWeCom(t)
	ctx.AdvanceAttempt()
	sent, err := sdkgo.RunMutation(ctx, newTestClient(t, fake.server.URL).SendGroupRobotMessage(), testRobotConnection, testRobotText)
	require.NoError(t, err)
	require.Equal(t, SendGroupRobotMessageBranchSent, sent.Branch)
}

func TestSendGroupRobotMessageRetriesADNSFailureOfTheStandardTransport(t *testing.T) {
	client, err := New(Config{BaseURL: "https://wecom-connector-test.invalid"}, testCredentials(), WithHTTPClient(&http.Client{Transport: &http.Transport{}}))
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("dns-flow", "post-step")
	_, err = sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
	failure, _ := requireRetry(t, err)
	require.Equal(t, sdkgo.FailureTransport, failure.Kind)
	requireSecretFree(t, failure.Message, err.Error())
}

// sendThenFailTransport delivers the post to WeCom, then reports a dial-looking error without a connection trace.
type sendThenFailTransport struct{ delegate http.RoundTripper }

func (transport sendThenFailTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	// A fresh context drops the connector's httptrace, so GotConn never fires for this delivered post.
	response, err := transport.delegate.RoundTrip(request.Clone(context.Background()))
	if err == nil {
		// Closing the discarded response cannot change what the fake WeCom recorded.
		_ = response.Body.Close()
	}
	return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
}

// failBeforeSendingTransport reports a dial error without sending; a custom transport still cannot prove that.
type failBeforeSendingTransport struct{}

func (failBeforeSendingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
}

func TestSendGroupRobotMessageCountsACustomTransportErrorAsAPossibleSend(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		transport     func(*fakeWeCom) http.RoundTripper
		expectedPosts int
	}{
		{name: "error after sending", transport: func(fake *fakeWeCom) http.RoundTripper {
			return sendThenFailTransport{delegate: fake.server.Client().Transport}
		}, expectedPosts: 3},
		{name: "error before sending", transport: func(*fakeWeCom) http.RoundTripper { return failBeforeSendingTransport{} }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fake := newFakeWeCom(t)
			client, err := New(Config{BaseURL: fake.server.URL}, testCredentials(), WithHTTPClient(&http.Client{Transport: testCase.transport(fake)}))
			require.NoError(t, err)
			ctx := singledispatchtest.NewContext("custom-transport-flow", "post-step")
			for attempt := 1; attempt < 3; attempt++ {
				_, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
				failure, _ := requireRetry(t, err)
				requireSecretFree(t, failure.Message, err.Error())
				ctx.AdvanceAttempt()
			}
			uncertain, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
			require.NoError(t, err)
			require.Equal(t, SendGroupRobotMessageBranchUncertain, uncertain.Branch)
			requireSecretFree(t, uncertain.Failure.Message)
			ctx.AdvanceAttempt()
			repeated, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
			require.NoError(t, err)
			require.Equal(t, SendGroupRobotMessageBranchUncertain, repeated.Branch)
			require.Equal(t, testCase.expectedPosts, fake.sendCount(), "a custom transport error never permits a post past the send cap")
		})
	}
}

func TestUnsentDialErrorsAreOnlyDNSDialAndProxyConnectFailures(t *testing.T) {
	wrap := func(err error) error { return &url.Error{Op: "Post", URL: "https://example.invalid", Err: err} }
	require.True(t, isUnsentDialError(wrap(&net.OpError{Op: "dial", Err: errors.New("refused")})))
	require.True(t, isUnsentDialError(wrap(&net.OpError{Op: "proxyconnect", Err: errors.New("refused")})))
	require.True(t, isUnsentDialError(wrap(&net.OpError{Op: "dial", Err: &net.DNSError{Err: "no such host", Name: "example.invalid"}})))
	require.False(t, isUnsentDialError(wrap(&net.OpError{Op: "write", Err: errors.New("broken pipe")})))
	require.False(t, isUnsentDialError(wrap(&net.OpError{Op: "read", Err: errors.New("reset")})))
	require.False(t, isUnsentDialError(wrap(io.ErrUnexpectedEOF)))
	require.False(t, isUnsentDialError(wrap(context.DeadlineExceeded)))
	require.True(t, isStandardTransport(nil))
	require.True(t, isStandardTransport(&http.Transport{}))
	require.False(t, isStandardTransport(failBeforeSendingTransport{}))
}

// Rejection codes from https://developer.work.weixin.qq.com/document/path/90313.
func TestSendGroupRobotMessageSelectsProviderRejectedForDefiniteRefusals(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		reply   fakeReply
		errcode string
	}{
		{name: "invalid webhook or robot removed", reply: fakeReply{body: `{"errcode":93000,"errmsg":"invalid webhook url, hint: [key=` + testWebhookKey + `]"}`}, errcode: "93000"},
		{name: "robot disabled", reply: fakeReply{body: `{"errcode":93004,"errmsg":"webhook disabled"}`}, errcode: "93004"},
		{name: "group does not allow posts", reply: fakeReply{body: `{"errcode":93001,"errmsg":"not allowed"}`}, errcode: "93001"},
		{name: "content too large", reply: fakeReply{body: `{"errcode":45002,"errmsg":"content size out of limit"}`}, errcode: "45002"},
		{name: "invalid msgtype", reply: fakeReply{body: `{"errcode":40008,"errmsg":"invalid message type"}`}, errcode: "40008"},
		{name: "HTTP 404", reply: fakeReply{status: http.StatusNotFound}, errcode: "HTTP 404"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fake := newFakeWeCom(t)
			fake.sendReplies = []fakeReply{testCase.reply}
			client := newTestClient(t, fake.server.URL)
			rejected, err := sdkgo.RunMutation(singledispatchtest.NewContext("rejected-flow", "post-step"), client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
			require.NoError(t, err)
			require.Equal(t, SendGroupRobotMessageBranchProviderRejected, rejected.Branch)
			require.NotNil(t, rejected.Failure)
			require.Contains(t, rejected.Failure.Message, testCase.errcode)
			requireSecretFree(t, rejected.Failure.Message)
			require.Equal(t, 1, fake.sendCount())
		})
	}
}

var unconfirmedGroupRobotReplies = []struct {
	name  string
	reply fakeReply
}{
	{name: "HTTP 500", reply: fakeReply{status: http.StatusInternalServerError}},
	{name: "HTTP 502", reply: fakeReply{status: http.StatusBadGateway}},
	{name: "HTTP 408", reply: fakeReply{status: http.StatusRequestTimeout}},
	{name: "HTTP 409", reply: fakeReply{status: http.StatusConflict}},
	{name: "HTTP 425", reply: fakeReply{status: http.StatusTooEarly}},
	{name: "connection dropped after the request", reply: fakeReply{dropConnection: true}},
	{name: "malformed JSON", reply: fakeReply{body: `{"errcode":`}},
	{name: "missing errcode", reply: fakeReply{body: `{"errmsg":"ok"}`}},
	{name: "oversized body", reply: fakeReply{body: `{"errcode":0,"errmsg":"` + strings.Repeat("x", 300000) + `"}`}},
}

func TestSendGroupRobotMessageResendsAnUnconfirmedPostBelowTheSendCap(t *testing.T) {
	for _, testCase := range unconfirmedGroupRobotReplies {
		t.Run(testCase.name, func(t *testing.T) {
			fake := newFakeWeCom(t)
			fake.sendReplies = []fakeReply{testCase.reply, {body: `{"errcode":0,"errmsg":"ok"}`}}
			client := newTestClient(t, fake.server.URL)
			ctx := singledispatchtest.NewContext("resend-flow", "post-step")
			_, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
			failure, _ := requireRetry(t, err)
			requireSecretFree(t, failure.Message, err.Error())
			ctx.AdvanceAttempt()
			sent, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
			require.NoError(t, err)
			require.Equal(t, SendGroupRobotMessageBranchSent, sent.Branch)
			require.Equal(t, 2, fake.sendCount())
		})
	}
}

func TestSendGroupRobotMessageSelectsUncertainAtTheSendCap(t *testing.T) {
	for _, testCase := range unconfirmedGroupRobotReplies {
		t.Run(testCase.name, func(t *testing.T) {
			fake := newFakeWeCom(t)
			fake.sendReplies = []fakeReply{testCase.reply, testCase.reply, testCase.reply, {body: `{"errcode":0,"errmsg":"ok"}`}}
			client := newTestClient(t, fake.server.URL)
			ctx := singledispatchtest.NewContext("cap-flow", "post-step")
			for attempt := 1; attempt < 3; attempt++ {
				_, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
				requireRetry(t, err)
				ctx.AdvanceAttempt()
			}
			uncertain, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
			require.NoError(t, err)
			require.Equal(t, SendGroupRobotMessageBranchUncertain, uncertain.Branch)
			requireSecretFree(t, uncertain.Failure.Message)
			ctx.AdvanceAttempt()
			repeated, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
			require.NoError(t, err)
			require.Equal(t, SendGroupRobotMessageBranchUncertain, repeated.Branch)
			require.Equal(t, 3, fake.sendCount(), "no post goes past the send cap")
		})
	}
}

func TestSendGroupRobotMessageHonorsAnApplicationSendCapOfOne(t *testing.T) {
	fake := newFakeWeCom(t)
	fake.sendReplies = []fakeReply{{status: http.StatusInternalServerError}}
	client := newTestClient(t, fake.server.URL)
	uncertain, err := sdkgo.RunMutation(singledispatchtest.NewContext("cap-one-flow", "post-step"), client.SendGroupRobotMessage(), testRobotConnection, testRobotText, sdkgo.WithMaxSends(1))
	require.NoError(t, err)
	require.Equal(t, SendGroupRobotMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, fake.sendCount())
}

func TestSendGroupRobotMessageRecordsCanResendBeforeADefiniteRefusal(t *testing.T) {
	fake := newFakeWeCom(t)
	fake.sendReplies = []fakeReply{{body: `{"errcode":93000,"errmsg":"invalid webhook url"}`}}
	client := newTestClient(t, fake.server.URL)
	ctx := singledispatchtest.NewContext("refusal-checkpoint-flow", "post-step")
	rejected, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
	require.NoError(t, err)
	require.Equal(t, SendGroupRobotMessageBranchProviderRejected, rejected.Branch)
	heartbeats := ctx.Heartbeats()
	require.Contains(t, string(heartbeats[len(heartbeats)-1]), `"state":"canResend"`)
	require.Equal(t, 1, fake.sendCount())
}

func TestSendGroupRobotMessageSelectsUncertainForARefusalOfAResend(t *testing.T) {
	fake := newFakeWeCom(t)
	fake.sendReplies = []fakeReply{{status: http.StatusInternalServerError}, {body: `{"errcode":93000,"errmsg":"invalid webhook url"}`}}
	client := newTestClient(t, fake.server.URL)
	ctx := singledispatchtest.NewContext("refused-resend-flow", "post-step")
	_, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
	requireRetry(t, err)
	ctx.AdvanceAttempt()
	uncertain, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
	require.NoError(t, err)
	require.Equal(t, SendGroupRobotMessageBranchUncertain, uncertain.Branch)
	require.Equal(t, 2, fake.sendCount())
}

func TestSendGroupRobotMessageRetriesAnUnreadableCheckpoint(t *testing.T) {
	fake := newFakeWeCom(t)
	client := newTestClient(t, fake.server.URL)
	ctx := singledispatchtest.NewContext("fail-closed-flow", "post-step")
	ctx.SetReadError(errors.New("heartbeat unavailable"))
	_, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
	requireRetry(t, err)
	require.Equal(t, 0, fake.sendCount())
}

func TestSendGroupRobotMessageRetriesACredentialFailureAfterAPossibleSend(t *testing.T) {
	fake := newFakeWeCom(t)
	fake.sendReplies = []fakeReply{{status: http.StatusInternalServerError}}
	client := newTestClient(t, fake.server.URL)
	ctx := singledispatchtest.NewContext("credential-after-send-flow", "post-step")
	_, err := sdkgo.RunMutation(ctx, client.SendGroupRobotMessage(), testRobotConnection, testRobotText)
	requireRetry(t, err)
	ctx.AdvanceAttempt()
	noCredentials, err := New(Config{BaseURL: fake.server.URL}, sdkgo.StaticCredentialProvider[Credentials]{})
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(ctx, noCredentials.SendGroupRobotMessage(), testRobotConnection, testRobotText)
	requireRetry(t, err)
	require.Equal(t, 1, fake.sendCount())
}

func TestSendGroupRobotMessageRejectsInvalidInputBeforePosting(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		input SendGroupRobotMessageInput
	}{
		{name: "empty content", input: SendGroupRobotMessageInput{MessageType: GroupRobotMessageText}},
		{name: "text over 2048 bytes", input: SendGroupRobotMessageInput{MessageType: GroupRobotMessageText, Content: strings.Repeat("字", 683)}},
		{name: "markdown over 4096 bytes", input: SendGroupRobotMessageInput{MessageType: GroupRobotMessageMarkdown, Content: strings.Repeat("a", 4097)}},
		{name: "invalid UTF-8", input: SendGroupRobotMessageInput{MessageType: GroupRobotMessageText, Content: "\xff"}},
		{name: "unknown type", input: SendGroupRobotMessageInput{MessageType: "news", Content: "hello"}},
		{name: "markdown mentions", input: SendGroupRobotMessageInput{MessageType: GroupRobotMessageMarkdown, Content: "hi", MentionedUserIDs: []string{"zhangsan"}}},
		{name: "mention with separator", input: SendGroupRobotMessageInput{MessageType: GroupRobotMessageText, Content: "hi", MentionedUserIDs: []string{"a|b"}}},
		{name: "blank mention", input: SendGroupRobotMessageInput{MessageType: GroupRobotMessageText, Content: "hi", MentionedMobileNumbers: []string{" "}}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fake := newFakeWeCom(t)
			defect, err := sdkgo.RunMutation(singledispatchtest.NewContext("defect-flow", "post-step"), newTestClient(t, fake.server.URL).SendGroupRobotMessage(), testRobotConnection, testCase.input)
			require.NoError(t, err)
			require.Equal(t, SendGroupRobotMessageBranchDefect, defect.Branch)
			require.Equal(t, sdkgo.FailureValidation, defect.Failure.Kind)
			require.Zero(t, fake.sendCount())
		})
	}
	maximumText := SendGroupRobotMessageInput{MessageType: GroupRobotMessageText, Content: strings.Repeat("字", 682) + "ab"}
	require.Empty(t, validateGroupRobotInput(maximumText), "exactly 2048 bytes is accepted")
}

func TestSendGroupRobotMessageRequiresAGroupRobotConnection(t *testing.T) {
	fake := newFakeWeCom(t)
	client := newTestClient(t, fake.server.URL)
	defect, err := sdkgo.RunMutation(singledispatchtest.NewContext("method-flow", "post-step"), client.SendGroupRobotMessage(), testApplicationConnection, testRobotText)
	require.NoError(t, err)
	require.Equal(t, SendGroupRobotMessageBranchDefect, defect.Branch)
	require.Contains(t, defect.Failure.Message, "group-robot method")

	for _, webhookKey := range []string{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=" + testWebhookKey, "short", "key with spaces"} {
		credentials := sdkgo.StaticCredentialProvider[Credentials]{testRobotConnection: {AuthMethodID: groupRobotAuthMethodID, WebhookKey: sdkgo.NewSecretString(webhookKey)}}
		keyClient, err := New(Config{BaseURL: fake.server.URL}, credentials)
		require.NoError(t, err)
		defect, err := sdkgo.RunMutation(singledispatchtest.NewContext("key-flow", "post-step"), keyClient.SendGroupRobotMessage(), testRobotConnection, testRobotText)
		require.NoError(t, err)
		require.Equal(t, SendGroupRobotMessageBranchDefect, defect.Branch)
		require.NotContains(t, defect.Failure.Message, webhookKey)
	}
	require.Zero(t, fake.sendCount())
}
