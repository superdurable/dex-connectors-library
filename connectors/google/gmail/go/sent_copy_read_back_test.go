// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail_test

import (
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	gmail "github.com/superdurable/dex-connectors-library/connectors/google/gmail/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

// crashAfterAcceptedSendTransport ends the attempt's goroutine once Gmail answered the send,
// like a Worker that dies after Gmail accepted but before the connector saw the answer.
type crashAfterAcceptedSendTransport struct{ base http.RoundTripper }

func (transport crashAfterAcceptedSendTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil || request.Method != http.MethodPost || !strings.HasSuffix(request.URL.Path, "/users/me/messages/send") {
		return response, err
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	runtime.Goexit()
	return nil, nil
}

func expectedCandidateQuery(isReply bool) string {
	if isReply {
		return "in:sent newer_than:2d to:sender@example.com"
	}
	return "in:sent newer_than:2d to:customer@example.com"
}

// TestGmailCrashAfterAcceptedSendIsNeverResent reproduces the live duplicate: Gmail rewrote the
// Message-ID of the accepted send, so only its preserved X-Dex-Message-ID can confirm it.
func TestGmailCrashAfterAcceptedSendIsNeverResent(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{
			answerSend:  writeGmailSendJSON(`{"id":"sent-1","threadId":"thread-1"}`),
			searchMatch: `{"id":"sent-1","threadId":"thread-1"}`,
		})
		crashingClient := newGmailClientWithHTTPClient(t, fake.URL, &http.Client{Transport: crashAfterAcceptedSendTransport{base: http.DefaultTransport}})
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		attemptEnded := make(chan struct{})
		go func() {
			defer close(attemptEnded)
			runGmailSendAttempt(stepContext, crashingClient, isReply)
		}()
		<-attemptEnded
		stepContext.AdvanceAttempt()
		require.Equal(t, singledispatch.StateClaimed, readPersistedMarker(t, stepContext).State)

		second := runGmailSendAttempt(stepContext, newGmailClient(t, fake.URL), isReply)
		require.NoError(t, second.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), second.branch)
		require.Equal(t, "sent-1", second.value.MessageID)
		require.Equal(t, "thread-1", second.value.ThreadID)
		sends, searches, _, _ := fake.counts()
		require.Equal(t, 1, sends, "the accepted send must not be posted again")
		require.Equal(t, 1, searches)
		require.Equal(t, 1, fake.headerReadCount())
		if isReply {
			require.Equal(t, []string{"in:sent newer_than:2d"}, fake.queries(), "a reply reads back before its source, so no recipient is known")
		} else {
			require.Equal(t, []string{expectedCandidateQuery(false)}, fake.queries())
		}
	})
}

// TestGmailPostedMessageCarriesTheDexMessageID pins the header Gmail keeps while it replaces Message-ID.
func TestGmailPostedMessageCarriesTheDexMessageID(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{answerSend: writeGmailSendJSON(`{"id":"sent-1","threadId":"thread-1"}`)})
		result := runGmailSendAttempt(newGmailDexContext(t.Name()), newGmailClient(t, fake.URL), isReply)
		require.NoError(t, result.err)
		messageIDs := fake.messageIDs()
		require.Len(t, messageIDs, 1)
		require.True(t, strings.HasSuffix(messageIDs[0], "@dex.superdurable.dev"))
		fake.mutex.Lock()
		defer fake.mutex.Unlock()
		require.Equal(t, "<"+messageIDs[0]+">", fake.dexMessageID)
	})
}

// TestGmailReadBackSkipsOtherSentCopies matches only this run's X-Dex-Message-ID: another call's
// copy, or one an earlier run of a reused Flow ID sent with the same X-Dex-Call-ID, is no proof.
func TestGmailReadBackSkipsOtherSentCopies(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{
			searchMatch:       `{"id":"sent-0","threadId":"thread-0"},{"id":"sent-1","threadId":"thread-1"}`,
			decoyCandidateIDs: map[string]bool{"sent-0": true},
		})
		result := runGmailSendAttempt(newGmailDexContext(t.Name()), newGmailClient(t, fake.URL), isReply)
		require.NoError(t, result.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), result.branch)
		require.Equal(t, "sent-1", result.value.MessageID)
		require.Equal(t, "thread-1", result.value.ThreadID)
		require.Equal(t, 2, fake.headerReadCount())
		require.Equal(t, []string{expectedCandidateQuery(isReply)}, fake.queries())
	})
}

// TestGmailOnlyOtherSentCopiesResendsBelowTheCap treats a candidate list without this run's copy
// as a miss: the poll reads each candidate once, and the next attempt resends.
func TestGmailOnlyOtherSentCopiesResendsBelowTheCap(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{
			answerSend:        writeGmailSendStatus(http.StatusInternalServerError),
			searchMatch:       `{"id":"sent-0","threadId":"thread-0"}`,
			decoyCandidateIDs: map[string]bool{"sent-0": true},
		})
		client := newGmailClient(t, fake.URL)
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		first := runGmailSendAttempt(stepContext, client, isReply)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, first.err, &retry)
		require.Contains(t, retry.Failure.Message, "the sent-mail search found no message")
		_, searches, _, _ := fake.counts()
		require.Equal(t, 4, searches)
		require.Equal(t, 1, fake.headerReadCount(), "a mismatched candidate is read once per poll")
		stepContext.AdvanceAttempt()
		second := runGmailSendAttempt(stepContext, client, isReply)
		require.ErrorAs(t, second.err, &retry)
		sends, _, _, _ := fake.counts()
		require.Equal(t, 2, sends)
	})
}

// TestGmailReadBackMatchesAKeptMessageID confirms a copy whose gateway kept the client Message-ID.
func TestGmailReadBackMatchesAKeptMessageID(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{
			searchMatch: `{"id":"sent-1","threadId":"thread-1"}`, keepsClientMessageID: true,
		})
		result := runGmailSendAttempt(newGmailDexContext(t.Name()), newGmailClient(t, fake.URL), isReply)
		require.NoError(t, result.err)
		require.Equal(t, "sent-1", result.value.MessageID)
	})
}

// TestGmailUnreadableCandidateLeavesTheReadBackInconclusive selects uncertain at the cap, because
// an unread candidate may be the accepted send.
func TestGmailUnreadableCandidateLeavesTheReadBackInconclusive(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		fake := newGmailSendFake(t, sendFakeBehavior{
			searchMatch: `{"id":"sent-1","threadId":"thread-1"}`, headerReadStatusCode: http.StatusInternalServerError,
		})
		result := runGmailSendAttempt(newGmailDexContext(t.Name()), newGmailClient(t, fake.URL), isReply, sdkgo.WithMaxSends(1))
		requireGmailUncertain(t, result)
		require.Contains(t, result.failure.Message, "the sent-mail search did not answer")
		sends, _, _, _ := fake.counts()
		require.Equal(t, 1, sends)
		require.Equal(t, 4, fake.headerReadCount(), "an unreadable candidate is read again on every lookup")
	})
}

// TestGmailReplyRetryReadsBackByRecipientBeforeResending finds a reply copy that only the recipient's list shows.
func TestGmailReplyRetryReadsBackByRecipientBeforeResending(t *testing.T) {
	decoys, decoyIDs := sentMailDecoys(25)
	fake := newGmailSendFake(t, sendFakeBehavior{
		answerSend: writeGmailSendJSON(`{"id":"sent-1","threadId":"thread-1"}`),
		searchMatchByQuery: map[string]string{
			"in:sent newer_than:2d":                       decoys,
			"in:sent newer_than:2d to:sender@example.com": `{"id":"sent-1","threadId":"thread-1"}`,
		},
		decoyCandidateIDs: decoyIDs,
	})
	crashingClient := newGmailClientWithHTTPClient(t, fake.URL, &http.Client{Transport: crashAfterAcceptedSendTransport{base: http.DefaultTransport}})
	stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
	attemptEnded := make(chan struct{})
	go func() {
		defer close(attemptEnded)
		runGmailSendAttempt(stepContext, crashingClient, true)
	}()
	<-attemptEnded
	stepContext.AdvanceAttempt()

	second := runGmailSendAttempt(stepContext, newGmailClient(t, fake.URL), true)
	require.NoError(t, second.err)
	require.Equal(t, gmail.ReplyToMessageBranchSent, second.branch)
	require.Equal(t, "sent-1", second.value.MessageID)
	sends, _, _, _ := fake.counts()
	require.Equal(t, 1, sends, "the accepted reply must not be posted again")
	require.Equal(t, []string{"in:sent newer_than:2d", expectedCandidateQuery(true)}, fake.queries())
	require.Equal(t, 26, fake.headerReadCount())
}

// TestGmailReadBackKeepsTheRefreshedCredential refreshes an expired token once per attempt.
func TestGmailReadBackKeepsTheRefreshedCredential(t *testing.T) {
	forSendAndReply(t, func(t *testing.T, isReply bool) {
		decoys, decoyIDs := sentMailDecoys(3)
		fake := newGmailSendFake(t, sendFakeBehavior{
			answerSendAttempt: func(response http.ResponseWriter, sendNumber int) {
				if sendNumber == 1 {
					response.WriteHeader(http.StatusInternalServerError)
					return
				}
				_, _ = response.Write([]byte(`{"id":"sent-2","threadId":"thread-1"}`))
			},
			searchMatch: decoys, decoyCandidateIDs: decoyIDs, rejectedAccessToken: "rejected-token",
		})
		provider := &rejectionRefreshingCredentialProvider{}
		client, err := gmail.New(gmail.Config{Endpoint: fake.URL}, provider)
		require.NoError(t, err)
		stepContext := singledispatchtest.NewContext("gmail-fence-test", t.Name())
		first := runGmailSendAttempt(stepContext, client, isReply)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, first.err, &retry)
		rejectedBefore, refreshesBefore := fake.rejectedTokenRequestCount(), provider.forcedRefreshes
		stepContext.AdvanceAttempt()

		second := runGmailSendAttempt(stepContext, client, isReply)
		require.NoError(t, second.err)
		require.Equal(t, sendBranch(isReply, gmail.SendMessageBranchSent, gmail.ReplyToMessageBranchSent), second.branch)
		require.Equal(t, 1, fake.rejectedTokenRequestCount()-rejectedBefore)
		require.Equal(t, 1, provider.forcedRefreshes-refreshesBefore)
	})
}

// sentMailDecoys lists other calls' Sent copies, newest first.
func sentMailDecoys(count int) (string, map[string]bool) {
	candidates := make([]string, 0, count)
	decoyIDs := make(map[string]bool, count)
	for index := range count {
		decoyID := "sent-other-" + strconv.Itoa(index)
		candidates = append(candidates, `{"id":"`+decoyID+`","threadId":"thread-other"}`)
		decoyIDs[decoyID] = true
	}
	return strings.Join(candidates, ","), decoyIDs
}
