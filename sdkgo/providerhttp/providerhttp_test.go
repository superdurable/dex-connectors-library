// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package providerhttp_test

import (
	"errors"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

func TestNewProviderHTTPClientCopiesCallerAndNeverFollowsRedirects(t *testing.T) {
	var redirectedRequests int
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirectedRequests++ }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		http.Redirect(response, request, target.URL, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	caller := &http.Client{Transport: http.DefaultTransport}
	hardened := providerhttp.NewProviderHTTPClient(caller, 3*time.Second)
	require.NotSame(t, caller, hardened)
	require.Nil(t, caller.CheckRedirect, "the caller's client is not modified")
	require.Zero(t, caller.Timeout)
	require.Equal(t, 3*time.Second, hardened.Timeout)
	require.Same(t, http.DefaultTransport, hardened.Transport)

	response, err := hardened.Get(origin.URL)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	require.Equal(t, http.StatusTemporaryRedirect, response.StatusCode)
	require.Zero(t, redirectedRequests)

	require.Equal(t, time.Minute, providerhttp.NewProviderHTTPClient(&http.Client{Timeout: time.Minute}, time.Second).Timeout,
		"a caller timeout wins")
	require.Equal(t, 5*time.Second, providerhttp.NewProviderHTTPClient(nil, 5*time.Second).Timeout)
}

func TestValidateBaseURL(t *testing.T) {
	for _, accepted := range []struct{ value, want string }{
		{"https://api.example.test/v1", "https://api.example.test/v1"},
		{"https://api.example.test/v1///", "https://api.example.test/v1"},
		{"https://api.example.test", "https://api.example.test"},
		{"http://127.0.0.1:8080/v1/", "http://127.0.0.1:8080/v1"},
		{"http://localhost:9", "http://localhost:9"},
		{"http://[::1]:9/base", "http://[::1]:9/base"},
	} {
		got, err := providerhttp.ValidateBaseURL(accepted.value)
		require.NoError(t, err, accepted.value)
		require.Equal(t, accepted.want, got)
	}
	for _, rejected := range []string{
		"", "api.example.test/v1", "/v1", "https:api.example.test", "ftp://api.example.test",
		"http://api.example.test/v1", "http://10.0.0.1/v1", "https://user:secret@api.example.test",
		"https://api.example.test/v1?key=secret", "https://api.example.test/v1?", "https://api.example.test/v1#x",
		"https://api.example.test/v1#", "https://api.example.test/ v1", "https://ap\u212ai.example.test",
		"http://localhos\u017f/v1",
	} {
		_, err := providerhttp.ValidateBaseURL(rejected)
		require.Error(t, err, rejected)
		if rejected != "" {
			require.NotContains(t, err.Error(), "secret")
		}
	}
}

func TestIsHeaderSafeCredential(t *testing.T) {
	require.True(t, providerhttp.IsHeaderSafeCredential("sk-abc_123|pipe~"))
	for _, unsafe := range []string{"", "has space", "tab\t", "line\nbreak", "nul\x00", "del\x7f", "café"} {
		require.False(t, providerhttp.IsHeaderSafeCredential(unsafe), "%q", unsafe)
	}
}

func TestReadBoundedBody(t *testing.T) {
	contents, err := providerhttp.ReadBoundedBody(strings.NewReader("12345"), 5)
	require.NoError(t, err)
	require.Equal(t, "12345", string(contents))

	_, err = providerhttp.ReadBoundedBody(strings.NewReader("123456"), 5)
	require.ErrorIs(t, err, providerhttp.ErrBodyTooLarge)

	_, err = providerhttp.ReadBoundedBody(iotest.ErrReader(io.ErrUnexpectedEOF), 5)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
	require.NotErrorIs(t, err, providerhttp.ErrBodyTooLarge)

	contents, err = providerhttp.ReadBoundedBody(strings.NewReader(`{"ok":true}`), math.MaxInt64)
	require.NoError(t, err)
	require.Equal(t, `{"ok":true}`, string(contents), "the largest limit reads the whole body")
}

func TestReadErrorTokensReturnsOnlyBoundedTokens(t *testing.T) {
	body := []byte(`{"error":{"type":"insufficient_quota","code":429,"message":"You exceeded your quota sk-secret",` +
		`"param":"a/b","details":[{"reason":"API_KEY_INVALID"}]},"list":["x.y:z-1"]}`)
	tokens := providerhttp.ReadErrorTokens(body, []string{
		"/error/type", "/error/code", "/error/message", "/error/param", "/error/missing",
		"/error/details/0/reason", "/list/0", "/list/01", "/error",
	})
	require.Equal(t, []string{"insufficient_quota", "429", "API_KEY_INVALID", "x.y:z-1"}, tokens)

	require.Empty(t, providerhttp.ReadErrorTokens([]byte(`<html>Bad Gateway</html>`), []string{"/error/type"}))
	require.Empty(t, providerhttp.ReadErrorTokens(body, nil))
	require.Empty(t, providerhttp.ReadErrorTokens([]byte(`{"error":{"code":"`+strings.Repeat("a", 65)+`"}}`), []string{"/error/code"}))
	require.Equal(t, []string{"slash"}, providerhttp.ReadErrorTokens([]byte(`{"a/b":{"~c":"slash"}}`), []string{"/a~1b/~0c"}))
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	require.Equal(t, 7*time.Second, providerhttp.ParseRetryAfter("7", now))
	require.Equal(t, 7*time.Second, providerhttp.ParseRetryAfter(" 7 ", now))
	require.Zero(t, providerhttp.ParseRetryAfter("0", now))
	require.Zero(t, providerhttp.ParseRetryAfter("", now))
	require.Zero(t, providerhttp.ParseRetryAfter("-3", now))
	require.Zero(t, providerhttp.ParseRetryAfter("1.5", now))
	require.Zero(t, providerhttp.ParseRetryAfter("soon", now))
	require.Equal(t, time.Hour, providerhttp.ParseRetryAfter("86400", now))
	require.Equal(t, time.Hour, providerhttp.ParseRetryAfter("99999999999999999999999", now))
	require.Equal(t, 90*time.Second, providerhttp.ParseRetryAfter(now.Add(90*time.Second).Format(http.TimeFormat), now))
	require.Equal(t, time.Hour, providerhttp.ParseRetryAfter(now.Add(48*time.Hour).Format(http.TimeFormat), now))
	require.Zero(t, providerhttp.ParseRetryAfter(now.Add(-time.Minute).Format(http.TimeFormat), now))
}

func TestServerSentEventReaderToleratesCommentsAndKeepAlives(t *testing.T) {
	stream := ": connected\n\n\n: keep-alive\r\n\r\nevent: delta\ndata: {\"a\":1}\ndata:second\r\nid: 7\nretry: 10\nunknown: x\n\n" +
		"data\n\n" + "event: ignored-without-data\n\n" + "data: last\n\n" + "data: partial-without-blank-line\n"
	reader := providerhttp.NewServerSentEventReader(strings.NewReader(stream), 1<<10, 64)

	event, err := reader.ReadEvent()
	require.NoError(t, err)
	require.Equal(t, providerhttp.ServerSentEvent{Type: "delta", Data: "{\"a\":1}\nsecond"}, event)
	event, err = reader.ReadEvent()
	require.NoError(t, err)
	require.Equal(t, providerhttp.ServerSentEvent{Data: ""}, event)
	event, err = reader.ReadEvent()
	require.NoError(t, err)
	require.Equal(t, providerhttp.ServerSentEvent{Data: "last"}, event)
	_, err = reader.ReadEvent()
	require.ErrorIs(t, err, io.EOF)
}

func TestServerSentEventReaderEnforcesLimits(t *testing.T) {
	keepAlives := strings.Repeat(": keep-alive\n\n", 10)
	_, err := providerhttp.NewServerSentEventReader(strings.NewReader(keepAlives), 50, 64).ReadEvent()
	require.ErrorIs(t, err, providerhttp.ErrStreamTooLarge, "comments and blank lines count toward the total")

	_, err = providerhttp.NewServerSentEventReader(strings.NewReader(keepAlives+"data: x\n\n"), 1<<10, 64).ReadEvent()
	require.NoError(t, err, "keep-alives do not count toward one event")

	longEvent := strings.Repeat("data: 0123456789\n", 10) + "\n"
	_, err = providerhttp.NewServerSentEventReader(strings.NewReader(longEvent), 1<<20, 64).ReadEvent()
	require.ErrorIs(t, err, providerhttp.ErrStreamEventTooLarge)

	longLine := "data: " + strings.Repeat("x", 10000) + "\n\n"
	_, err = providerhttp.NewServerSentEventReader(strings.NewReader(longLine), 1<<20, 64).ReadEvent()
	require.ErrorIs(t, err, providerhttp.ErrStreamEventTooLarge)

	longComment := ":" + strings.Repeat("x", 10000) + "\n\n"
	_, err = providerhttp.NewServerSentEventReader(strings.NewReader(longComment), 1<<20, 64).ReadEvent()
	require.ErrorIs(t, err, providerhttp.ErrStreamEventTooLarge)

	readFailure := errors.New("connection reset")
	_, err = providerhttp.NewServerSentEventReader(io.MultiReader(strings.NewReader("data: x\n"), iotest.ErrReader(readFailure)), 1<<10, 64).ReadEvent()
	require.ErrorIs(t, err, readFailure)

	event, err := providerhttp.NewServerSentEventReader(strings.NewReader("data: x\n\n"), math.MaxInt64, math.MaxInt).ReadEvent()
	require.NoError(t, err, "the largest limits do not overflow")
	require.Equal(t, "x", event.Data)
}
