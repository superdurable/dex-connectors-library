// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

func TestGetMessageDecodesHeadersAndBodies(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/users/me/messages/message-1", request.URL.Path)
		require.Equal(t, "full", request.URL.Query().Get("format"))
		_, _ = response.Write([]byte(gmailMessageJSON("message-1", "thread-1", false, "hello", "<p>hello</p>")))
	}))
	defer server.Close()
	result, err := connector.RunQuery(newGmailDexContext("read-message"), newGmailClient(t, server.URL).GetMessage(), gmailConnection, gmail.GetMessageInput{MessageID: "message-1"})
	require.NoError(t, err)
	require.Equal(t, gmail.GetMessageBranchRead, result.Branch)
	require.Equal(t, "thread-1", result.Value.ThreadID)
	require.Equal(t, "Sender <sender@example.com>", result.Value.From)
	require.Equal(t, "hello", result.Value.TextBody)
	require.Equal(t, "<p>hello</p>", result.Value.HTMLBody)
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
	result, err := connector.RunMutation(newGmailDexContext("reply-message"), newGmailClient(t, server.URL).ReplyToMessage(), gmailConnection, gmail.ReplyToMessageInput{MessageID: "message-2", TextBody: "处理结束"})
	require.NoError(t, err)
	require.Equal(t, gmail.ReplyToMessageBranchSent, result.Branch)
	require.Contains(t, raw, `To: "Sender" <sender@example.com>`)
	require.Contains(t, raw, "In-Reply-To: <message-2@example.com>")
	require.Contains(t, raw, "References: <root@example.com> <message-2@example.com>")
	require.Contains(t, raw, "处理结束")
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
	client, err := gmail.New(gmail.Config{Endpoint: server.URL, PollInterval: time.Second}, connector.StaticCredentialProvider[gmail.Credentials]{
		gmailConnection: {AccessToken: connector.NewSecretString("gmail-token"), PrimaryEmail: "owner@example.com"},
	})
	require.NoError(t, err)
	connection, err := gmail.NewConnection(client, gmailConnection)
	require.NoError(t, err)

	rootEvents := make(chan connector.TriggerEvent[gmail.MessageEvent], 2)
	rootRunner := gmail.NewMessageReceivedTrigger(gmail.MessageReceivedTriggerConfig{
		Connection: connection, BindingName: "gmail-thread-start",
		Configuration: gmail.MessageReceivedTriggerConfiguration{MessageMatcher: gmail.MessageMatcher{SenderEmails: []string{"sender@example.com"}}},
		Target: connector.TriggerTargetFunc[gmail.MessageEvent](func(_ context.Context, event connector.TriggerEvent[gmail.MessageEvent]) error {
			rootEvents <- event
			return nil
		}),
	})
	replyEvents := make(chan connector.TriggerEvent[gmail.MessageEvent], 2)
	replyRunner := gmail.NewReplyReceivedTrigger(gmail.ReplyReceivedTriggerConfig{
		Connection: connection, BindingName: "gmail-thread-reply",
		Configuration: gmail.ReplyReceivedTriggerConfiguration{ReplyMatcher: gmail.MessageMatcher{MessageContains: "approval"}},
		Target: connector.TriggerTargetFunc[gmail.MessageEvent](func(_ context.Context, event connector.TriggerEvent[gmail.MessageEvent]) error {
			replyEvents <- event
			return nil
		}),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var waitGroup sync.WaitGroup
	waitGroup.Add(2)
	go func() { defer waitGroup.Done(); _ = rootRunner.Run(ctx) }()
	go func() { defer waitGroup.Done(); _ = replyRunner.Run(ctx) }()

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
	require.Empty(t, rootEvents)
	require.Empty(t, replyEvents)
}

func receiveTriggerEvent(t *testing.T, events <-chan connector.TriggerEvent[gmail.MessageEvent]) connector.TriggerEvent[gmail.MessageEvent] {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(4 * time.Second):
		t.Fatal("Gmail Trigger event was not delivered")
		return connector.TriggerEvent[gmail.MessageEvent]{}
	}
}

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
		parts = append(parts, map[string]any{"mimeType": "text/plain", "body": map[string]string{"data": base64.RawURLEncoding.EncodeToString([]byte(textBody))}})
	}
	if htmlBody != "" {
		parts = append(parts, map[string]any{"mimeType": "text/html", "body": map[string]string{"data": base64.RawURLEncoding.EncodeToString([]byte(htmlBody))}})
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
