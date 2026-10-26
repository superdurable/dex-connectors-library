// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// sendFakeBehavior scripts how the fake Gmail API answers one test's send, search, and thread read.
type sendFakeBehavior struct {
	// answerSend writes the messages.send answer; nil aborts the connection after reading the request.
	answerSend        func(http.ResponseWriter)
	answerSendAttempt func(http.ResponseWriter, int)
	// searchMatch is the messages.list match JSON; empty answers with no messages.
	searchMatch          string
	matchFromSearch      int
	searchStatusCode     int
	sourceReadStatusCode int
	// threadReadID answers a format=minimal read with this thread; empty answers 500.
	threadReadID                                                       string
	isSendHanging, isSearchHanging, isSourceReadHanging, hangFirstSend bool
}

// gmailSendFake is a Gmail API fake whose search matches only the exact documented rfc822msgid query.
type gmailSendFake struct {
	*httptest.Server
	behavior                                  sendFakeBehavior
	release                                   chan struct{}
	mutex                                     sync.Mutex
	sends, searches, sourceReads, threadReads int
	rfcMessageID, rejectedSearchText          string
	rfcMessageIDs                             []string
}

func newGmailSendFake(t *testing.T, behavior sendFakeBehavior) *gmailSendFake {
	t.Helper()
	fake := &gmailSendFake{behavior: behavior, release: make(chan struct{})}
	fake.Server = httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(fake.Close)
	t.Cleanup(func() { close(fake.release) })
	return fake
}

func (fake *gmailSendFake) serveHTTP(response http.ResponseWriter, request *http.Request) {
	switch {
	case request.Method == http.MethodGet && request.URL.Path == "/users/me/messages/source-1":
		fake.mutex.Lock()
		fake.sourceReads++
		fake.mutex.Unlock()
		if fake.behavior.isSourceReadHanging {
			fake.hang(request)
			return
		}
		if fake.behavior.sourceReadStatusCode != 0 {
			response.WriteHeader(fake.behavior.sourceReadStatusCode)
			return
		}
		_, _ = response.Write([]byte(gmailMessageJSON("source-1", "thread-1", true, "", "")))
	case request.Method == http.MethodPost && request.URL.Path == "/users/me/messages/send":
		fake.serveSend(response, request)
	case request.Method == http.MethodGet && request.URL.Path == "/users/me/messages":
		fake.serveSearch(response, request)
	case request.Method == http.MethodGet && strings.HasPrefix(request.URL.Path, "/users/me/messages/"):
		fake.mutex.Lock()
		fake.threadReads++
		fake.mutex.Unlock()
		if fake.behavior.threadReadID == "" || request.URL.Query().Get("format") != "minimal" {
			response.WriteHeader(http.StatusInternalServerError)
			return
		}
		messageID := strings.TrimPrefix(request.URL.Path, "/users/me/messages/")
		_, _ = response.Write([]byte(`{"id":"` + messageID + `","threadId":"` + fake.behavior.threadReadID + `"}`))
	default:
		http.NotFound(response, request)
	}
}

func (fake *gmailSendFake) serveSend(response http.ResponseWriter, request *http.Request) {
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
	fake.mutex.Lock()
	fake.sends++
	sendNumber := fake.sends
	for _, line := range strings.Split(string(raw), "\r\n") {
		if value, isMessageID := strings.CutPrefix(line, "Message-ID: "); isMessageID {
			fake.rfcMessageID = strings.TrimSuffix(strings.TrimPrefix(value, "<"), ">")
			fake.rfcMessageIDs = append(fake.rfcMessageIDs, fake.rfcMessageID)
		}
	}
	fake.mutex.Unlock()
	switch {
	case fake.behavior.isSendHanging || (fake.behavior.hangFirstSend && sendNumber == 1):
		fake.hang(request)
	case fake.behavior.answerSendAttempt != nil:
		fake.behavior.answerSendAttempt(response, sendNumber)
	case fake.behavior.answerSend == nil:
		panic(http.ErrAbortHandler)
	default:
		fake.behavior.answerSend(response)
	}
}

func (fake *gmailSendFake) serveSearch(response http.ResponseWriter, request *http.Request) {
	fake.mutex.Lock()
	fake.searches++
	searchNumber := fake.searches
	expectedQuery := "in:sent rfc822msgid:" + fake.rfcMessageID
	query := request.URL.Query().Get("q")
	if (fake.rfcMessageID != "" && query != expectedQuery) || !strings.HasPrefix(query, "in:sent rfc822msgid:") || !strings.HasSuffix(query, "@dex.superdurable.dev") || request.URL.Query().Get("maxResults") != "1" || request.URL.Query().Get("includeSpamTrash") != "true" {
		fake.rejectedSearchText = request.URL.RawQuery
		fake.mutex.Unlock()
		response.WriteHeader(http.StatusBadRequest)
		return
	}
	fake.mutex.Unlock()
	if fake.behavior.searchStatusCode != 0 {
		response.WriteHeader(fake.behavior.searchStatusCode)
		return
	}
	if fake.behavior.isSearchHanging {
		fake.hang(request)
		return
	}
	if fake.behavior.searchMatch == "" || (fake.behavior.matchFromSearch > 0 && searchNumber < fake.behavior.matchFromSearch) {
		_, _ = response.Write([]byte(`{"resultSizeEstimate":0}`))
		return
	}
	_, _ = response.Write([]byte(`{"messages":[` + fake.behavior.searchMatch + `]}`))
}

func (fake *gmailSendFake) hang(request *http.Request) {
	select {
	case <-request.Context().Done():
	case <-fake.release:
	}
}

func (fake *gmailSendFake) counts() (sends int, searches int, threadReads int, rejectedSearch string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return fake.sends, fake.searches, fake.threadReads, fake.rejectedSearchText
}

func (fake *gmailSendFake) sourceReadCount() int {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return fake.sourceReads
}

func (fake *gmailSendFake) makeSentMailSearchMatchAvailable(match string) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.behavior.searchMatch = match
	fake.behavior.matchFromSearch = 0
}

func (fake *gmailSendFake) messageIDs() []string {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return append([]string(nil), fake.rfcMessageIDs...)
}

// sendAttemptResult is one send or reply attempt seen through the shared SendMessageOutput.
type sendAttemptResult struct {
	branch  sdkgo.BranchID
	value   gmail.SendMessageOutput
	failure *sdkgo.Failure
	err     error
}

func runGmailSendAttempt(ctx *gmailDexContext, client *gmail.Client, isReply bool) sendAttemptResult {
	if isReply {
		result, err := sdkgo.RunMutation(ctx, client.ReplyToMessage(), gmailConnection, gmail.ReplyToMessageInput{MessageID: "source-1", TextBody: "Update"})
		return sendAttemptResult{branch: result.Branch, value: result.Value, failure: result.Failure, err: err}
	}
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), gmailConnection, gmail.SendMessageInput{To: []string{"customer@example.com"}, Subject: "Progress", TextBody: "Update"})
	return sendAttemptResult{branch: result.Branch, value: result.Value, failure: result.Failure, err: err}
}

func newGmailClientWithHTTPClient(t *testing.T, endpoint string, httpClient *http.Client) *gmail.Client {
	t.Helper()
	client, err := gmail.New(gmail.Config{Endpoint: endpoint}, sdkgo.StaticCredentialProvider[gmail.Credentials]{
		gmailConnection: {AccessToken: sdkgo.NewSecretString("gmail-token"), PrimaryEmail: "owner@example.com"},
	}, gmail.WithHTTPClient(httpClient))
	require.NoError(t, err)
	return client
}

func closedLocalAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return address
}

func sendBranch(isReply bool, sendBranch sdkgo.BranchID, replyBranch sdkgo.BranchID) sdkgo.BranchID {
	if isReply {
		return replyBranch
	}
	return sendBranch
}

func forSendAndReply(t *testing.T, run func(t *testing.T, isReply bool)) {
	t.Helper()
	for _, isReply := range []bool{false, true} {
		name := "send"
		if isReply {
			name = "reply"
		}
		t.Run(name, func(t *testing.T) { run(t, isReply) })
	}
}

// TestGmailTracedConnectFailureRetries proves the positive evidence that lets a write retry: a traced refused connection.
func TestGmailTracedConnectFailureRetries(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{})
		serverAddress, refusedAddress := strings.TrimPrefix(fake.URL, "http://"), closedLocalAddress(t)
		var dials atomic.Int32
		transport := &http.Transport{DisableKeepAlives: true, DialContext: func(ctx context.Context, network string, _ string) (net.Conn, error) {
			address := refusedAddress
			if isReply && dials.Add(1) == 1 {
				address = serverAddress
			}
			return (&net.Dialer{}).DialContext(ctx, network, address)
		}}
		client := newGmailClientWithHTTPClient(t, fake.URL, &http.Client{Transport: transport})
		result := runGmailSendAttempt(newGmailDexContext("refused-"+t.Name()), client, isReply)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, result.err, &retry)
		require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
		sends, searches, _, _ := fake.counts()
		require.Zero(t, sends)
		require.Zero(t, searches)
	})
}

type untracedFailingWriteTransport struct{ base http.RoundTripper }

func (transport untracedFailingWriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost {
		return nil, errors.New("custom transport failed without httptrace events")
	}
	return transport.base.RoundTrip(request)
}

type unreadableAcceptedBody struct{}

func (unreadableAcceptedBody) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
func (unreadableAcceptedBody) Close() error             { return nil }

type unreadableAcceptedResponseTransport struct{ base http.RoundTripper }

func (transport unreadableAcceptedResponseTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err == nil && request.Method == http.MethodPost && response.StatusCode == http.StatusOK {
		// The fixture replaces the accepted body with a failing reader.
		_ = response.Body.Close()
		response.Body = unreadableAcceptedBody{}
	}
	return response, err
}

// TestGmailUntracedTransportFailureIsPossiblySent requires read-back before a later resend.
func TestGmailUntracedTransportFailureIsPossiblySent(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{})
		client := newGmailClientWithHTTPClient(t, fake.URL, &http.Client{Transport: untracedFailingWriteTransport{base: http.DefaultTransport}})
		result := runGmailSendAttempt(newGmailDexContext("untraced-"+t.Name()), client, isReply)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, result.err, &retry)
		require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
		_, searches, _, _ := fake.counts()
		require.Equal(t, 4, searches)
	})
}

func TestGmailAmbiguousWritesSearchSentMail(t *testing.T) {
	for _, test := range []struct {
		name       string
		answerSend func(http.ResponseWriter)
		isFound    bool
		isAccepted bool
	}{
		{name: "lost response found", isFound: true},
		{name: "lost response missing"},
		{name: "success without message ID found", answerSend: writeGmailSendJSON(`{"threadId":"thread-1"}`), isFound: true, isAccepted: true},
		{name: "success without message ID missing", answerSend: writeGmailSendJSON(`{"threadId":"thread-1"}`), isAccepted: true},
		{name: "server error found", answerSend: writeGmailSendStatus(http.StatusInternalServerError), isFound: true},
		{name: "server error missing", answerSend: writeGmailSendStatus(http.StatusInternalServerError)},
	} {
		t.Run(test.name, func(t *testing.T) {
			forSendAndReply(t, func(t *testing.T, isReply bool) {
				behavior := sendFakeBehavior{answerSend: test.answerSend}
				if test.isFound {
					behavior.searchMatch = `{"id":"sent-1","threadId":"thread-1"}`
				}
				fake := newGmailSendFake(t, behavior)
				result := runGmailSendAttempt(newGmailDexContext(t.Name()), newGmailClient(t, fake.URL), isReply)
				sends, searches, _, rejectedSearch := fake.counts()
				require.Empty(t, rejectedSearch, "the search must use the exact documented rfc822msgid query")
				require.Equal(t, 1, sends)
				if test.isFound {
					require.Equal(t, 1, searches)
				} else {
					require.Equal(t, 4, searches)
				}
				if !test.isFound {
					if test.isAccepted {
						require.NoError(t, result.err)
						require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), result.branch)
						require.Empty(t, result.value.MessageID)
						if isReply {
							require.Equal(t, "thread-1", result.value.ThreadID)
						} else {
							require.Empty(t, result.value.ThreadID)
						}
						return
					}
					var retry *sdkgo.RetryError
					require.ErrorAs(t, result.err, &retry)
					require.Contains(t, retry.Failure.Message, "the sent-mail search found no message")
					return
				}
				require.NoError(t, result.err)
				require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), result.branch)
				require.Equal(t, gmail.SendMessageOutput{
					Sender: "owner@example.com", Recipients: result.value.Recipients, MessageID: "sent-1", ThreadID: "thread-1",
				}, result.value)
				require.Nil(t, result.failure)
			})
		})
	}
}

// TestGmailReadBackMatchInAnotherThreadIsSent treats any match as proof: a reply is never resent after one.
func TestGmailReadBackMatchInAnotherThreadIsSent(t *testing.T) {
	for _, test := range []struct {
		name       string
		answerSend func(http.ResponseWriter)
	}{
		{name: "lost response"},
		{name: "server error", answerSend: writeGmailSendStatus(http.StatusInternalServerError)},
		{name: "service unavailable", answerSend: writeGmailSendStatus(http.StatusServiceUnavailable)},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newGmailSendFake(t, sendFakeBehavior{answerSend: test.answerSend, searchMatch: `{"id":"sent-1","threadId":"thread-other"}`})
			result := runGmailSendAttempt(newGmailDexContext(t.Name()), newGmailClient(t, fake.URL), true)
			require.NoError(t, result.err)
			require.Equal(t, gmail.ReplyToMessageBranchSent, result.branch)
			require.Equal(t, "sent-1", result.value.MessageID)
			require.Equal(t, "thread-other", result.value.ThreadID)
			sends, _, _, _ := fake.counts()
			require.Equal(t, 1, sends)
		})
	}
}

// TestGmailSuccessWithMessageIDIsSent never searches or resends after Gmail returned a message ID.
func TestGmailSuccessWithMessageIDIsSent(t *testing.T) {
	for _, test := range []struct {
		name                       string
		sendAnswer, threadReadID   string
		wantSendThread, wantThread string
		wantThreadReads            int
	}{
		{name: "thread returned", sendAnswer: `{"id":"message-1","threadId":"thread-1"}`, wantSendThread: "thread-1", wantThread: "thread-1"},
		{name: "reply placed in another thread", sendAnswer: `{"id":"message-1","threadId":"thread-new"}`, wantSendThread: "thread-new", wantThread: "thread-new"},
		{name: "missing thread read by ID", sendAnswer: `{"id":"message-1"}`, threadReadID: "thread-read", wantSendThread: "thread-read", wantThread: "thread-read", wantThreadReads: 1},
		{name: "missing thread and failed read", sendAnswer: `{"id":"message-1"}`, wantSendThread: "", wantThread: "thread-1", wantThreadReads: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			forSendAndReply(t, func(t *testing.T, isReply bool) {
				fake := newGmailSendFake(t, sendFakeBehavior{answerSend: writeGmailSendJSON(test.sendAnswer), threadReadID: test.threadReadID})
				result := runGmailSendAttempt(newGmailDexContext(t.Name()), newGmailClient(t, fake.URL), isReply)
				require.NoError(t, result.err)
				require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), result.branch)
				require.Equal(t, "message-1", result.value.MessageID)
				wantThread := test.wantSendThread
				if isReply {
					wantThread = test.wantThread
				}
				require.Equal(t, wantThread, result.value.ThreadID)
				sends, searches, threadReads, _ := fake.counts()
				require.Equal(t, 1, sends)
				require.Zero(t, searches)
				require.Equal(t, test.wantThreadReads, threadReads)
			})
		})
	}
}

// TestGmailSendAttemptReturnsBeforeTheExecuteTimeout bounds a hanging send by the Step deadline.
//
// The deadline here is 6 seconds, so the POST ends at 3.5 seconds and the attempt at 5.
func TestGmailSendAttemptReturnsBeforeTheExecuteTimeout(t *testing.T) {
	for _, test := range []struct {
		name                 string
		behavior             sendFakeBehavior
		isReply              bool
		wantSends, wantReads int
	}{
		{name: "send hangs and the search misses", behavior: sendFakeBehavior{isSendHanging: true}, wantSends: 1, wantReads: 1},
		{name: "send and search hang", behavior: sendFakeBehavior{isSendHanging: true, isSearchHanging: true}, wantSends: 1, wantReads: 1},
		{name: "reply hangs and the search misses", behavior: sendFakeBehavior{isSendHanging: true}, isReply: true, wantSends: 1, wantReads: 1},
		{name: "reply source read hangs", behavior: sendFakeBehavior{isSourceReadHanging: true}, isReply: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fake := newGmailSendFake(t, test.behavior)
			stepContext, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			stepDeadline, _ := stepContext.Deadline()
			result := runGmailSendAttempt(&gmailDexContext{Context: stepContext, step: t.Name()}, newGmailClient(t, fake.URL), test.isReply)
			require.True(t, time.Now().Before(stepDeadline.Add(-500*time.Millisecond)), "Invoke must return before the Execute deadline")
			var retry *sdkgo.RetryError
			require.ErrorAs(t, result.err, &retry)
			sends, searches, _, _ := fake.counts()
			require.Equal(t, test.wantSends, sends)
			if test.wantReads > 0 {
				require.Positive(t, searches)
			} else {
				require.Zero(t, searches)
			}
		})
	}
}

func TestGmailSendWithoutTimeLeftRetriesBeforeSending(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{answerSend: writeGmailSendJSON(`{"id":"message-1","threadId":"thread-1"}`)})
		stepContext, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		defer cancel()
		result := runGmailSendAttempt(&gmailDexContext{Context: stepContext, step: t.Name()}, newGmailClient(t, fake.URL), isReply)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, result.err, &retry)
		sends, _, _, _ := fake.counts()
		require.Zero(t, sends)
	})
}

func TestGmailSlowSendFindsMailOnNextAttemptWithoutAnotherPost(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{
			hangFirstSend: true,
		})
		client := newGmailClient(t, fake.URL)
		stepContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		first := runGmailSendAttempt(&gmailDexContext{Context: stepContext, step: t.Name()}, client, isReply)
		cancel()
		var retry *sdkgo.RetryError
		require.ErrorAs(t, first.err, &retry)
		fake.makeSentMailSearchMatchAvailable(`{"id":"sent-1","threadId":"thread-1"}`)
		second := runGmailSendAttempt(&gmailDexContext{Context: context.Background(), step: t.Name(), attempt: 2}, client, isReply)
		require.NoError(t, second.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), second.branch)
		require.Equal(t, "sent-1", second.value.MessageID)
		require.Equal(t, "thread-1", second.value.ThreadID)
		sends, searches, _, _ := fake.counts()
		require.Equal(t, 1, sends)
		require.GreaterOrEqual(t, searches, 2)
	})
}

func TestGmailNextAttemptResendsAfterSearchMiss(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{answerSendAttempt: func(response http.ResponseWriter, sendNumber int) {
			if sendNumber == 1 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = response.Write([]byte(`{"id":"sent-2","threadId":"thread-1"}`))
		}})
		client := newGmailClient(t, fake.URL)
		first := runGmailSendAttempt(newGmailDexContext(t.Name()), client, isReply)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, first.err, &retry)
		second := runGmailSendAttempt(&gmailDexContext{Context: context.Background(), step: t.Name(), attempt: 2}, client, isReply)
		require.NoError(t, second.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), second.branch)
		sends, searches, _, _ := fake.counts()
		require.Equal(t, 2, sends)
		require.Equal(t, 5, searches)
		messageIDs := fake.messageIDs()
		require.Len(t, messageIDs, 2)
		require.Equal(t, messageIDs[0], messageIDs[1])
	})
}

func TestGmailSearchFailureRetriesWithoutResending(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{
			answerSend: writeGmailSendStatus(http.StatusInternalServerError), searchStatusCode: http.StatusServiceUnavailable,
		})
		client := newGmailClient(t, fake.URL)
		first := runGmailSendAttempt(newGmailDexContext(t.Name()), client, isReply)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, first.err, &retry)
		second := runGmailSendAttempt(&gmailDexContext{Context: context.Background(), step: t.Name(), attempt: 2}, client, isReply)
		require.ErrorAs(t, second.err, &retry)
		sends, searches, _, _ := fake.counts()
		require.Equal(t, 1, sends)
		require.Equal(t, 5, searches)
	})
}

func TestGmailPreSendMatchSkipsPost(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{
			answerSend:  writeGmailSendStatus(http.StatusInternalServerError),
			searchMatch: `{"id":"sent-1","threadId":"thread-1"}`, matchFromSearch: 5,
		})
		client := newGmailClient(t, fake.URL)
		first := runGmailSendAttempt(newGmailDexContext(t.Name()), client, isReply)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, first.err, &retry)
		second := runGmailSendAttempt(&gmailDexContext{Context: context.Background(), step: t.Name(), attempt: 2}, client, isReply)
		require.NoError(t, second.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), second.branch)
		sends, searches, _, _ := fake.counts()
		require.Equal(t, 1, sends)
		require.Equal(t, 5, searches)
	})
}

func TestGmailReplyPreSendMatchPrecedesSourceRead(t *testing.T) {
	for _, sourceStatus := range []int{http.StatusNotFound, http.StatusForbidden} {
		t.Run(http.StatusText(sourceStatus), func(t *testing.T) {
			fake := newGmailSendFake(t, sendFakeBehavior{
				searchMatch: `{"id":"sent-reply","threadId":"sent-thread"}`, sourceReadStatusCode: sourceStatus,
			})
			result := runGmailSendAttempt(&gmailDexContext{Context: context.Background(), step: t.Name(), attempt: 2}, newGmailClient(t, fake.URL), true)
			require.NoError(t, result.err)
			require.Equal(t, gmail.ReplyToMessageBranchSent, result.branch)
			require.Equal(t, "sent-reply", result.value.MessageID)
			require.Equal(t, "sent-thread", result.value.ThreadID)
			require.Zero(t, fake.sourceReadCount())
			sends, searches, _, _ := fake.counts()
			require.Zero(t, sends)
			require.Equal(t, 1, searches)
		})
	}
}

func TestGmailPreSendSearchTimeoutSkipsPost(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{isSearchHanging: true})
		stepContext, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		result := runGmailSendAttempt(&gmailDexContext{Context: stepContext, step: t.Name(), attempt: 2}, newGmailClient(t, fake.URL), isReply)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, result.err, &retry)
		sends, searches, _, _ := fake.counts()
		require.Zero(t, sends)
		require.Equal(t, 1, searches)
		if isReply {
			require.Zero(t, fake.sourceReadCount())
		}
	})
}

func TestGmailReadBackPollFindsNewlyIndexedMessage(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		for _, accepted := range []bool{false, true} {
			name := "ambiguous"
			answer := writeGmailSendStatus(http.StatusInternalServerError)
			if accepted {
				name = "accepted_without_id"
				answer = writeGmailSendJSON(`{"threadId":"thread-1"}`)
			}
			t.Run(name, func(t *testing.T) {
				fake := newGmailSendFake(t, sendFakeBehavior{answerSend: answer,
					searchMatch: `{"id":"sent-late","threadId":"thread-1"}`, matchFromSearch: 3})
				result := runGmailSendAttempt(newGmailDexContext(t.Name()), newGmailClient(t, fake.URL), isReply)
				require.NoError(t, result.err)
				require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), result.branch)
				require.Equal(t, "sent-late", result.value.MessageID)
				sends, searches, _, _ := fake.counts()
				require.Equal(t, 1, sends)
				require.Equal(t, 3, searches)
			})
		}
	})
}

func TestGmailUnusableAcceptedBodiesNeverResend(t *testing.T) {
	for _, test := range []struct {
		name       string
		body       string
		unreadable bool
	}{
		{name: "read_failure", body: `{"id":"sent-1"}`, unreadable: true},
		{name: "oversized", body: strings.Repeat("x", (1<<20)+1)},
		{name: "invalid_json", body: `{"id":`},
		{name: "missing_id", body: `{"threadId":"thread-1"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			forSendAndReply(t, func(t *testing.T, isReply bool) {
				fake := newGmailSendFake(t, sendFakeBehavior{answerSend: writeGmailSendJSON(test.body)})
				client := newGmailClient(t, fake.URL)
				if test.unreadable {
					client = newGmailClientWithHTTPClient(t, fake.URL, &http.Client{Transport: unreadableAcceptedResponseTransport{base: http.DefaultTransport}})
				}
				result := runGmailSendAttempt(newGmailDexContext(t.Name()), client, isReply)
				require.NoError(t, result.err)
				require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), result.branch)
				require.Empty(t, result.value.MessageID)
				if isReply {
					require.Equal(t, "thread-1", result.value.ThreadID)
				} else {
					require.Empty(t, result.value.ThreadID)
				}
				sends, searches, _, _ := fake.counts()
				require.Equal(t, 1, sends)
				require.Equal(t, 4, searches)
			})
		})
	}
}

func TestGmailSecondAttemptRechecksSentMailAfterProviderRejection(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{
			answerSendAttempt: func(response http.ResponseWriter, sendNumber int) {
				if sendNumber == 1 {
					response.WriteHeader(http.StatusInternalServerError)
				} else {
					response.WriteHeader(http.StatusForbidden)
				}
			},
			searchMatch: `{"id":"sent-first","threadId":"thread-1"}`, matchFromSearch: 6,
		})
		client := newGmailClient(t, fake.URL)
		first := runGmailSendAttempt(newGmailDexContext(t.Name()), client, isReply)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, first.err, &retry)
		second := runGmailSendAttempt(&gmailDexContext{Context: context.Background(), step: t.Name(), attempt: 2}, client, isReply)
		require.NoError(t, second.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), second.branch)
		require.Equal(t, "sent-first", second.value.MessageID)
		sends, searches, _, _ := fake.counts()
		require.Equal(t, 2, sends)
		require.Equal(t, 6, searches)
	})
}

func TestGmailMessageIDSeparatesFlowRuns(t *testing.T) {
	fake := newGmailSendFake(t, sendFakeBehavior{answerSend: writeGmailSendJSON(`{"id":"sent","threadId":"thread"}`)})
	client := newGmailClient(t, fake.URL)
	for _, run := range []struct {
		id      string
		started time.Time
	}{
		{id: "first-run", started: time.Unix(10, 0)},
		{id: "second-run", started: time.Unix(10, 0)},
		{id: "first-run", started: time.Unix(11, 0)},
	} {
		result := runGmailSendAttempt(&gmailDexContext{Context: context.Background(), step: t.Name(), runID: run.id, flowStartedAt: run.started}, client, false)
		require.NoError(t, result.err)
	}
	messageIDs := fake.messageIDs()
	require.Len(t, messageIDs, 3)
	require.NotEqual(t, messageIDs[0], messageIDs[1])
	require.NotEqual(t, messageIDs[0], messageIDs[2])
}

func TestGmailRateLimitAndServiceUnavailableRetry(t *testing.T) {
	for _, test := range []struct {
		name       string
		statusCode int
		wantKind   sdkgo.FailureKind
	}{
		{name: "429", statusCode: http.StatusTooManyRequests, wantKind: sdkgo.FailureRateLimit},
		{name: "503", statusCode: http.StatusServiceUnavailable, wantKind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			forSendAndReply(t, func(t *testing.T, isReply bool) {
				fake := newGmailSendFake(t, sendFakeBehavior{answerSend: writeGmailSendStatus(test.statusCode)})
				result := runGmailSendAttempt(newGmailDexContext(t.Name()), newGmailClient(t, fake.URL), isReply)
				var retry *sdkgo.RetryError
				require.ErrorAs(t, result.err, &retry)
				require.Equal(t, test.wantKind, retry.Failure.Kind)
			})
		})
	}
}

func TestGmailServiceUnavailableReadBackReleasesTheWriteConnection(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{
			answerSend: func(response http.ResponseWriter) {
				response.WriteHeader(http.StatusServiceUnavailable)
				_, _ = response.Write([]byte(`{"error":"temporarily unavailable"}`))
			},
			searchMatch: `{"id":"sent-1","threadId":"thread-1"}`,
		})
		transport := &http.Transport{MaxConnsPerHost: 1}
		t.Cleanup(transport.CloseIdleConnections)
		client := newGmailClientWithHTTPClient(t, fake.URL, &http.Client{Transport: transport, Timeout: 2 * time.Second})
		result := runGmailSendAttempt(newGmailDexContext(t.Name()), client, isReply)
		require.NoError(t, result.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), result.branch)
		require.Equal(t, "sent-1", result.value.MessageID)
	})
}

func writeGmailSendJSON(body string) func(http.ResponseWriter) {
	return func(response http.ResponseWriter) { _, _ = response.Write([]byte(body)) }
}

func writeGmailSendStatus(statusCode int) func(http.ResponseWriter) {
	return func(response http.ResponseWriter) { response.WriteHeader(statusCode) }
}
