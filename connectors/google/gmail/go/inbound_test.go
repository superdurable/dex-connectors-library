// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
)

func TestGetMessageDecodesHeadersAndBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/users/me/messages/message-1", request.URL.Path)
		require.Equal(t, "full", request.URL.Query().Get("format"))
		_, _ = response.Write([]byte(gmailMessageJSON("message-1", "thread-1", false, "hello", "<p>hello</p>")))
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(newGmailDexContext("read-message"), newGmailClient(t, server.URL).GetMessage(), gmailConnection, gmail.GetMessageInput{MessageID: "message-1"})
	require.NoError(t, err)
	require.Equal(t, gmail.GetMessageBranchRead, result.Branch)
	require.Equal(t, "thread-1", result.Value.ThreadID)
	require.Equal(t, "Sender <sender@example.com>", result.Value.From)
	require.Equal(t, "hello", result.Value.TextBody)
	require.Equal(t, "<p>hello</p>", result.Value.HTMLBody)
}

// TestGetMessageDecodesGmailBodyPadding accepts the padded base64url Gmail returns for most bodies,
// the unpadded form, and rejects malformed data.
func TestGetMessageDecodesGmailBodyPadding(t *testing.T) {
	for _, test := range []struct {
		name, data, wantText string
		wantBranch           sdkgo.BranchID
	}{
		{name: "two padding characters", data: "SGk=", wantText: "Hi", wantBranch: gmail.GetMessageBranchRead},
		{name: "one padding character", data: "SGVsbG8=", wantText: "Hello", wantBranch: gmail.GetMessageBranchRead},
		{name: "no padding needed", data: "SGV5", wantText: "Hey", wantBranch: gmail.GetMessageBranchRead},
		{name: "unpadded", data: "SGVsbG8", wantText: "Hello", wantBranch: gmail.GetMessageBranchRead},
		{name: "excess padding", data: "SGVsbG8==", wantBranch: gmail.GetMessageBranchInvalidResponse},
		{name: "padding inside", data: "SGk=SGk=", wantBranch: gmail.GetMessageBranchInvalidResponse},
		{name: "standard alphabet", data: "+/8=", wantBranch: gmail.GetMessageBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				_, _ = response.Write([]byte(`{"id":"message-1","threadId":"thread-1","internalDate":"1000","payload":{"mimeType":"text/plain",` +
					`"headers":[{"name":"Subject","value":"Welcome — read this"}],"body":{"size":5,"data":"` + test.data + `"}}}`))
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(newGmailDexContext("read-padded-"+test.name), newGmailClient(t, server.URL).GetMessage(), gmailConnection, gmail.GetMessageInput{MessageID: "message-1"})
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			if test.wantBranch == gmail.GetMessageBranchRead {
				require.Equal(t, test.wantText, result.Value.TextBody)
				require.Equal(t, "Welcome — read this", result.Value.Subject)
			}
		})
	}
}

func TestGetMessageClassifiesProviderAndResponseFailures(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body       string
		wantBranch sdkgo.BranchID
		wantKind   sdkgo.FailureKind
	}{
		{name: "provider rejected", status: http.StatusForbidden, body: `{}`, wantBranch: gmail.GetMessageBranchProviderRejected, wantKind: sdkgo.FailureAuthorization},
		{name: "invalid response", status: http.StatusOK, body: `{"id":`, wantBranch: gmail.GetMessageBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(test.status)
				_, _ = response.Write([]byte(test.body))
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(newGmailDexContext("read-message-"+test.name), newGmailClient(t, server.URL).GetMessage(), gmailConnection, gmail.GetMessageInput{MessageID: "message-1"})
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, test.wantKind, result.Failure.Kind)
		})
	}
}

// TestGetMessageCredentialFailuresRetryUnlessTerminal keeps defect for reconnection-only failures.
func TestGetMessageCredentialFailuresRetryUnlessTerminal(t *testing.T) {
	for _, test := range []struct {
		err        error
		isTerminal bool
	}{
		{err: sdkgo.NewReauthorizationRequiredError(errors.New("revoked")), isTerminal: true},
		{err: projectconfig.ErrReauthorizationRequired, isTerminal: true},
		{err: projectconfig.ErrObjectNotFound, isTerminal: true},
		{err: projectconfig.ErrConnectionNotSelected, isTerminal: true},
		{err: projectconfig.ErrCredentialBrokerRejected, isTerminal: true},
		{err: projectconfig.ErrCredentialBrokerUnavailable},
		{err: errors.New("connection is not configured")},
		{err: errors.New("unlisted resolve failure")},
	} {
		t.Run(test.err.Error(), func(t *testing.T) {
			client, err := gmail.New(gmail.Config{Endpoint: "http://127.0.0.1:1"}, &sequencedGmailCredentials{resolutions: []error{test.err}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(newGmailDexContext("read-message-credentials"), client.GetMessage(), gmailConnection, gmail.GetMessageInput{MessageID: "message-1"})
			if test.isTerminal {
				require.NoError(t, err)
				require.Equal(t, gmail.GetMessageBranchDefect, result.Branch)
				return
			}
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		})
	}
}

func TestReplyToMessageUsesThreadAndRFCReplyHeaders(t *testing.T) {
	var raw string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.Method + " " + request.URL.Path {
		case "GET /users/me/messages/message-2":
			_, _ = response.Write([]byte(gmailMessageJSON("message-2", "thread-1", true, "", "")))
		case "POST /users/me/messages/send":
			var payload map[string]string
			require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
			require.Equal(t, "thread-1", payload["threadId"])
			decoded, err := base64.RawURLEncoding.DecodeString(payload["raw"])
			require.NoError(t, err)
			raw = string(decoded)
			_, _ = response.Write([]byte(`{"id":"message-3","threadId":"thread-1"}`))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(newGmailDexContext("reply-message"), newGmailClient(t, server.URL).ReplyToMessage(), gmailConnection, gmail.ReplyToMessageInput{MessageID: "message-2", TextBody: "Processing complete"})
	require.NoError(t, err)
	require.Equal(t, gmail.ReplyToMessageBranchSent, result.Branch)
	require.Contains(t, raw, `To: "Sender" <sender@example.com>`)
	require.Contains(t, raw, "In-Reply-To: <message-2@example.com>")
	require.Contains(t, raw, "References: <root@example.com> <message-2@example.com>")
	require.Contains(t, raw, "Processing complete")
}

func TestPollingTriggersSeparateRootsAndRepliesAndSuppressRescans(t *testing.T) {
	listCalls := make(chan struct{}, 8)
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/users/me/messages":
			listCalls <- struct{}{}
			_, _ = response.Write([]byte(`{"messages":[{"id":"reply-1","threadId":"thread-1"},{"id":"root-1","threadId":"thread-1"}]}`))
		case "/users/me/messages/root-1":
			_, _ = response.Write([]byte(gmailMessageJSON("root-1", "thread-1", false, "", "")))
		case "/users/me/messages/reply-1":
			_, _ = response.Write([]byte(gmailMessageJSON("reply-1", "thread-1", true, "", "")))
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	client, err := gmail.New(gmail.Config{Endpoint: server.URL, PollInterval: time.Second}, sdkgo.StaticCredentialProvider[gmail.Credentials]{
		gmailConnection: {AccessToken: sdkgo.NewSecretString("gmail-token"), PrimaryEmail: "owner@example.com"},
	})
	require.NoError(t, err)
	connection, err := gmail.NewConnection(client, gmailConnection)
	require.NoError(t, err)

	rootEvents := make(chan sdkgo.TriggerEvent[gmail.MessageEvent], 2)
	rootRunner := gmail.NewMessageReceivedTrigger(gmail.MessageReceivedTriggerConfig{
		Connection: connection, ConnectionName: gmailConnection.Name, BindingName: "gmail-thread-start",
		Configuration: gmail.MessageReceivedTriggerConfiguration{MessageMatcher: gmail.MessageMatcher{SenderEmails: []string{"sender@example.com"}}},
		Target: sdkgo.TriggerTargetFunc[gmail.MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[gmail.MessageEvent]) error {
			rootEvents <- event
			return nil
		}),
	})
	replyEvents := make(chan sdkgo.TriggerEvent[gmail.MessageEvent], 2)
	replyRunner := gmail.NewReplyReceivedTrigger(gmail.ReplyReceivedTriggerConfig{
		Connection: connection, ConnectionName: gmailConnection.Name, BindingName: "gmail-thread-reply",
		Configuration: gmail.ReplyReceivedTriggerConfiguration{ReplyMatcher: gmail.MessageMatcher{MessageContains: "approval"}},
		Target: sdkgo.TriggerTargetFunc[gmail.MessageEvent](func(_ context.Context, event sdkgo.TriggerEvent[gmail.MessageEvent]) error {
			replyEvents <- event
			return nil
		}),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runnerErrors := make(chan error, 2)
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() { defer waitGroup.Done(); runnerErrors <- rootRunner.Run(ctx) }()
	go func() { defer waitGroup.Done(); runnerErrors <- replyRunner.Run(ctx) }()

	root := receiveTriggerEvent(t, rootEvents)
	reply := receiveTriggerEvent(t, replyEvents)
	require.Equal(t, "root-1", root.ID)
	require.False(t, root.Payload.IsReply)
	require.Equal(t, "reply-1", reply.ID)
	require.True(t, reply.Payload.IsReply)
	for range 4 {
		select {
		case <-listCalls:
		case <-time.After(4 * time.Second):
			t.Fatal("Gmail polling did not rescan")
		}
	}
	cancel()
	waitGroup.Wait()
	close(runnerErrors)
	for err := range runnerErrors {
		require.ErrorIs(t, err, context.Canceled)
	}
	require.Empty(t, rootEvents)
	require.Empty(t, replyEvents)
}

func receiveTriggerEvent(t *testing.T, events <-chan sdkgo.TriggerEvent[gmail.MessageEvent]) sdkgo.TriggerEvent[gmail.MessageEvent] {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(4 * time.Second):
		t.Fatal("Gmail Trigger event was not delivered")
		return sdkgo.TriggerEvent[gmail.MessageEvent]{}
	}
}

// gmailMessageJSON pads body data as Gmail does when a body's length is not a multiple of three.
func gmailMessageJSON(messageID string, threadID string, reply bool, textBody string, htmlBody string) string {
	headers := []map[string]string{
		{"name": "Message-ID", "value": "<" + messageID + "@example.com>"},
		{"name": "From", "value": "Sender <sender@example.com>"},
		{"name": "To", "value": "owner@example.com"},
		{"name": "Subject", "value": "Approval request"},
	}
	if reply {
		headers = append(headers,
			map[string]string{"name": "In-Reply-To", "value": "<root@example.com>"},
			map[string]string{"name": "References", "value": "<root@example.com>"},
		)
	}
	parts := []map[string]any{}
	if textBody != "" {
		parts = append(parts, map[string]any{"mimeType": "text/plain", "body": map[string]string{"data": base64.URLEncoding.EncodeToString([]byte(textBody))}})
	}
	if htmlBody != "" {
		parts = append(parts, map[string]any{"mimeType": "text/html", "body": map[string]string{"data": base64.URLEncoding.EncodeToString([]byte(htmlBody))}})
	}
	contents, err := json.Marshal(map[string]any{
		"id": messageID, "threadId": threadID, "labelIds": []string{"INBOX"},
		"snippet": "approval granted", "internalDate": "1000",
		"payload": map[string]any{"mimeType": "multipart/alternative", "headers": headers, "parts": parts},
	})
	if err != nil {
		panic(err)
	}
	return string(contents)
}
