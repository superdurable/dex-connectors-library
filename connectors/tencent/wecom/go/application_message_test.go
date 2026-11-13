// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wecom

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

var testApplicationText = SendApplicationMessageInput{
	ToUserIDs: []string{"zhangsan", "lisi"}, ToDepartmentIDs: []string{"2"}, ToTagIDs: []string{"7", "8"},
	MessageType: ApplicationMessageText, Content: "Incident INC-7 is open", IsConfidential: true,
}

func runApplicationSend(t *testing.T, client *Client, input SendApplicationMessageInput) (sdkgo.MutationResult[SentApplicationMessage], error) {
	t.Helper()
	return sdkgo.RunMutation(singledispatchtest.NewContext("application-flow", "send-step"), client.SendApplicationMessage(), testApplicationConnection, input)
}

func TestSendApplicationMessageSendsWithDuplicateCheckAndCachedToken(t *testing.T) {
	fake := newFakeWeCom(t)
	fake.sendReplies = []fakeReply{{body: `{"errcode":0,"errmsg":"ok","invaliduser":"wangwu|zhaoliu","invalidparty":"9","invalidtag":"","unlicenseduser":"lisi","msgid":"fake-msgid-1"}`}}
	client := newTestClient(t, fake.server.URL)
	sent, err := runApplicationSend(t, client, testApplicationText)
	require.NoError(t, err)
	require.Equal(t, SendApplicationMessageBranchSent, sent.Branch)
	require.Equal(t, SentApplicationMessage{
		MessageID: "fake-msgid-1", InvalidUserIDs: []string{"wangwu", "zhaoliu"}, InvalidDepartmentIDs: []string{"9"},
		InvalidTagIDs: []string{}, UnlicensedUserIDs: []string{"lisi"},
	}, sent.Value)
	require.Equal(t, 1, fake.tokenCallCount())
	require.Equal(t, testCorpID, fake.tokenQueries[0].Get("corpid"))
	require.Equal(t, testCorpSecret, fake.tokenQueries[0].Get("corpsecret"))
	require.Equal(t, testAccessTokenPrefix+"1", fake.sendTokens[0])
	// The four-hour interval is the documented maximum of duplicate_check_interval.
	require.JSONEq(t, `{"touser":"zhangsan|lisi","toparty":"2","totag":"7|8","msgtype":"text","agentid":1000002,"text":{"content":"Incident INC-7 is open"},"safe":1,"enable_duplicate_check":1,"duplicate_check_interval":14400}`, fake.sendBodies[0])

	markdown := SendApplicationMessageInput{ToAllMembers: true, MessageType: ApplicationMessageMarkdown, Content: "**INC-7** resolved"}
	_, err = runApplicationSend(t, client, markdown)
	require.NoError(t, err)
	require.Equal(t, 1, fake.tokenCallCount(), "a cached token serves later sends")
	require.JSONEq(t, `{"touser":"@all","msgtype":"markdown","agentid":1000002,"markdown":{"content":"**INC-7** resolved"},"enable_duplicate_check":1,"duplicate_check_interval":14400}`, fake.sendBodies[1])
}

// Every attempt repeats the same request bytes, so WeCom's duplicate check suppresses a resend of an accepted message.
func TestSendApplicationMessageRetriesEveryUnconfirmedAttemptWithIdenticalBytes(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		reply fakeReply
		kind  sdkgo.FailureKind
	}{
		{name: "system busy", reply: fakeReply{body: `{"errcode":-1,"errmsg":"system busy"}`}, kind: sdkgo.FailureAvailability},
		{name: "frequency limit", reply: fakeReply{body: `{"errcode":45009,"errmsg":"api freq out of limit"}`}, kind: sdkgo.FailureRateLimit},
		{name: "concurrency limit", reply: fakeReply{body: `{"errcode":45033,"errmsg":"api concurrent out of limit"}`}, kind: sdkgo.FailureRateLimit},
		{name: "HTTP 429", reply: fakeReply{status: http.StatusTooManyRequests}, kind: sdkgo.FailureRateLimit},
		{name: "HTTP 500", reply: fakeReply{status: http.StatusInternalServerError}, kind: sdkgo.FailureAvailability},
		{name: "HTTP 503", reply: fakeReply{status: http.StatusServiceUnavailable}, kind: sdkgo.FailureAvailability},
		{name: "connection dropped after the request", reply: fakeReply{dropConnection: true}, kind: sdkgo.FailureTransport},
		{name: "malformed JSON", reply: fakeReply{body: `{"errcode":`}, kind: sdkgo.FailureProtocol},
		{name: "oversized body", reply: fakeReply{body: `{"errcode":0,"invaliduser":"` + strings.Repeat("u|", 140000) + `"}`}, kind: sdkgo.FailureResponseTooLarge},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fake := newFakeWeCom(t)
			fake.sendReplies = []fakeReply{testCase.reply, {body: `{"errcode":0,"errmsg":"ok","msgid":"fake-msgid-2"}`}}
			client := newTestClient(t, fake.server.URL)
			_, err := runApplicationSend(t, client, testApplicationText)
			failure, _ := requireRetry(t, err)
			require.Equal(t, testCase.kind, failure.Kind)
			requireSecretFree(t, failure.Message, err.Error())
			sent, err := runApplicationSend(t, client, testApplicationText)
			require.NoError(t, err)
			require.Equal(t, SendApplicationMessageBranchSent, sent.Branch)
			require.Equal(t, "fake-msgid-2", sent.Value.MessageID)
			require.Equal(t, 2, fake.sendCount())
			require.Equal(t, fake.sendBodies[0], fake.sendBodies[1])
			require.Equal(t, 1, fake.tokenCallCount())
		})
	}
}

func TestSendApplicationMessageRetriesAnUnreachableServer(t *testing.T) {
	client := newTestClient(t, unreachableBaseURL(t))
	_, err := runApplicationSend(t, client, testApplicationText)
	failure, _ := requireRetry(t, err)
	require.Equal(t, sdkgo.FailureTransport, failure.Kind)
	requireSecretFree(t, failure.Message, err.Error())
}

// WeCom can revoke a token before expiry (https://developer.work.weixin.qq.com/document/path/91039).
func TestSendApplicationMessageReplacesARejectedToken(t *testing.T) {
	for _, errcode := range []string{"40014", "41001", "42001"} {
		t.Run(errcode, func(t *testing.T) {
			fake := newFakeWeCom(t)
			fake.sendReplies = []fakeReply{{body: `{"errcode":` + errcode + `,"errmsg":"invalid access_token"}`}, {body: `{"errcode":0,"errmsg":"ok","msgid":"fake-msgid-3"}`}}
			client := newTestClient(t, fake.server.URL)
			_, err := runApplicationSend(t, client, testApplicationText)
			failure, _ := requireRetry(t, err)
			require.Equal(t, sdkgo.FailureAuthentication, failure.Kind)
			sent, err := runApplicationSend(t, client, testApplicationText)
			require.NoError(t, err)
			require.Equal(t, SendApplicationMessageBranchSent, sent.Branch)
			require.Equal(t, 2, fake.tokenCallCount())
			require.Equal(t, []string{testAccessTokenPrefix + "1", testAccessTokenPrefix + "2"}, fake.sendTokens)
		})
	}
}

func TestSendApplicationMessageRenewsATokenBeforeItExpires(t *testing.T) {
	fake := newFakeWeCom(t)
	client := newTestClient(t, fake.server.URL)
	now := time.Date(2026, 11, 12, 9, 0, 0, 0, time.UTC)
	client.now = func() time.Time { return now }
	_, err := runApplicationSend(t, client, testApplicationText)
	require.NoError(t, err)
	now = now.Add(7200*time.Second - accessTokenRefreshSkew - time.Second)
	_, err = runApplicationSend(t, client, testApplicationText)
	require.NoError(t, err)
	require.Equal(t, 1, fake.tokenCallCount())
	now = now.Add(time.Second)
	_, err = runApplicationSend(t, client, testApplicationText)
	require.NoError(t, err)
	require.Equal(t, 2, fake.tokenCallCount(), "a token within the refresh skew is renewed")
	require.Equal(t, time.Minute, usableAccessTokenLifetime(2*time.Minute))
}

// An accepted send whose response was lost is suppressed only within duplicate_check_interval.
func TestSendApplicationMessageSelectsUncertainForARetryPastTheDuplicateCheckWindow(t *testing.T) {
	firstAttemptAt := singledispatchtest.NewContext("window-flow", "send-step").FirstAttemptAt()
	for _, testCase := range []struct {
		name        string
		attempt     int
		elapsed     time.Duration
		isUncertain bool
	}{
		{name: "retry just inside the cutoff", attempt: 2, elapsed: 3*time.Hour + 49*time.Minute + 59*time.Second},
		{name: "retry at the cutoff", attempt: 2, elapsed: 3*time.Hour + 50*time.Minute, isUncertain: true},
		{name: "retry after the window", attempt: 9, elapsed: 5 * time.Hour, isUncertain: true},
		{name: "first attempt", attempt: 1, elapsed: 5 * time.Hour},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fake := newFakeWeCom(t)
			client := newTestClient(t, fake.server.URL)
			client.now = func() time.Time { return firstAttemptAt.Add(testCase.elapsed) }
			ctx := singledispatchtest.NewContext("window-flow", "send-step")
			for attempt := 1; attempt < testCase.attempt; attempt++ {
				ctx.AdvanceAttempt()
			}
			result, err := sdkgo.RunMutation(ctx, client.SendApplicationMessage(), testApplicationConnection, testApplicationText)
			require.NoError(t, err)
			if testCase.isUncertain {
				require.Equal(t, SendApplicationMessageBranchUncertain, result.Branch)
				require.Contains(t, result.Failure.Message, "four-hour duplicate check")
				require.Zero(t, fake.tokenCallCount())
				require.Zero(t, fake.sendCount(), "a resend outside the duplicate check could deliver twice")
				return
			}
			require.Equal(t, SendApplicationMessageBranchSent, result.Branch)
			require.Equal(t, 1, fake.sendCount())
		})
	}
	require.Equal(t, 4*time.Hour-10*time.Minute, DuplicateCheckResendCutoff)
}

func TestSendApplicationMessageKeepsATokenPerConnectionSharingAClient(t *testing.T) {
	fake := newFakeWeCom(t)
	secondConnection := sdkgo.ConnectionRef{Provider: "wecom", Name: "wecom-app-2"}
	credentials := testCredentials()
	credentials[secondConnection] = Credentials{
		AuthMethodID: applicationAuthMethodID, CorpID: "ww9f8e7d6c5b4a3a2b1", AgentID: "1000003", CorpSecret: sdkgo.NewSecretString(testCorpSecret + "-second"),
	}
	client, err := New(Config{BaseURL: fake.server.URL}, credentials)
	require.NoError(t, err)
	for range 2 {
		for _, connection := range []sdkgo.ConnectionRef{testApplicationConnection, secondConnection} {
			sent, err := sdkgo.RunMutation(singledispatchtest.NewContext("shared-flow", "send-step"), client.SendApplicationMessage(), connection, testApplicationText)
			require.NoError(t, err)
			require.Equal(t, SendApplicationMessageBranchSent, sent.Branch)
		}
	}
	require.Equal(t, 2, fake.tokenCallCount(), "alternating connections never evict each other's token")
	require.Equal(t, []string{testAccessTokenPrefix + "1", testAccessTokenPrefix + "2", testAccessTokenPrefix + "1", testAccessTokenPrefix + "2"}, fake.sendTokens)
	require.Equal(t, "ww9f8e7d6c5b4a3a2b1", fake.tokenQueries[1].Get("corpid"))

	key := newAccessTokenCacheKey(testCorpID, testCorpSecret)
	require.NotContains(t, fmt.Sprintf("%#v", key), testCorpSecret, "the cache key holds only a secret fingerprint")
	require.NotEqual(t, key, newAccessTokenCacheKey(testCorpID, testCorpSecret+"-rotated"))
}

func TestSendApplicationMessageSharesOneExchangePerKeyAcrossConcurrentConnections(t *testing.T) {
	fake := newFakeWeCom(t)
	fake.tokenDelay = 100 * time.Millisecond
	secondConnection := sdkgo.ConnectionRef{Provider: "wecom", Name: "wecom-app-2"}
	credentials := testCredentials()
	credentials[secondConnection] = Credentials{
		AuthMethodID: applicationAuthMethodID, CorpID: testCorpID, AgentID: "1000003", CorpSecret: sdkgo.NewSecretString(testCorpSecret + "-second"),
	}
	client, err := New(Config{BaseURL: fake.server.URL}, credentials)
	require.NoError(t, err)
	var group sync.WaitGroup
	for index := range 8 {
		connection := []sdkgo.ConnectionRef{testApplicationConnection, secondConnection}[index%2]
		group.Add(1)
		go func() {
			defer group.Done()
			sent, err := sdkgo.RunMutation(singledispatchtest.NewContext("concurrent-flow", "send-step"), client.SendApplicationMessage(), connection, testApplicationText)
			require.NoError(t, err)
			require.Equal(t, SendApplicationMessageBranchSent, sent.Branch)
		}()
	}
	group.Wait()
	require.Equal(t, 2, fake.tokenCallCount(), "one exchange per corp ID and secret")
	require.Equal(t, 8, fake.sendCount())
}

// An idle slot holding an expired token must survive another key's lookup, or two callers of one key could fetch at once.
func TestAccessTokenSlotsAreNeverReplacedWhileAnotherKeyIsActive(t *testing.T) {
	cache := newAccessTokenCache()
	keyA := newAccessTokenCacheKey(testCorpID, testCorpSecret)
	keyB := newAccessTokenCacheKey("ww9f8e7d6c5b4a3a2b1", testCorpSecret+"-second")
	slotA := cache.slotFor(keyA)
	slotA.value, slotA.expiresAt = "expired-token", time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	cache.slotFor(keyB)
	require.Same(t, slotA, cache.slotFor(keyA))
}

func TestTwoConcurrentSendsOfOneKeyExchangeOnceWhileAnotherKeyIsActive(t *testing.T) {
	const otherCorpID = "ww9f8e7d6c5b4a3a2b1"
	releaseFirstExchange := make(chan struct{})
	var releaseOnce sync.Once
	var mutex sync.Mutex
	exchangesByCorpID := map[string]int{}
	sendTokens := []string{}
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cgi-bin/gettoken":
			corpID := r.URL.Query().Get("corpid")
			mutex.Lock()
			exchangesByCorpID[corpID]++
			exchange := exchangesByCorpID[corpID]
			mutex.Unlock()
			if corpID == testCorpID && exchange == 1 {
				<-releaseFirstExchange
			}
			_, err := fmt.Fprintf(w, `{"errcode":0,"errmsg":"ok","access_token":"%s-token-%d","expires_in":7200}`, corpID, exchange)
			require.NoError(t, err)
		case "/cgi-bin/message/send":
			mutex.Lock()
			sendTokens = append(sendTokens, r.URL.Query().Get("access_token"))
			mutex.Unlock()
			_, err := w.Write([]byte(`{"errcode":0,"errmsg":"ok","msgid":"fake-msgid"}`))
			require.NoError(t, err)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(provider.Close)
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseFirstExchange) }) })
	exchangeCount := func(corpID string) int {
		mutex.Lock()
		defer mutex.Unlock()
		return exchangesByCorpID[corpID]
	}
	otherConnection := sdkgo.ConnectionRef{Provider: "wecom", Name: "wecom-app-2"}
	credentials := testCredentials()
	credentials[otherConnection] = Credentials{
		AuthMethodID: applicationAuthMethodID, CorpID: otherCorpID, AgentID: "1000003", CorpSecret: sdkgo.NewSecretString(testCorpSecret + "-second"),
	}
	client, err := New(Config{BaseURL: provider.URL}, credentials)
	require.NoError(t, err)
	send := func(connection sdkgo.ConnectionRef) error {
		sent, err := sdkgo.RunMutation(singledispatchtest.NewContext("single-flight-flow", "send-step"), client.SendApplicationMessage(), connection, testApplicationText)
		if err == nil && sent.Branch != SendApplicationMessageBranchSent {
			return fmt.Errorf("selected %s", sent.Branch)
		}
		return err
	}
	results := make(chan error, 2)
	go func() { results <- send(testApplicationConnection) }()
	require.Eventually(t, func() bool { return exchangeCount(testCorpID) == 1 }, 10*time.Second, 5*time.Millisecond, "the first send holds the key's exchange")
	go func() { results <- send(testApplicationConnection) }()
	require.NoError(t, send(otherConnection), "another key exchanges while the first key's exchange is in flight")
	require.Equal(t, 1, exchangeCount(otherCorpID))
	releaseOnce.Do(func() { close(releaseFirstExchange) })
	require.NoError(t, <-results)
	require.NoError(t, <-results)
	require.Equal(t, 1, exchangeCount(testCorpID), "both sends of one key share one gettoken")
	mutex.Lock()
	defer mutex.Unlock()
	require.ElementsMatch(t, []string{otherCorpID + "-token-1", testCorpID + "-token-1", testCorpID + "-token-1"}, sendTokens)
}

func TestSendApplicationMessageCachesOneTokenPerCredential(t *testing.T) {
	fake := newFakeWeCom(t)
	rotated := testCredentials()
	client := newTestClient(t, fake.server.URL)
	_, err := runApplicationSend(t, client, testApplicationText)
	require.NoError(t, err)
	credentials := rotated[testApplicationConnection]
	credentials.CorpSecret = sdkgo.NewSecretString(testCorpSecret + "-rotated")
	rotated[testApplicationConnection] = credentials
	client.credentials = rotated
	_, err = runApplicationSend(t, client, testApplicationText)
	require.NoError(t, err)
	require.Equal(t, 2, fake.tokenCallCount(), "a rotated secret never reuses the old token")
}

func TestSendApplicationMessageSharesOneTokenExchangeAcrossConcurrentSends(t *testing.T) {
	fake := newFakeWeCom(t)
	fake.tokenDelay = 100 * time.Millisecond
	client := newTestClient(t, fake.server.URL)
	var group sync.WaitGroup
	for range 8 {
		group.Add(1)
		go func() {
			defer group.Done()
			sent, err := runApplicationSend(t, client, testApplicationText)
			require.NoError(t, err)
			require.Equal(t, SendApplicationMessageBranchSent, sent.Branch)
		}()
	}
	group.Wait()
	require.Equal(t, 1, fake.tokenCallCount())
	require.Equal(t, 8, fake.sendCount())
}

// Token errors from https://developer.work.weixin.qq.com/document/path/90313. Wrong secrets are never
// retried because repeated attempts can block the caller IP for an hour.
func TestSendApplicationMessageClassifiesTokenExchangeFailures(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		reply       fakeReply
		isRetryable bool
		kind        sdkgo.FailureKind
		message     string
	}{
		{name: "invalid secret", reply: fakeReply{body: `{"errcode":40001,"errmsg":"invalid credential, hint: corpsecret=` + testCorpSecret + `"}`}, kind: sdkgo.FailureAuthentication, message: "40001"},
		{name: "invalid corp ID", reply: fakeReply{body: `{"errcode":40013,"errmsg":"invalid corpid"}`}, kind: sdkgo.FailureAuthentication, message: "40013"},
		{name: "untrusted IP", reply: fakeReply{body: `{"errcode":60020,"errmsg":"not allow to access from your ip"}`}, kind: sdkgo.FailureAuthorization, message: "企业可信IP"},
		{name: "unexpected HTTP status", reply: fakeReply{status: http.StatusForbidden}, kind: sdkgo.FailureProviderRejection, message: "HTTP"},
		{name: "system busy", reply: fakeReply{body: `{"errcode":-1,"errmsg":"system busy"}`}, isRetryable: true, kind: sdkgo.FailureAvailability},
		{name: "frequency limit", reply: fakeReply{body: `{"errcode":45009,"errmsg":"api freq out of limit"}`}, isRetryable: true, kind: sdkgo.FailureRateLimit},
		{name: "HTTP 502", reply: fakeReply{status: http.StatusBadGateway}, isRetryable: true, kind: sdkgo.FailureAvailability},
		{name: "HTTP 429", reply: fakeReply{status: http.StatusTooManyRequests}, isRetryable: true, kind: sdkgo.FailureRateLimit},
		{name: "malformed JSON", reply: fakeReply{body: `[]`}, isRetryable: true, kind: sdkgo.FailureProtocol},
		{name: "missing token", reply: fakeReply{body: `{"errcode":0,"errmsg":"ok","expires_in":7200}`}, isRetryable: true, kind: sdkgo.FailureProtocol},
		{name: "missing expiry", reply: fakeReply{body: `{"errcode":0,"errmsg":"ok","access_token":"fake-access-token-x"}`}, isRetryable: true, kind: sdkgo.FailureProtocol},
		{name: "connection dropped", reply: fakeReply{dropConnection: true}, isRetryable: true, kind: sdkgo.FailureTransport},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fake := newFakeWeCom(t)
			fake.tokenReplies = []fakeReply{testCase.reply}
			result, err := runApplicationSend(t, newTestClient(t, fake.server.URL), testApplicationText)
			if testCase.isRetryable {
				failure, _ := requireRetry(t, err)
				require.Equal(t, testCase.kind, failure.Kind)
				requireSecretFree(t, failure.Message, err.Error())
			} else {
				require.NoError(t, err)
				require.Equal(t, SendApplicationMessageBranchProviderRejected, result.Branch)
				require.Equal(t, testCase.kind, result.Failure.Kind)
				require.Contains(t, result.Failure.Message, testCase.message)
				requireSecretFree(t, result.Failure.Message)
			}
			require.Zero(t, fake.sendCount())
		})
	}
}

// Send errors from https://developer.work.weixin.qq.com/document/path/90313 and
// https://developer.work.weixin.qq.com/document/path/90236.
func TestSendApplicationMessageSelectsProviderRejectedForDefiniteRefusals(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		errcode string
		kind    sdkgo.FailureKind
	}{
		{name: "no valid recipient", errcode: "81013", kind: sdkgo.FailureProviderRejection},
		{name: "all recipients empty", errcode: "82001", kind: sdkgo.FailureProviderRejection},
		{name: "invalid AgentId", errcode: "40056", kind: sdkgo.FailureProviderRejection},
		{name: "token for another application", errcode: "301002", kind: sdkgo.FailureAuthorization},
		{name: "no API permission", errcode: "48002", kind: sdkgo.FailureAuthorization},
		{name: "untrusted IP", errcode: "60020", kind: sdkgo.FailureAuthorization},
		{name: "content too large", errcode: "45002", kind: sdkgo.FailureProviderRejection},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fake := newFakeWeCom(t)
			fake.sendReplies = []fakeReply{{body: `{"errcode":` + testCase.errcode + `,"errmsg":"refused, hint: [access_token=fake-access-token-1]"}`}}
			rejected, err := runApplicationSend(t, newTestClient(t, fake.server.URL), testApplicationText)
			require.NoError(t, err)
			require.Equal(t, SendApplicationMessageBranchProviderRejected, rejected.Branch)
			require.Equal(t, testCase.kind, rejected.Failure.Kind)
			require.Contains(t, rejected.Failure.Message, testCase.errcode)
			requireSecretFree(t, rejected.Failure.Message)
			require.Equal(t, 1, fake.sendCount())
		})
	}
}

func TestSendApplicationMessageRejectsInvalidInputBeforeAnyRequest(t *testing.T) {
	text := func(change func(*SendApplicationMessageInput)) SendApplicationMessageInput {
		input := SendApplicationMessageInput{ToUserIDs: []string{"zhangsan"}, MessageType: ApplicationMessageText, Content: "hello"}
		change(&input)
		return input
	}
	for _, testCase := range []struct {
		name  string
		input SendApplicationMessageInput
	}{
		{name: "no recipient", input: text(func(input *SendApplicationMessageInput) { input.ToUserIDs = nil })},
		{name: "all members and a list", input: text(func(input *SendApplicationMessageInput) { input.ToAllMembers = true })},
		{name: "@all as a user ID", input: text(func(input *SendApplicationMessageInput) { input.ToUserIDs = []string{"@all"} })},
		{name: "user ID with separator", input: text(func(input *SendApplicationMessageInput) { input.ToUserIDs = []string{"a|b"} })},
		{name: "too many users", input: text(func(input *SendApplicationMessageInput) { input.ToUserIDs = make([]string, 1001) })},
		{name: "non-numeric department", input: text(func(input *SendApplicationMessageInput) { input.ToDepartmentIDs = []string{"ops"} })},
		{name: "too many tags", input: text(func(input *SendApplicationMessageInput) { input.ToTagIDs = make([]string, 101) })},
		{name: "empty content", input: text(func(input *SendApplicationMessageInput) { input.Content = "" })},
		{name: "content over 2048 bytes", input: text(func(input *SendApplicationMessageInput) { input.Content = strings.Repeat("a", 2049) })},
		{name: "unknown type", input: text(func(input *SendApplicationMessageInput) { input.MessageType = "news" })},
		{name: "confidential markdown", input: text(func(input *SendApplicationMessageInput) {
			input.MessageType, input.IsConfidential = ApplicationMessageMarkdown, true
		})},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fake := newFakeWeCom(t)
			defect, err := runApplicationSend(t, newTestClient(t, fake.server.URL), testCase.input)
			require.NoError(t, err)
			require.Equal(t, SendApplicationMessageBranchDefect, defect.Branch)
			require.Equal(t, sdkgo.FailureValidation, defect.Failure.Kind)
			require.Zero(t, fake.tokenCallCount())
			require.Zero(t, fake.sendCount())
		})
	}
}

func TestSendApplicationMessageRequiresValidApplicationCredentials(t *testing.T) {
	fake := newFakeWeCom(t)
	client := newTestClient(t, fake.server.URL)
	defect, err := sdkgo.RunMutation(singledispatchtest.NewContext("method-flow", "send-step"), client.SendApplicationMessage(), testRobotConnection, testApplicationText)
	require.NoError(t, err)
	require.Equal(t, SendApplicationMessageBranchDefect, defect.Branch)
	require.Contains(t, defect.Failure.Message, "application method")

	for _, credentials := range []Credentials{
		{AuthMethodID: applicationAuthMethodID, CorpID: "ww 1", AgentID: testAgentID, CorpSecret: sdkgo.NewSecretString(testCorpSecret)},
		{AuthMethodID: applicationAuthMethodID, CorpID: testCorpID, AgentID: "0", CorpSecret: sdkgo.NewSecretString(testCorpSecret)},
		{AuthMethodID: applicationAuthMethodID, CorpID: testCorpID, AgentID: "app", CorpSecret: sdkgo.NewSecretString(testCorpSecret)},
		{AuthMethodID: applicationAuthMethodID, CorpID: testCorpID, AgentID: testAgentID, CorpSecret: sdkgo.NewSecretString("secret with spaces")},
		{AuthMethodID: applicationAuthMethodID, CorpID: testCorpID, AgentID: testAgentID},
	} {
		invalidClient, err := New(Config{BaseURL: fake.server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testApplicationConnection: credentials})
		require.NoError(t, err)
		defect, err := runApplicationSend(t, invalidClient, testApplicationText)
		require.NoError(t, err)
		require.Equal(t, SendApplicationMessageBranchDefect, defect.Branch)
		require.Equal(t, sdkgo.FailureAuthentication, defect.Failure.Kind)
		requireSecretFree(t, defect.Failure.Message)
	}
	require.Zero(t, fake.tokenCallCount())
}
