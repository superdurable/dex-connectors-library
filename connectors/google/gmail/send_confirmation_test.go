// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type failingWriteTransport struct{ base http.RoundTripper }

func (transport failingWriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost {
		return nil, errors.New("request never left the process")
	}
	return transport.base.RoundTrip(request)
}

func TestGmailProvenUnsentWritesRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/users/me/messages/source-1" {
			_, _ = response.Write([]byte(gmailMessageJSON("source-1", "thread-1", true, "", "")))
		}
	}))
	t.Cleanup(server.Close)
	client, err := gmail.New(gmail.Config{Endpoint: server.URL}, sdkgo.StaticCredentialProvider[gmail.Credentials]{
		gmailConnection: {AccessToken: sdkgo.NewSecretString("gmail-token"), PrimaryEmail: "owner@example.com"},
	}, gmail.WithHTTPClient(&http.Client{Transport: failingWriteTransport{base: http.DefaultTransport}}))
	require.NoError(t, err)
	for _, test := range []struct {
		name string
		run  func(string) error
	}{
		{name: "send", run: func(step string) error {
			_, err := sdkgo.RunMutation(newGmailDexContext(step), client.SendMessage(), gmailConnection, gmail.SendMessageInput{
				To: []string{"customer@example.com"}, Subject: "Progress", TextBody: "Update",
			})
			return err
		}},
		{name: "reply", run: func(step string) error {
			_, err := sdkgo.RunMutation(newGmailDexContext(step), client.ReplyToMessage(), gmailConnection, gmail.ReplyToMessageInput{
				MessageID: "source-1", TextBody: "Update",
			})
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, test.run("unsent-"+test.name), &retry)
			require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
		})
	}
}

func TestGmailAmbiguousWritesSearchSentMail(t *testing.T) {
	for _, test := range []struct {
		name       string
		isReply    bool
		loseAnswer bool
		isFound    bool
		wantBranch sdkgo.BranchID
	}{
		{name: "send lost response found", isFound: true, loseAnswer: true, wantBranch: gmail.SendMessageBranchSent},
		{name: "send lost response missing", loseAnswer: true, wantBranch: gmail.SendMessageBranchUncertain},
		{name: "send invalid success found", isFound: true, wantBranch: gmail.SendMessageBranchSent},
		{name: "send invalid success missing", wantBranch: gmail.SendMessageBranchUncertain},
		{name: "reply lost response found", isReply: true, isFound: true, loseAnswer: true, wantBranch: gmail.ReplyToMessageBranchSent},
		{name: "reply lost response missing", isReply: true, loseAnswer: true, wantBranch: gmail.ReplyToMessageBranchUncertain},
		{name: "reply invalid success found", isReply: true, isFound: true, wantBranch: gmail.ReplyToMessageBranchSent},
		{name: "reply invalid success missing", isReply: true, wantBranch: gmail.ReplyToMessageBranchUncertain},
	} {
		t.Run(test.name, func(t *testing.T) {
			var mutex sync.Mutex
			sends, searches, rfcMessageID := 0, 0, ""
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				switch {
				case request.Method == http.MethodGet && request.URL.Path == "/users/me/messages/source-1":
					_, _ = response.Write([]byte(gmailMessageJSON("source-1", "thread-1", true, "", "")))
				case request.Method == http.MethodPost && request.URL.Path == "/users/me/messages/send":
					var payload map[string]string
					if json.NewDecoder(request.Body).Decode(&payload) != nil {
						response.WriteHeader(http.StatusBadRequest)
						return
					}
					raw, err := base64.RawURLEncoding.DecodeString(payload["raw"])
					if err != nil {
						response.WriteHeader(http.StatusBadRequest)
						return
					}
					mutex.Lock()
					sends++
					for _, line := range strings.Split(string(raw), "\r\n") {
						if strings.HasPrefix(line, "Message-ID: ") {
							rfcMessageID = strings.TrimPrefix(line, "Message-ID: ")
						}
					}
					mutex.Unlock()
					if test.loseAnswer {
						panic(http.ErrAbortHandler)
					}
					_, _ = response.Write([]byte(`{"threadId":"thread-1"}`))
				case request.Method == http.MethodGet && request.URL.Path == "/users/me/messages":
					mutex.Lock()
					searches++
					messageID := rfcMessageID
					mutex.Unlock()
					if !strings.Contains(request.URL.Query().Get("q"), "in:sent rfc822msgid:"+messageID) || request.URL.Query().Get("maxResults") != "10" {
						response.WriteHeader(http.StatusBadRequest)
						return
					}
					if test.isFound {
						_, _ = response.Write([]byte(`{"messages":[{"id":"sent-1","threadId":"thread-1"}]}`))
					} else {
						_, _ = response.Write([]byte(`{"messages":[]}`))
					}
				default:
					http.NotFound(response, request)
				}
			}))
			t.Cleanup(server.Close)
			client := newGmailClient(t, server.URL)
			var branch sdkgo.BranchID
			var failure *sdkgo.Failure
			var messageID string
			if test.isReply {
				result, err := sdkgo.RunMutation(newGmailDexContext(test.name), client.ReplyToMessage(), gmailConnection, gmail.ReplyToMessageInput{MessageID: "source-1", TextBody: "Update"})
				require.NoError(t, err)
				branch, failure, messageID = result.Branch, result.Failure, result.Value.MessageID
			} else {
				result, err := sdkgo.RunMutation(newGmailDexContext(test.name), client.SendMessage(), gmailConnection, gmail.SendMessageInput{To: []string{"customer@example.com"}, Subject: "Progress", TextBody: "Update"})
				require.NoError(t, err)
				branch, failure, messageID = result.Branch, result.Failure, result.Value.MessageID
			}
			require.Equal(t, test.wantBranch, branch)
			if test.isFound {
				require.Equal(t, "sent-1", messageID)
				require.Nil(t, failure)
			} else {
				require.NotNil(t, failure)
			}
			mutex.Lock()
			defer mutex.Unlock()
			require.Equal(t, 1, sends)
			require.Equal(t, 1, searches)
			require.NotEmpty(t, rfcMessageID)
		})
	}
}

func TestGmailRateLimitAndServiceUnavailableRetry(t *testing.T) {
	for _, test := range []struct {
		name       string
		isReply    bool
		statusCode int
		wantKind   sdkgo.FailureKind
	}{
		{name: "send 429", statusCode: http.StatusTooManyRequests, wantKind: sdkgo.FailureRateLimit},
		{name: "send 503", statusCode: http.StatusServiceUnavailable, wantKind: sdkgo.FailureAvailability},
		{name: "reply 429", isReply: true, statusCode: http.StatusTooManyRequests, wantKind: sdkgo.FailureRateLimit},
		{name: "reply 503", isReply: true, statusCode: http.StatusServiceUnavailable, wantKind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				switch {
				case request.Method == http.MethodGet && request.URL.Path == "/users/me/messages/source-1":
					_, _ = response.Write([]byte(gmailMessageJSON("source-1", "thread-1", true, "", "")))
				case request.Method == http.MethodGet && request.URL.Path == "/users/me/messages":
					_, _ = response.Write([]byte(`{"messages":[]}`))
				case request.Method == http.MethodPost && request.URL.Path == "/users/me/messages/send":
					response.WriteHeader(test.statusCode)
				default:
					http.NotFound(response, request)
				}
			}))
			t.Cleanup(server.Close)
			client := newGmailClient(t, server.URL)
			var err error
			if test.isReply {
				_, err = sdkgo.RunMutation(newGmailDexContext(test.name), client.ReplyToMessage(), gmailConnection, gmail.ReplyToMessageInput{MessageID: "source-1", TextBody: "Update"})
			} else {
				_, err = sdkgo.RunMutation(newGmailDexContext(test.name), client.SendMessage(), gmailConnection, gmail.SendMessageInput{To: []string{"customer@example.com"}, Subject: "Progress", TextBody: "Update"})
			}
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.wantKind, retry.Failure.Kind)
		})
	}
}
