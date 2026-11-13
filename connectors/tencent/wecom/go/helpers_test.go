// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wecom

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testWebhookKey = "693a91f6-7a2c-4bc4-97a0-0ec2b1fa5aa0"
	testCorpID     = "ww1a2b3c4d5e6f7a8b"
	testAgentID    = "1000002"
	testCorpSecret = "fake-test-secret_0123456789abcdefghijklmnopq"
)

var (
	testRobotConnection       = sdkgo.ConnectionRef{Provider: "wecom", Name: "wecom-robot"}
	testApplicationConnection = sdkgo.ConnectionRef{Provider: "wecom", Name: "wecom-app"}
)

func testCredentials() sdkgo.StaticCredentialProvider[Credentials] {
	return sdkgo.StaticCredentialProvider[Credentials]{
		testRobotConnection: {AuthMethodID: groupRobotAuthMethodID, WebhookKey: sdkgo.NewSecretString(testWebhookKey)},
		testApplicationConnection: {
			AuthMethodID: applicationAuthMethodID, CorpID: testCorpID, AgentID: testAgentID, CorpSecret: sdkgo.NewSecretString(testCorpSecret),
		},
	}
}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, testCredentials())
	require.NoError(t, err)
	return client
}

// requireRetry asserts a Retry decision and returns its failure and requested delay.
func requireRetry(t *testing.T, err error) (sdkgo.Failure, time.Duration) {
	t.Helper()
	var retryError *sdkgo.RetryError
	require.ErrorAs(t, err, &retryError)
	var delayed *dex.RetryAfterError
	if errors.As(err, &delayed) {
		return retryError.Failure, delayed.After
	}
	return retryError.Failure, 0
}

// requireSecretFree asserts that no credential or token leaks into user-visible text.
func requireSecretFree(t *testing.T, texts ...string) {
	t.Helper()
	for _, text := range texts {
		for _, secret := range []string{testWebhookKey, testCorpSecret, testAccessTokenPrefix, "key=", "corpsecret", "access_token="} {
			require.NotContains(t, text, secret)
		}
	}
}

// closeAfterReadingRequest drops the connection once the request arrived, so its outcome is unknown.
func closeAfterReadingRequest(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	hijacker, ok := w.(http.Hijacker)
	require.True(t, ok)
	connection, _, err := hijacker.Hijack()
	require.NoError(t, err)
	require.NoError(t, connection.Close())
}

// unreachableBaseURL is a loopback address with no listener, so a request never leaves the process.
func unreachableBaseURL(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return "http://" + address
}

// fakeWeCom serves gettoken, message/send, and webhook/send with scripted replies and records requests.
type fakeWeCom struct {
	t      *testing.T
	server *httptest.Server
	mutex  sync.Mutex
	// tokenReplies and sendReplies are consumed in order; the last one repeats.
	tokenReplies []fakeReply
	sendReplies  []fakeReply
	tokenQueries []url.Values
	sendBodies   []string
	sendTokens   []string
	tokenDelay   time.Duration
	issuedTokens int
}

type fakeReply struct {
	status int
	body   string
	// dropConnection closes the connection after reading the request.
	dropConnection bool
}

const testAccessTokenPrefix = "fake-access-token-"

func newFakeWeCom(t *testing.T) *fakeWeCom {
	t.Helper()
	fake := &fakeWeCom{t: t}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(fake.server.Close)
	return fake
}

func (fake *fakeWeCom) serveHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	require.NoError(fake.t, err)
	fake.mutex.Lock()
	var reply fakeReply
	switch r.URL.Path {
	case "/cgi-bin/gettoken":
		fake.tokenQueries = append(fake.tokenQueries, r.URL.Query())
		reply = nextReply(fake.tokenReplies, len(fake.tokenQueries), fake.defaultTokenReply())
		delay := fake.tokenDelay
		fake.mutex.Unlock()
		time.Sleep(delay)
	case "/cgi-bin/message/send", "/cgi-bin/webhook/send":
		fake.sendBodies = append(fake.sendBodies, string(body))
		fake.sendTokens = append(fake.sendTokens, r.URL.Query().Get("access_token"))
		reply = nextReply(fake.sendReplies, len(fake.sendBodies), fakeReply{body: `{"errcode":0,"errmsg":"ok","msgid":"fake-msgid-1"}`})
		fake.mutex.Unlock()
	default:
		fake.mutex.Unlock()
		http.NotFound(w, r)
		return
	}
	if reply.dropConnection {
		closeAfterReadingRequest(fake.t, w)
		return
	}
	if reply.status != 0 {
		w.WriteHeader(reply.status)
	}
	_, err = w.Write([]byte(reply.body))
	require.NoError(fake.t, err)
}

// defaultTokenReply issues a distinct token per exchange, so tests can see which token a send used.
func (fake *fakeWeCom) defaultTokenReply() fakeReply {
	fake.issuedTokens++
	token := testAccessTokenPrefix + strconv.Itoa(fake.issuedTokens)
	return fakeReply{body: `{"errcode":0,"errmsg":"ok","access_token":"` + token + `","expires_in":7200}`}
}

func nextReply(replies []fakeReply, callNumber int, fallback fakeReply) fakeReply {
	if len(replies) == 0 {
		return fallback
	}
	return replies[min(callNumber, len(replies))-1]
}

func (fake *fakeWeCom) tokenCallCount() int {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return len(fake.tokenQueries)
}

func (fake *fakeWeCom) sendCount() int {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	return len(fake.sendBodies)
}
