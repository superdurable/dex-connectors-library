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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
	"github.com/superdurable/dex/sdk-go/dex"
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

func runGmailSendAttempt(ctx dex.Context, client *gmail.Client, isReply bool, options ...sdkgo.RunOption) sendAttemptResult {
	if isReply {
		result, err := sdkgo.RunMutation(ctx, client.ReplyToMessage(), gmailConnection, gmail.ReplyToMessageInput{MessageID: "source-1", TextBody: "Update"}, options...)
		return sendAttemptResult{branch: result.Branch, value: result.Value, failure: result.Failure, err: err}
	}
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), gmailConnection, gmail.SendMessageInput{To: []string{"customer@example.com"}, Subject: "Progress", TextBody: "Update"}, options...)
	return sendAttemptResult{branch: result.Branch, value: result.Value, failure: result.Failure, err: err}
}

func callIDWithoutProviderRequest(t *testing.T, ctx dex.Context, client *gmail.Client, isReply bool) sdkgo.CallID {
	t.Helper()
	if isReply {
		result, err := sdkgo.RunMutation(ctx, client.ReplyToMessage(), gmailConnection, gmail.ReplyToMessageInput{})
		require.NoError(t, err)
		require.Equal(t, gmail.ReplyToMessageBranchDefect, result.Branch)
		return result.Receipt.CallID
	}
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), gmailConnection, gmail.SendMessageInput{})
	require.NoError(t, err)
	require.Equal(t, gmail.SendMessageBranchDefect, result.Branch)
	return result.Receipt.CallID
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
				stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
				result := runGmailSendAttempt(stepContext, newGmailClient(t, fake.URL), isReply)
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
						require.NotNil(t, result.value.Recipients)
						stepContext.AdvanceAttempt()
						require.Equal(t, singledispatch.StateAccepted, readPersistedMarker(t, stepContext).State)
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
				require.NotNil(t, result.value.Recipients)
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

func TestGmailServiceUnavailableSearchMissResendsWithTheSameMessageID(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{answerSendAttempt: func(response http.ResponseWriter, sendNumber int) {
			if sendNumber == 1 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = response.Write([]byte(`{"id":"sent-2","threadId":"thread-1"}`))
		}})
		client := newGmailClient(t, fake.URL)
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		first := runGmailSendAttempt(stepContext, client, isReply)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, first.err, &retry)
		require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
		stepContext.AdvanceAttempt()
		marker := readPersistedMarker(t, stepContext)
		require.Equal(t, singledispatch.StateClaimed, marker.State)
		require.Equal(t, int32(1), marker.SendCount)
		second := runGmailSendAttempt(stepContext, client, isReply)
		require.NoError(t, second.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), second.branch)
		require.Equal(t, "sent-2", second.value.MessageID)
		sends, searches, _, _ := fake.counts()
		require.Equal(t, 2, sends)
		require.Equal(t, 5, searches)
		messageIDs := fake.messageIDs()
		require.Len(t, messageIDs, 2)
		require.Equal(t, messageIDs[0], messageIDs[1])
	})
}

// requireGmailUncertain asserts that an attempt selected the uncertain branch.
func requireGmailUncertain(t *testing.T, result sendAttemptResult) {
	t.Helper()
	require.NoError(t, result.err)
	require.Equal(t, sdkgo.UncertainBranchID, result.branch)
	require.NotNil(t, result.failure)
}

// TestGmailUnconfirmedSendResendsBelowTheCapThenSelectsUncertain resends with one Message-ID below
// the cap, selects uncertain when the final send stays unconfirmed, and afterwards only reads back.
func TestGmailUnconfirmedSendResendsBelowTheCapThenSelectsUncertain(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{answerSend: writeGmailSendStatus(http.StatusInternalServerError)})
		client := newGmailClient(t, fake.URL)
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		maxSends := int(gmail.SendMessageDefinition.MaxSends)
		if isReply {
			maxSends = int(gmail.ReplyToMessageDefinition.MaxSends)
		}
		require.Equal(t, 3, maxSends)
		var retry *sdkgo.RetryError
		for attempt := 1; attempt <= maxSends; attempt++ {
			result := runGmailSendAttempt(stepContext, client, isReply)
			if attempt < maxSends {
				require.ErrorAs(t, result.err, &retry)
			} else {
				requireGmailUncertain(t, result)
				require.Contains(t, result.failure.Message, "the sent-mail search found no message")
			}
			sends, searches, _, _ := fake.counts()
			require.Equal(t, attempt, sends)
			// Attempt one polls four times; later attempts search once before the POST, then poll.
			require.Equal(t, 5*attempt-1, searches)
			stepContext.AdvanceAttempt()
			marker := readPersistedMarker(t, stepContext)
			require.Equal(t, singledispatch.StateClaimed, marker.State)
			require.Equal(t, int32(attempt), marker.SendCount)
		}
		atCap := runGmailSendAttempt(stepContext, client, isReply)
		requireGmailUncertain(t, atCap)
		require.Contains(t, atCap.failure.Message, "no further")
		sends, searches, _, _ := fake.counts()
		require.Equal(t, maxSends, sends)
		require.Equal(t, 5*maxSends, searches)
		if isReply {
			require.Equal(t, maxSends, fake.sourceReadCount())
		}
		messageIDs := fake.messageIDs()
		require.Len(t, messageIDs, maxSends)
		for _, messageID := range messageIDs {
			require.Equal(t, messageIDs[0], messageID)
		}
	})
}

// TestGmailFinalSendConfirmedByReadBackIsSent selects sent when Sent Mail confirms the send the cap allows last.
func TestGmailFinalSendConfirmedByReadBackIsSent(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		// Attempts one and two search nine times; attempt three misses before its POST, then matches.
		fake := newGmailSendFake(t, sendFakeBehavior{
			answerSend:  writeGmailSendStatus(http.StatusInternalServerError),
			searchMatch: `{"id":"sent-3","threadId":"thread-1"}`, matchFromSearch: 11,
		})
		client := newGmailClient(t, fake.URL)
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		for attempt := 1; attempt < 3; attempt++ {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, runGmailSendAttempt(stepContext, client, isReply).err, &retry)
			stepContext.AdvanceAttempt()
		}
		final := runGmailSendAttempt(stepContext, client, isReply)
		require.NoError(t, final.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), final.branch)
		require.Equal(t, "sent-3", final.value.MessageID)
		sends, searches, _, _ := fake.counts()
		require.Equal(t, 3, sends)
		require.Equal(t, 11, searches)
	})
}

// TestGmailSendCapFollowsTheStepMaxSends selects uncertain at the application's send cap, not a fixed one.
func TestGmailSendCapFollowsTheStepMaxSends(t *testing.T) {
	for _, maxSends := range []int32{1, 5} {
		t.Run(strconv.Itoa(int(maxSends)), func(t *testing.T) {
			forSendAndReply(t, func(t *testing.T, isReply bool) {
				fake := newGmailSendFake(t, sendFakeBehavior{answerSend: writeGmailSendStatus(http.StatusInternalServerError)})
				client := newGmailClient(t, fake.URL)
				stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
				for attempt := int32(1); attempt < maxSends; attempt++ {
					var retry *sdkgo.RetryError
					require.ErrorAs(t, runGmailSendAttempt(stepContext, client, isReply, sdkgo.WithMaxSends(maxSends)).err, &retry)
					stepContext.AdvanceAttempt()
				}
				requireGmailUncertain(t, runGmailSendAttempt(stepContext, client, isReply, sdkgo.WithMaxSends(maxSends)))
				sends, _, _, _ := fake.counts()
				require.Equal(t, int(maxSends), sends)
			})
		})
	}
}

// TestGmailRejectedResendKeepsTheEarlierSendUnknown selects uncertain instead of reporting that
// nothing changed: a definite refusal proves only that the resend failed.
func TestGmailRejectedResendKeepsTheEarlierSendUnknown(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{answerSendAttempt: func(response http.ResponseWriter, sendNumber int) {
			if sendNumber == 1 {
				response.WriteHeader(http.StatusInternalServerError)
				return
			}
			response.WriteHeader(http.StatusBadRequest)
			_, _ = response.Write([]byte(`{"error":{"code":400}}`))
		}})
		client := newGmailClient(t, fake.URL)
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		first := runGmailSendAttempt(stepContext, client, isReply)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, first.err, &retry)
		stepContext.AdvanceAttempt()
		second := runGmailSendAttempt(stepContext, client, isReply)
		requireGmailUncertain(t, second)
		require.Equal(t, sdkgo.FailureProviderRejection, second.failure.Kind)
		require.Contains(t, second.failure.Message, "an earlier send may still be delivered")
		sends, _, _, _ := fake.counts()
		require.Equal(t, 2, sends)
		stepContext.AdvanceAttempt()
		marker := readPersistedMarker(t, stepContext)
		require.Equal(t, singledispatch.StateClaimed, marker.State)
		require.Equal(t, int32(2), marker.SendCount)
	})
}

// TestGmailIndefiniteResendRefusalRetriesBelowTheCap treats a 409 resend as an unknown outcome.
func TestGmailIndefiniteResendRefusalRetriesBelowTheCap(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{answerSendAttempt: func(response http.ResponseWriter, sendNumber int) {
			if sendNumber == 1 {
				response.WriteHeader(http.StatusInternalServerError)
				return
			}
			response.WriteHeader(http.StatusConflict)
		}})
		client := newGmailClient(t, fake.URL)
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		var retry *sdkgo.RetryError
		for attempt := 1; attempt < 3; attempt++ {
			require.ErrorAs(t, runGmailSendAttempt(stepContext, client, isReply).err, &retry)
			stepContext.AdvanceAttempt()
		}
		// The final send the cap allows is refused indefinitely too, so the earlier sends stay unconfirmed.
		third := runGmailSendAttempt(stepContext, client, isReply)
		requireGmailUncertain(t, third)
		require.Equal(t, sdkgo.FailureConflict, third.failure.Kind)
		sends, _, _, _ := fake.counts()
		require.Equal(t, 3, sends)
	})
}

// TestGmailRejectedFirstSendIsProviderRejected keeps a definite 4xx terminal when nothing was sent before.
func TestGmailRejectedFirstSendIsProviderRejected(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{answerSend: writeGmailSendStatus(http.StatusBadRequest)})
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		result := runGmailSendAttempt(stepContext, newGmailClient(t, fake.URL), isReply)
		require.NoError(t, result.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchProviderRejected, gmail.ReplyToMessageBranchProviderRejected), result.branch)
		// canResend proves the refused POST sent nothing, so agent hosts report failure.
		stepContext.AdvanceAttempt()
		marker := readPersistedMarker(t, stepContext)
		require.Equal(t, singledispatch.StateCanResend, marker.State)
		require.Equal(t, int32(1), marker.Attempt)
	})
}

// TestGmailUnprovenRefusalOnFirstSendRetriesUnknown repeats a 4xx outside the allowlist, reading back first.
func TestGmailUnprovenRefusalOnFirstSendRetriesUnknown(t *testing.T) {
	for _, statusCode := range []int{http.StatusRequestTimeout, http.StatusConflict, http.StatusTeapot} {
		t.Run(strconv.Itoa(statusCode), func(t *testing.T) {
			forSendAndReply(t, func(t *testing.T, isReply bool) {
				fake := newGmailSendFake(t, sendFakeBehavior{answerSendAttempt: func(response http.ResponseWriter, sendNumber int) {
					if sendNumber == 1 {
						response.WriteHeader(statusCode)
						return
					}
					_, _ = response.Write([]byte(`{"id":"sent-2","threadId":"thread-1"}`))
				}})
				client := newGmailClient(t, fake.URL)
				stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
				first := runGmailSendAttempt(stepContext, client, isReply)
				var retry *sdkgo.RetryError
				require.ErrorAs(t, first.err, &retry)
				require.Contains(t, retry.Failure.Message, "Gmail may still have processed the request")
				stepContext.AdvanceAttempt()
				marker := readPersistedMarker(t, stepContext)
				require.Equal(t, singledispatch.StateClaimed, marker.State)
				second := runGmailSendAttempt(stepContext, client, isReply)
				require.NoError(t, second.err)
				require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), second.branch)
				sends, searches, _, _ := fake.counts()
				require.Equal(t, 2, sends)
				require.Equal(t, 1, searches)
			})
		})
	}
}

// TestGmailRejectedSendAfterRateLimitRecordsCanResend treats a refused POST after a proven unsent one as clean.
func TestGmailRejectedSendAfterRateLimitRecordsCanResend(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{answerSendAttempt: func(response http.ResponseWriter, sendNumber int) {
			if sendNumber == 1 {
				response.WriteHeader(http.StatusTooManyRequests)
				return
			}
			response.WriteHeader(http.StatusBadRequest)
		}})
		client := newGmailClient(t, fake.URL)
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		first := runGmailSendAttempt(stepContext, client, isReply)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, first.err, &retry)
		stepContext.AdvanceAttempt()
		second := runGmailSendAttempt(stepContext, client, isReply)
		require.NoError(t, second.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchProviderRejected, gmail.ReplyToMessageBranchProviderRejected), second.branch)
		stepContext.AdvanceAttempt()
		marker := readPersistedMarker(t, stepContext)
		require.Equal(t, singledispatch.StateCanResend, marker.State)
		require.Equal(t, int32(2), marker.Attempt)
	})
}

// TestGmailReplySourceRejectedAfterUnconfirmedSendIsUncertain keeps an earlier reply unknown when the source vanished.
func TestGmailReplySourceRejectedAfterUnconfirmedSendIsUncertain(t *testing.T) {
	fake := newGmailSendFake(t, sendFakeBehavior{answerSend: writeGmailSendStatus(http.StatusInternalServerError)})
	client := newGmailClient(t, fake.URL)
	stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
	first := runGmailSendAttempt(stepContext, client, true)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, first.err, &retry)
	fake.mutex.Lock()
	fake.behavior.sourceReadStatusCode = http.StatusNotFound
	fake.mutex.Unlock()
	stepContext.AdvanceAttempt()
	second := runGmailSendAttempt(stepContext, client, true)
	requireGmailUncertain(t, second)
	require.Equal(t, sdkgo.FailureNotFound, second.failure.Kind)
	sends, _, _, _ := fake.counts()
	require.Equal(t, 1, sends)
}

// TestGmailReplyIndefiniteSourceReadCountsOnlyEarlierSends retries an unclear source read
// before any POST even when the earlier sends leave room for only one more send.
func TestGmailReplyIndefiniteSourceReadCountsOnlyEarlierSends(t *testing.T) {
	fake := newGmailSendFake(t, sendFakeBehavior{answerSend: writeGmailSendStatus(http.StatusInternalServerError)})
	client := newGmailClient(t, fake.URL)
	stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
	for attempt := 1; attempt <= 2; attempt++ {
		var retry *sdkgo.RetryError
		require.ErrorAs(t, runGmailSendAttempt(stepContext, client, true).err, &retry)
		stepContext.AdvanceAttempt()
	}
	fake.mutex.Lock()
	fake.behavior.sourceReadStatusCode = http.StatusServiceUnavailable
	fake.mutex.Unlock()
	third := runGmailSendAttempt(stepContext, client, true)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, third.err, &retry, "two earlier sends stay below the cap of three")
	stepContext.AdvanceAttempt()
	marker := readPersistedMarker(t, stepContext)
	require.Equal(t, singledispatch.StateNotSent, marker.State)
	require.Equal(t, int32(2), marker.SendCount)
	sends, _, _, _ := fake.counts()
	require.Equal(t, 2, sends)

	fake.mutex.Lock()
	fake.behavior.sourceReadStatusCode = 0
	fake.behavior.answerSend = writeGmailSendJSON(`{"id":"sent-4","threadId":"thread-1"}`)
	fake.mutex.Unlock()
	_, searchesBefore, _, _ := fake.counts()
	sourceReadsBefore := fake.sourceReadCount()
	fourth := runGmailSendAttempt(stepContext, client, true)
	require.NoError(t, fourth.err)
	require.Equal(t, gmail.ReplyToMessageBranchSent, fourth.branch)
	sends, searches, _, _ := fake.counts()
	require.Equal(t, 3, sends)
	require.Greater(t, searches, searchesBefore, "attempt four searches Sent Mail")
	require.Greater(t, fake.sourceReadCount(), sourceReadsBefore, "attempt four rereads the source")
}

// TestGmailReplySourceRejectedOnFirstAttemptIsProviderRejected keeps a clean first rejection terminal.
func TestGmailReplySourceRejectedOnFirstAttemptIsProviderRejected(t *testing.T) {
	fake := newGmailSendFake(t, sendFakeBehavior{sourceReadStatusCode: http.StatusNotFound})
	result := runGmailSendAttempt(singledispatchtest.NewContext("gmail-fence-test", t.Name()), newGmailClient(t, fake.URL), true)
	require.NoError(t, result.err)
	require.Equal(t, gmail.ReplyToMessageBranchProviderRejected, result.branch)
}

func TestGmailRateLimitCheckpointPermitsOneLaterPost(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{answerSendAttempt: func(response http.ResponseWriter, sendNumber int) {
			if sendNumber == 1 {
				response.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_, _ = response.Write([]byte(`{"id":"sent-2","threadId":"thread-1"}`))
		}})
		client := newGmailClient(t, fake.URL)
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		first := runGmailSendAttempt(stepContext, client, isReply)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, first.err, &retry)
		stepContext.AdvanceAttempt()
		marker := readPersistedMarker(t, stepContext)
		require.Equal(t, singledispatch.StateCanResend, marker.State)
		second := runGmailSendAttempt(stepContext, client, isReply)
		require.NoError(t, second.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), second.branch)
		sends, searches, _, _ := fake.counts()
		require.Equal(t, 2, sends)
		require.Equal(t, 1, searches)
	})
}

func TestGmailSecondAttemptWithoutCheckpointSearchesThenResends(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{answerSend: writeGmailSendJSON(`{"id":"sent-2","threadId":"thread-1"}`)})
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		stepContext.AdvanceAttempt()
		result := runGmailSendAttempt(stepContext, newGmailClient(t, fake.URL), isReply)
		require.NoError(t, result.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), result.branch)
		sends, searches, threadReads, _ := fake.counts()
		require.Equal(t, 1, sends)
		require.Equal(t, 1, searches)
		require.Zero(t, threadReads)
		if isReply {
			require.Equal(t, 1, fake.sourceReadCount())
		}
		stepContext.AdvanceAttempt()
		marker := readPersistedMarker(t, stepContext)
		// The missing marker counts the first attempt as a possible send.
		require.Equal(t, int32(2), marker.SendCount)
	})
}

// TestGmailInvalidCheckpointFailsClosedWithoutProviderRequests retries, because an earlier send may have happened.
func TestGmailInvalidCheckpointFailsClosedWithoutProviderRequests(t *testing.T) {
	for _, scenario := range []string{"impossible send count", "heartbeat read error"} {
		t.Run(scenario, func(t *testing.T) {
			forSendAndReply(t, func(t *testing.T, isReply bool) {
				fake := newGmailSendFake(t, sendFakeBehavior{})
				client := newGmailClient(t, fake.URL)
				stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
				callID := callIDWithoutProviderRequest(t, stepContext, client, isReply)
				require.NotEmpty(t, callID)
				stepContext.AdvanceAttempt()
				switch scenario {
				case "impossible send count":
					// One attempt cannot have claimed three sends.
					seedPersistedMarker(t, stepContext, singledispatch.Marker{CallID: callID, Attempt: 1, State: singledispatch.StateClaimed, SendCount: 3})
				case "heartbeat read error":
					stepContext.SetReadError(errors.New("heartbeat unavailable"))
				}
				result := runGmailSendAttempt(stepContext, client, isReply)
				var retry *sdkgo.RetryError
				require.ErrorAs(t, result.err, &retry)
				require.Contains(t, retry.Failure.Message, "an earlier send may still be delivered")
				sends, searches, threadReads, _ := fake.counts()
				require.Zero(t, sends)
				require.Zero(t, searches)
				require.Zero(t, threadReads)
				require.Zero(t, fake.sourceReadCount())
			})
		})
	}
}

// TestGmailInvalidInputAfterPossibleSendIsUncertain never selects defect once an earlier send may have happened.
func TestGmailInvalidInputAfterPossibleSendIsUncertain(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{})
		client := newGmailClient(t, fake.URL)
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		callID := callIDWithoutProviderRequest(t, stepContext, client, isReply)
		stepContext.AdvanceAttempt()
		seedPersistedMarker(t, stepContext, singledispatch.Marker{CallID: callID, Attempt: 1, State: singledispatch.StateClaimed, SendCount: 1})
		var result sendAttemptResult
		if isReply {
			outcome, err := sdkgo.RunMutation(stepContext, client.ReplyToMessage(), gmailConnection, gmail.ReplyToMessageInput{})
			result = sendAttemptResult{branch: outcome.Branch, failure: outcome.Failure, err: err}
		} else {
			outcome, err := sdkgo.RunMutation(stepContext, client.SendMessage(), gmailConnection, gmail.SendMessageInput{})
			result = sendAttemptResult{branch: outcome.Branch, failure: outcome.Failure, err: err}
		}
		requireGmailUncertain(t, result)
		require.Contains(t, result.failure.Message, "an earlier send may still be delivered")
		sends, _, _, _ := fake.counts()
		require.Zero(t, sends)
	})
}

// TestGmailRefusalNamingMessageIsReadBackByID confirms a message a refusal body names by its ID.
func TestGmailRefusalNamingMessageIsReadBackByID(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{threadReadID: "thread-1", answerSend: func(response http.ResponseWriter) {
			response.WriteHeader(http.StatusBadRequest)
			_, _ = response.Write([]byte(`{"id":"sent-1","threadId":"thread-1"}`))
		}})
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		result := runGmailSendAttempt(stepContext, newGmailClient(t, fake.URL), isReply)
		require.NoError(t, result.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), result.branch)
		require.Equal(t, "sent-1", result.value.MessageID)
		require.Equal(t, "thread-1", result.value.ThreadID)
		sends, searches, threadReads, _ := fake.counts()
		require.Equal(t, 1, sends)
		require.Zero(t, searches)
		require.Equal(t, 1, threadReads)
		stepContext.AdvanceAttempt()
		require.Equal(t, "sent-1", readPersistedMarker(t, stepContext).ProviderID)
	})
}

// TestGmailUnconfirmedRefusalNamingMessageIsNeverResent reads back a named message instead of
// resending it; the saved ID forbids a resend, so an unconfirmed later read-back is uncertain.
func TestGmailUnconfirmedRefusalNamingMessageIsNeverResent(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{answerSend: func(response http.ResponseWriter) {
			response.WriteHeader(http.StatusBadRequest)
			_, _ = response.Write([]byte(`{"id":"sent-1"}`))
		}})
		client := newGmailClient(t, fake.URL)
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, runGmailSendAttempt(stepContext, client, isReply).err, &retry)
		stepContext.AdvanceAttempt()
		require.Equal(t, "sent-1", readPersistedMarker(t, stepContext).ProviderID)
		second := runGmailSendAttempt(stepContext, client, isReply)
		requireGmailUncertain(t, second)
		sends, _, threadReads, _ := fake.counts()
		require.Equal(t, 1, sends, "a message the refusal named is read back, never resent")
		require.Equal(t, 2, threadReads)
	})
}

func TestGmailSavedProviderIDConfirmsWithoutSearch(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{
			answerSend: writeGmailSendJSON(`{"id":"sent-1","threadId":"thread-1"}`), threadReadID: "thread-1",
		})
		client := newGmailClient(t, fake.URL)
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		first := runGmailSendAttempt(stepContext, client, isReply)
		require.NoError(t, first.err)
		stepContext.AdvanceAttempt()
		marker := readPersistedMarker(t, stepContext)
		require.Equal(t, "sent-1", marker.ProviderID)
		second := runGmailSendAttempt(stepContext, client, isReply)
		require.NoError(t, second.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), second.branch)
		require.Equal(t, "sent-1", second.value.MessageID)
		sends, searches, threadReads, _ := fake.counts()
		require.Equal(t, 1, sends)
		require.Zero(t, searches)
		require.Equal(t, 1, threadReads)
		if isReply {
			require.Equal(t, 1, fake.sourceReadCount())
		}
	})
}

func TestGmailReadBackMatchCheckpointsProviderID(t *testing.T) {
	for _, statusCode := range []int{http.StatusServiceUnavailable, http.StatusInternalServerError} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			forSendAndReply(t, func(t *testing.T, isReply bool) {
				fake := newGmailSendFake(t, sendFakeBehavior{
					answerSend:  writeGmailSendStatus(statusCode),
					searchMatch: `{"id":"sent-1","threadId":"thread-1"}`,
				})
				stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
				result := runGmailSendAttempt(stepContext, newGmailClient(t, fake.URL), isReply)
				require.NoError(t, result.err)
				require.Equal(t, "sent-1", result.value.MessageID)
				stepContext.AdvanceAttempt()
				marker := readPersistedMarker(t, stepContext)
				require.Equal(t, singledispatch.StateClaimed, marker.State)
				require.Equal(t, "sent-1", marker.ProviderID)
			})
		})
	}
}

// TestGmailSavedProviderIDIsNeverResent reads back an accepted send's ID and never resends it; an
// unconfirmed read-back is uncertain because no resend is permitted.
func TestGmailSavedProviderIDIsNeverResent(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{answerSend: writeGmailSendJSON(`{"id":"sent-1","threadId":"thread-1"}`)})
		client := newGmailClient(t, fake.URL)
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		first := runGmailSendAttempt(stepContext, client, isReply)
		require.NoError(t, first.err)
		stepContext.AdvanceAttempt()
		// threadReadID is empty, so reading the saved ID fails and Sent Mail finds nothing.
		second := runGmailSendAttempt(stepContext, client, isReply)
		requireGmailUncertain(t, second)
		sends, searches, _, _ := fake.counts()
		require.Equal(t, 1, sends)
		require.Equal(t, 1, searches)
	})
}

func TestGmailInconclusiveSearchResendsWithTheSameMessageID(t *testing.T) {
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
		require.Equal(t, 2, sends)
		require.Equal(t, 9, searches)
		messageIDs := fake.messageIDs()
		require.Len(t, messageIDs, 2)
		require.Equal(t, messageIDs[0], messageIDs[1])
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

func TestGmailClaimedSecondAttemptFindsPreviouslySentMail(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{
			answerSendAttempt: func(response http.ResponseWriter, sendNumber int) {
				if sendNumber == 1 {
					response.WriteHeader(http.StatusInternalServerError)
				} else {
					response.WriteHeader(http.StatusForbidden)
				}
			},
			searchMatch: `{"id":"sent-first","threadId":"thread-1"}`, matchFromSearch: 5,
		})
		client := newGmailClient(t, fake.URL)
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		first := runGmailSendAttempt(stepContext, client, isReply)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, first.err, &retry)
		stepContext.AdvanceAttempt()
		second := runGmailSendAttempt(stepContext, client, isReply)
		require.NoError(t, second.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), second.branch)
		require.Equal(t, "sent-first", second.value.MessageID)
		sends, searches, _, _ := fake.counts()
		require.Equal(t, 1, sends)
		require.Equal(t, 5, searches)
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

// sequencedGmailCredentials returns one resolution per call, then repeats the last.
type sequencedGmailCredentials struct {
	mutex       sync.Mutex
	resolutions []error
	resolved    int
}

func (provider *sequencedGmailCredentials) Resolve(sdkgo.Call) (gmail.Credentials, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	err := provider.resolutions[min(provider.resolved, len(provider.resolutions)-1)]
	provider.resolved++
	if err != nil {
		return gmail.Credentials{}, err
	}
	return gmail.Credentials{AccessToken: sdkgo.NewSecretString("gmail-token"), PrimaryEmail: "owner@example.com"}, nil
}

func (provider *sequencedGmailCredentials) ResolveWithRefresh(
	_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[gmail.Credentials],
) (gmail.Credentials, error) {
	return provider.Resolve(call)
}

// TestGmailCredentialFailureAfterPossibleSend keeps a possibly delivered send unknown: a transient
// failure retries, and one that needs reconnection selects uncertain because nothing can read back.
func TestGmailCredentialFailureAfterPossibleSend(t *testing.T) {
	for _, testCase := range []struct {
		name          string
		credentialErr error
		isUncertain   bool
	}{
		{name: "transient", credentialErr: errors.New("credential store is unavailable")},
		{name: "reauthorization", credentialErr: sdkgo.NewReauthorizationRequiredError(errors.New("grant revoked")), isUncertain: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			forSendAndReply(t, func(t *testing.T, isReply bool) {
				fake := newGmailSendFake(t, sendFakeBehavior{answerSend: writeGmailSendStatus(http.StatusInternalServerError)})
				provider := &sequencedGmailCredentials{resolutions: []error{nil, testCase.credentialErr}}
				client, err := gmail.New(gmail.Config{Endpoint: fake.URL}, provider)
				require.NoError(t, err)
				stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
				first := runGmailSendAttempt(stepContext, client, isReply)
				var retry *sdkgo.RetryError
				require.ErrorAs(t, first.err, &retry)
				stepContext.AdvanceAttempt()
				second := runGmailSendAttempt(stepContext, client, isReply)
				if testCase.isUncertain {
					requireGmailUncertain(t, second)
					require.Equal(t, sdkgo.FailureAuthentication, second.failure.Kind)
					require.Contains(t, second.failure.Message, "need reconnection")
				} else {
					require.ErrorAs(t, second.err, &retry)
					require.Equal(t, sdkgo.FailureAuthentication, retry.Failure.Kind)
					require.Contains(t, retry.Failure.Message, "an earlier send may still be delivered")
				}
				sends, _, _, _ := fake.counts()
				require.Equal(t, 1, sends)
				// The claim stays; notSent would let the next attempt skip read-back.
				stepContext.AdvanceAttempt()
				marker := readPersistedMarker(t, stepContext)
				require.Equal(t, singledispatch.StateClaimed, marker.State)
				require.Equal(t, int32(1), marker.Attempt)
			})
		})
	}
}

// TestGmailMessageIDCheckIgnoresOtherFieldTypes accepts any body whose id is a non-empty string.
func TestGmailMessageIDCheckIgnoresOtherFieldTypes(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		statusCode  int
		body        string
		wantsSent   bool
		wantsRetry  bool
		wantsReject bool
	}{
		{name: "accepted_numeric_thread", statusCode: http.StatusOK, body: `{"id":"sent-1","threadId":7}`, wantsSent: true},
		{name: "refusal_naming_message", statusCode: http.StatusBadRequest, body: `{"id":"sent-1","threadId":7}`, wantsSent: true},
		{name: "refusal_numeric_id", statusCode: http.StatusBadRequest, body: `{"id":7}`, wantsReject: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			forSendAndReply(t, func(t *testing.T, isReply bool) {
				fake := newGmailSendFake(t, sendFakeBehavior{threadReadID: "thread-1", answerSend: func(response http.ResponseWriter) {
					response.WriteHeader(testCase.statusCode)
					_, _ = response.Write([]byte(testCase.body))
				}})
				result := runGmailSendAttempt(singledispatchtest.NewContext("gmail-fence-test", t.Name()), newGmailClient(t, fake.URL), isReply)
				sends, searches, _, _ := fake.counts()
				require.Equal(t, 1, sends)
				switch {
				case testCase.wantsSent:
					require.NoError(t, result.err)
					require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), result.branch)
					require.Equal(t, "sent-1", result.value.MessageID)
					require.Zero(t, searches, "a named message needs no Sent Mail search")
				case testCase.wantsRetry:
					var retry *sdkgo.RetryError
					require.ErrorAs(t, result.err, &retry)
				case testCase.wantsReject:
					require.NoError(t, result.err)
					require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchProviderRejected, gmail.ReplyToMessageBranchProviderRejected), result.branch)
				}
			})
		})
	}
}

// TestGmailCanResendAfterUnknownSendSearchesBeforeResending reads back a counted send that a later 429 did not erase.
func TestGmailCanResendAfterUnknownSendSearchesBeforeResending(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{answerSendAttempt: func(response http.ResponseWriter, sendNumber int) {
			if sendNumber == 1 {
				response.WriteHeader(http.StatusInternalServerError)
				return
			}
			response.WriteHeader(http.StatusTooManyRequests)
		}})
		client := newGmailClient(t, fake.URL)
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, runGmailSendAttempt(stepContext, client, isReply).err, &retry)
		stepContext.AdvanceAttempt()
		require.ErrorAs(t, runGmailSendAttempt(stepContext, client, isReply).err, &retry)
		stepContext.AdvanceAttempt()
		require.Equal(t, singledispatch.StateCanResend, readPersistedMarker(t, stepContext).State)
		fake.makeSentMailSearchMatchAvailable(`{"id":"sent-1","threadId":"thread-1"}`)
		third := runGmailSendAttempt(stepContext, client, isReply)
		require.NoError(t, third.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), third.branch)
		sends, _, _, _ := fake.counts()
		require.Equal(t, 2, sends, "the first send landed, so the third attempt reads it back instead of resending")
	})
}

// TestGmailPreSendRetryAfterCountedCanResendRecordsNothing keeps the counted send visible to later attempts.
func TestGmailPreSendRetryAfterCountedCanResendRecordsNothing(t *testing.T) {
	for _, testCase := range []struct {
		failure string
		isReply bool
	}{{failure: "search"}, {failure: "search", isReply: true}, {failure: "source_read", isReply: true}} {
		failure, isReply := testCase.failure, testCase.isReply
		t.Run(failure+"-"+strconv.FormatBool(isReply), func(t *testing.T) {
			fake := newGmailSendFake(t, sendFakeBehavior{answerSendAttempt: func(response http.ResponseWriter, sendNumber int) {
				if sendNumber == 1 {
					response.WriteHeader(http.StatusInternalServerError)
					return
				}
				response.WriteHeader(http.StatusTooManyRequests)
			}})
			client := newGmailClient(t, fake.URL)
			stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, runGmailSendAttempt(stepContext, client, isReply).err, &retry)
			stepContext.AdvanceAttempt()
			require.ErrorAs(t, runGmailSendAttempt(stepContext, client, isReply).err, &retry)
			stepContext.AdvanceAttempt()
			fake.mutex.Lock()
			if failure == "search" {
				fake.behavior.searchStatusCode = http.StatusInternalServerError
			} else {
				fake.behavior.sourceReadStatusCode = http.StatusInternalServerError
			}
			fake.mutex.Unlock()
			require.ErrorAs(t, runGmailSendAttempt(stepContext, client, isReply).err, &retry)
			sends, _, _, _ := fake.counts()
			require.Equal(t, 2, sends)
			// The notSent marker retains the counted first send.
			stepContext.AdvanceAttempt()
			marker := readPersistedMarker(t, stepContext)
			require.Equal(t, singledispatch.StateNotSent, marker.State)
			require.Equal(t, int32(3), marker.Attempt)
			require.Equal(t, int32(1), marker.SendCount)
		})
	}
}

// TestGmailTerminalCredentialFailureOnFirstAttemptIsDefect selects defect when nothing was sent.
func TestGmailTerminalCredentialFailureOnFirstAttemptIsDefect(t *testing.T) {
	for _, terminal := range []error{
		sdkgo.NewReauthorizationRequiredError(errors.New("authorization revoked")), projectconfig.ErrReauthorizationRequired,
		projectconfig.ErrObjectNotFound, projectconfig.ErrConnectionNotSelected, projectconfig.ErrCredentialBrokerRejected,
	} {
		t.Run(terminal.Error(), func(t *testing.T) {
			forSendAndReply(t, func(t *testing.T, isReply bool) {
				fake := newGmailSendFake(t, sendFakeBehavior{})
				client, err := gmail.New(gmail.Config{Endpoint: fake.URL}, &sequencedGmailCredentials{resolutions: []error{terminal}})
				require.NoError(t, err)
				result := runGmailSendAttempt(singledispatchtest.NewContext("gmail-fence-test", t.Name()), client, isReply)
				require.NoError(t, result.err)
				require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchDefect, gmail.ReplyToMessageBranchDefect), result.branch)
				sends, searches, _, _ := fake.counts()
				require.Zero(t, sends)
				require.Zero(t, searches)
			})
		})
	}
}

// TestGmailTransientCredentialFailureOnFirstAttemptRetries records notSent instead of selecting defect.
func TestGmailTransientCredentialFailureOnFirstAttemptRetries(t *testing.T) {
	for _, transient := range []error{
		errors.New("credential store is unavailable"), errors.New("connection is not configured"),
		projectconfig.ErrCredentialBrokerUnavailable, projectconfig.ErrRejectedCredentialNotRefreshed,
	} {
		t.Run(transient.Error(), func(t *testing.T) {
			forSendAndReply(t, func(t *testing.T, isReply bool) {
				fake := newGmailSendFake(t, sendFakeBehavior{})
				client, err := gmail.New(gmail.Config{Endpoint: fake.URL}, &sequencedGmailCredentials{resolutions: []error{transient}})
				require.NoError(t, err)
				stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
				result := runGmailSendAttempt(stepContext, client, isReply)
				var retry *sdkgo.RetryError
				require.ErrorAs(t, result.err, &retry)
				sends, searches, _, _ := fake.counts()
				require.Zero(t, sends)
				require.Zero(t, searches)
				stepContext.AdvanceAttempt()
				require.Equal(t, singledispatch.StateNotSent, readPersistedMarker(t, stepContext).State)
			})
		})
	}
}
