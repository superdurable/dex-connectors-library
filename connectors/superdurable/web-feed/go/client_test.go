// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type countingRoundTripper struct{ calls int }

func (transport *countingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	transport.calls++
	return nil, http.ErrNotSupported
}

func TestNewAppliesDefaultsAndValidatesSettings(t *testing.T) {
	client, err := New(Config{}, nil)
	require.NoError(t, err)
	defaults := DefaultConfig()
	require.Equal(t, defaults.UserAgent, client.userAgent)
	require.Equal(t, defaults.MaxDocumentBytes, client.maxDocumentBytes)
	require.Equal(t, defaults.MaxPageBytes, client.maxPageBytes)
	require.Equal(t, defaults.Timeout, client.httpClient.Timeout)
	require.Nil(t, client.httpClient.Jar)

	for _, test := range []struct {
		config Config
		text   string
	}{
		{Config{Timeout: 500 * time.Millisecond}, "timeout"},
		{Config{Timeout: -time.Second}, "timeout"},
		{Config{MaxDocumentBytes: 100}, "maxDocumentBytes"},
		{Config{MaxDocumentBytes: 257 << 20}, "maxDocumentBytes"},
		{Config{MaxPageBytes: 65 << 20}, "maxPageBytes"},
		{Config{UserAgent: "Readeré"}, "userAgent"},
		{Config{UserAgent: "Reader\r\nX-Injected: 1"}, "userAgent"},
		{Config{UserAgent: strings.Repeat("a", 257)}, "userAgent"},
	} {
		_, err := New(test.config, nil)
		require.ErrorContains(t, err, test.text)
	}
	_, err = New(Config{}, nil, nil)
	require.ErrorContains(t, err, "option is nil")
	_, err = New(Config{}, nil, clockTestOption{})
	require.ErrorContains(t, err, "clock is required")
}

func TestWithHTTPClientKeepsThePolicyAndDropsCookies(t *testing.T) {
	transport := &countingRoundTripper{}
	_, err := New(Config{}, nil, WithHTTPClient(&http.Client{Transport: transport}))
	require.ErrorContains(t, err, "must be nil or an *http.Transport", "a custom RoundTripper would bypass the address policy")
	require.Zero(t, transport.calls)

	site := newTestSite(t)
	site.serve("https://blog.example.com/feed.xml", "text/xml", `<rss><channel><title>Cookies</title></channel></rss>`)
	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	jar.SetCookies(&url.URL{Scheme: "https", Host: "blog.example.com"}, []*http.Cookie{{Name: "session", Value: "secret"}})
	base := site.server.Client()
	base.Jar = jar
	base.Timeout = 3 * time.Second
	client, err := New(Config{UserAgent: "ExampleReader/2.0 (+https://www.example.com/reader)"}, nil,
		WithHTTPClient(base),
		addressResolutionTestOption{resolve: site.resolve, dial: site.dial},
		clockTestOption{clock: func() time.Time { return testClockTime }},
	)
	require.NoError(t, err)
	require.Equal(t, 3*time.Second, client.httpClient.Timeout, "a caller timeout is kept")
	require.Same(t, jar, base.Jar, "the caller's client is unchanged")
	result, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/feed.xml"})
	requireBranch(t, result, err, ReadFeedBranchRead, "")
	request := site.recordedRequests()[0]
	require.Empty(t, request.header.Get("Cookie"))
	require.Equal(t, "ExampleReader/2.0 (+https://www.example.com/reader)", request.header.Get("User-Agent"))
}

func TestTextHelpersBoundAndCleanProviderText(t *testing.T) {
	require.Equal(t, "One two", plainTextFromHTML("<p>One</p><p>two</p>"))
	require.Equal(t, "Bold word", plainTextFromHTML("<b>Bo</b>ld <i>word</i>"))
	require.Equal(t, "a < b & c", plainTextFromHTML("a &lt; b &amp; c"))
	require.Equal(t, "Visible", plainTextFromHTML("<style>p{}</style><script>x()</script><svg><text>logo</text></svg>Visible"))
	require.Equal(t, "plain text", plainTextFromHTML("  plain\n\ttext "))
	require.Equal(t, "abcdefghij", truncateCharacters("abcdefghij", 10))
	require.Equal(t, "one two…", truncateCharacters("one two three four", 10))
	require.Equal(t, "日本語…", truncateCharacters("日本語のテキスト", 4))
	require.Equal(t, "…", truncateCharacters("abc", 1))
	require.Empty(t, resolveLink(nil, "/relative"))
	require.Empty(t, resolveLink(mustParseURL(t, "https://www.example.com/"), "javascript:alert(1)"))
	require.Empty(t, resolveLink(mustParseURL(t, "https://www.example.com/"), "https://user:pass@www.example.com/"))
	require.Equal(t, "https://www.example.com/a/b", resolveLink(mustParseURL(t, "https://www.example.com/a/"), " b "))
	long := strings.Repeat("x", maxIdentifierBytes+1)
	require.Equal(t, boundedIdentifier(long), boundedIdentifier(long))
	require.True(t, strings.HasPrefix(boundedIdentifier(long), "sha256:"))
	require.Equal(t, "Avery Chen", rssAuthorName("avery@example.com (Avery Chen)"))
	require.Equal(t, "Avery Chen", rssAuthorName("Avery Chen <avery@example.com>"))
	require.Equal(t, "avery@example.com", rssAuthorName("avery@example.com"))
	require.Equal(t, "Avery Chen", rssAuthorName("Avery Chen"))
}

func mustParseURL(t *testing.T, value string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(value)
	require.NoError(t, err)
	return parsed
}
