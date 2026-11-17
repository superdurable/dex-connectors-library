// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const articleTextPageFixture = `<!DOCTYPE html>
<html lang="en"><head>
<meta charset="utf-8">
<title>Faster builds with a shared cache | Example</title>
<meta name="description" content="How the team   cut clean build times in half.">
<meta property="og:title" content="Faster builds with a shared cache">
<meta property="og:site_name" content="Example Engineering">
<meta property="article:published_time" content="2026-11-09T17:00:00+00:00">
<link rel="canonical" href="/blog/faster-builds">
<style>body { color: black; }</style>
<script>window.analytics = "<p>not text</p>";</script>
</head><body>
<header><a href="/">Example</a><nav><a href="/blog">Blog</a> <a href="/about">About</a></nav></header>
<main>
<article class="related"><h2>Related: Release notes</h2><p>Short teaser.</p></article>
<article>
  <header><p class="byline">By Avery Chen</p></header>
  <h1>Faster builds with a shared cache</h1>
  <p>Clean builds used to take   twenty minutes.
     Now they take <em>ten</em>&nbsp;minutes &amp; less.</p>
  <figure><img src="/chart.png" alt="Build times"><figcaption>Build times by week.</figcaption></figure>
  <p>First line<br>Second line</p>
  <ul><li>Share one remote cache.</li><li>Key entries by inputs.</li></ul>
  <aside>Subscribe to the newsletter.</aside>
  <noscript><p>Enable scripts.</p></noscript>
  <svg><text>Chart label</text></svg>
  <form><button>Copy link</button><label>Topic <select><option>All</option></select></label></form>
</article>
</main>
<footer><p>© 2026 Example</p></footer>
</body></html>`

const articleTextFixtureText = "Faster builds with a shared cache\n\n" +
	"Clean builds used to take twenty minutes. Now they take ten minutes & less.\n\n" +
	"Build times by week.\n\n" +
	"First line\nSecond line\n\n" +
	"Share one remote cache.\n\n" +
	"Key entries by inputs.\n\n" +
	"Topic"

func TestReadPageExtractsTheLargestArticle(t *testing.T) {
	site := newTestSite(t)
	site.handle("https://www.example.com/blog/faster-builds", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("ETag", `"a1"`)
		w.Header().Set("Last-Modified", "Mon, 09 Nov 2026 18:00:00 GMT")
		_, _ = w.Write([]byte(articleTextPageFixture))
	})
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadPage(), ReadPageInput{URL: "https://www.example.com/blog/faster-builds?utm_source=feed#top"})
	requireBranch(t, result, err, ReadPageBranchRead, "")
	require.Equal(t, Page{
		FinalURL:     "https://www.example.com/blog/faster-builds?utm_source=feed",
		StatusCode:   http.StatusOK,
		ContentType:  "text/html",
		Title:        "Faster builds with a shared cache | Example",
		Description:  "How the team cut clean build times in half.",
		SiteName:     "Example Engineering",
		CanonicalURL: "https://www.example.com/blog/faster-builds",
		PublishedAt:  timestamp(t, "2026-11-09T17:00:00Z"),
		Text:         articleTextFixtureText,
		ETag:         `"a1"`,
		LastModified: "Mon, 09 Nov 2026 18:00:00 GMT",
		FetchedAt:    testClockTime,
	}, result.Value)
	requests := site.recordedRequests()
	require.Len(t, requests, 1)
	require.Equal(t, readablePageAcceptHeader, requests[0].header.Get("Accept"))
	require.Empty(t, requests[0].header.Get("If-None-Match"))
}

func TestReadPageFallsBackToMainThenBody(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://www.example.com/main", "text/html", `<html><head>
<meta property="og:title" content="Open Graph title">
<meta property="og:description" content="Open Graph description">
<script type="application/ld+json">{"@type": "NewsArticle", "datePublished": "2026-11-01T08:00:00Z"}</script>
</head><body><div>Outside main.</div><main><h1>Inside main</h1><div>Second block <b>bold</b>text.</div></main></body></html>`)
	site.serve("https://www.example.com/body", "", `<html><body><nav>Menu</nav><div>Plain   body
text.</div><p>Another paragraph.</p><aside>Ad</aside></body></html>`)
	client := site.newClient(t, Config{})

	result, err := runQuery(t, client.ReadPage(), ReadPageInput{URL: "https://www.example.com/main"})
	requireBranch(t, result, err, ReadPageBranchRead, "")
	require.Equal(t, "Inside main\n\nSecond block boldtext.", result.Value.Text)
	require.Equal(t, "Open Graph title", result.Value.Title)
	require.Equal(t, "Open Graph description", result.Value.Description)
	require.Equal(t, timestamp(t, "2026-11-01T08:00:00Z"), result.Value.PublishedAt)

	result, err = runQuery(t, client.ReadPage(), ReadPageInput{URL: "https://www.example.com/body"})
	requireBranch(t, result, err, ReadPageBranchRead, "")
	require.Equal(t, "Plain body text.\n\nAnother paragraph.", result.Value.Text)
	require.Equal(t, "text/html", result.Value.ContentType, "a missing media type reads as HTML")
	require.Nil(t, result.Value.PublishedAt)
	require.Empty(t, result.Value.Title)
}

func TestReadPageReturnsPlainTextAsIs(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://www.example.com/notes.txt", "text/plain; charset=utf-8", "\ufeff\n  Release notes\n\n- <b>faster</b> builds\n  - indented\n\n")
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadPage(), ReadPageInput{URL: "https://www.example.com/notes.txt"})
	requireBranch(t, result, err, ReadPageBranchRead, "")
	require.Equal(t, Page{
		FinalURL: "https://www.example.com/notes.txt", StatusCode: http.StatusOK, ContentType: "text/plain",
		Text: "Release notes\n\n- <b>faster</b> builds\n  - indented", FetchedAt: testClockTime,
	}, result.Value)
}

func TestReadPageTruncatesTheText(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://www.example.com/long", "text/html", `<article><p>`+strings.Repeat("word ", 40)+`</p><p>tail</p></article>`)
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadPage(), ReadPageInput{URL: "https://www.example.com/long", MaxTextCharacters: 23})
	requireBranch(t, result, err, ReadPageBranchRead, "")
	require.Equal(t, "word word word word…", result.Value.Text)
	require.True(t, result.Value.IsTextTruncated)

	result, err = runQuery(t, client.ReadPage(), ReadPageInput{URL: "https://www.example.com/long"})
	requireBranch(t, result, err, ReadPageBranchRead, "")
	require.False(t, result.Value.IsTextTruncated)
	require.True(t, strings.HasSuffix(result.Value.Text, "word\n\ntail"))

	text, isTruncated := truncateText("first paragraph\n\nsecond", 18)
	require.Equal(t, "first paragraph…", text)
	require.True(t, isTruncated)
	text, isTruncated = truncateText("abcdef", 1)
	require.Equal(t, "…", text)
	require.True(t, isTruncated)
}

func TestReadPageConditionalGet(t *testing.T) {
	site := newTestSite(t)
	site.handle("https://www.example.com/post", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"a1"` {
			w.Header().Set("ETag", `"a2"`)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusNotModified)
	})
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadPage(), ReadPageInput{URL: "https://www.example.com/post", ETag: `"a1"`, LastModified: "Mon, 09 Nov 2026 18:00:00 GMT"})
	requireBranch(t, result, err, ReadPageBranchNotModified, "")
	require.Equal(t, Page{
		FinalURL: "https://www.example.com/post", StatusCode: http.StatusNotModified, ETag: `"a2"`,
		LastModified: "Mon, 09 Nov 2026 18:00:00 GMT", FetchedAt: testClockTime,
	}, result.Value)
	require.Equal(t, "Mon, 09 Nov 2026 18:00:00 GMT", site.recordedRequests()[0].header.Get("If-Modified-Since"))

	result, err = runQuery(t, client.ReadPage(), ReadPageInput{URL: "https://www.example.com/post"})
	requireBranch(t, result, err, ReadPageBranchInvalidResponse, sdkgo.FailureProtocol)
	require.Contains(t, result.Failure.Message, "without validators")
}

func TestReadPageFollowsRedirects(t *testing.T) {
	site := newTestSite(t)
	site.handle("https://old.example.com/post", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://www.example.com/blog/post", http.StatusMovedPermanently)
	})
	site.serve("https://www.example.com/blog/post", "application/xhtml+xml", `<html><body><article><p>Moved post.</p></article></body></html>`)
	site.handle("https://loop.example.com/a", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/a", http.StatusFound)
	})
	site.handle("https://www.example.com/to-internal", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://internal.example.com/post", http.StatusFound)
	})
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadPage(), ReadPageInput{URL: "http://old.example.com/post"})
	requireBranch(t, result, err, ReadPageBranchRead, "")
	require.Equal(t, "https://www.example.com/blog/post", result.Value.FinalURL)
	require.Equal(t, "application/xhtml+xml", result.Value.ContentType)
	require.Equal(t, "Moved post.", result.Value.Text)

	result, err = runQuery(t, client.ReadPage(), ReadPageInput{URL: "https://loop.example.com/a"})
	requireBranch(t, result, err, ReadPageBranchInvalidResponse, sdkgo.FailureProtocol)
	require.Contains(t, result.Failure.Message, "redirected more than 5 times")

	result, err = runQuery(t, client.ReadPage(), ReadPageInput{URL: "https://www.example.com/to-internal"})
	requireBranch(t, result, err, ReadPageBranchUnreachable, sdkgo.FailureValidation)
	require.NotContains(t, site.dialedAddresses(), "10.0.0.5:443")
}

func TestReadPageClassifiesResponses(t *testing.T) {
	site := newTestSite(t)
	status := func(code int) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(code) }
	}
	site.handle("https://www.example.com/404", status(http.StatusNotFound))
	site.handle("https://www.example.com/410", status(http.StatusGone))
	site.handle("https://www.example.com/403", status(http.StatusForbidden))
	site.serve("https://www.example.com/pdf", "application/pdf", "%PDF-1.7")
	site.serve("https://www.example.com/json", "application/json", `{"text": "not a page"}`)
	site.serve("https://www.example.com/large", "text/html", "<p>"+strings.Repeat("a", 2048)+"</p>")
	client := site.newClient(t, Config{MaxDocumentBytes: 1024})
	for _, test := range []struct {
		path   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		text   string
	}{
		{"404", ReadPageBranchNotFound, sdkgo.FailureNotFound, "HTTP 404"},
		{"410", ReadPageBranchNotFound, sdkgo.FailureNotFound, "HTTP 410"},
		{"403", ReadPageBranchProviderRejected, sdkgo.FailureProviderRejection, "HTTP 403"},
		{"pdf", ReadPageBranchInvalidResponse, sdkgo.FailureProtocol, "serves application/pdf, not an HTML or plain-text page"},
		{"json", ReadPageBranchInvalidResponse, sdkgo.FailureProtocol, "serves application/json"},
		{"large", ReadPageBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "larger than the connection's limit for an HTML or plain-text page"},
	} {
		t.Run(test.path, func(t *testing.T) {
			result, err := runQuery(t, client.ReadPage(), ReadPageInput{URL: "https://www.example.com/" + test.path})
			requireBranch(t, result, err, test.branch, test.kind)
			require.Contains(t, result.Failure.Message, test.text)
			require.Equal(t, "readPage", result.Failure.Operation)
		})
	}
}

func TestReadPageRetriesTransientFailures(t *testing.T) {
	site := newTestSite(t)
	site.handle("https://www.example.com/429", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	site.handle("https://www.example.com/502", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	})
	site.handle("https://www.example.com/dropped", func(w http.ResponseWriter, _ *http.Request) {
		connection, _, err := w.(http.Hijacker).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	})
	client := site.newClient(t, Config{})
	for _, test := range []struct {
		path  string
		after time.Duration
	}{{"429", 7 * time.Second}, {"502", 0}, {"dropped", 0}} {
		t.Run(test.path, func(t *testing.T) {
			_, err := runQuery(t, client.ReadPage(), ReadPageInput{URL: "https://www.example.com/" + test.path})
			require.Error(t, err)
			var retryAfter *dex.RetryAfterError
			if test.after > 0 {
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, test.after, retryAfter.After)
			} else {
				require.False(t, errors.As(err, &retryAfter))
			}
			require.NotContains(t, err.Error(), "www.example.com")
		})
	}
}

func TestReadPageRefusesInvalidInputAndUnsafeTargets(t *testing.T) {
	site := newTestSite(t)
	client := site.newClient(t, Config{})
	for _, input := range []ReadPageInput{
		{URL: ""},
		{URL: "post.html"},
		{URL: "https://user:secret@www.example.com/post"},
		{URL: "https://www.example.com/post", MaxTextCharacters: -1},
		{URL: "https://www.example.com/post", MaxTextCharacters: maxPageTextCharacters + 1},
		{URL: "https://www.example.com/post", ETag: "\"café\""},
	} {
		result, err := runQuery(t, client.ReadPage(), input)
		requireBranch(t, result, err, ReadPageBranchDefect, sdkgo.FailureValidation)
		require.NotContains(t, result.Failure.Message, "secret")
	}
	result, err := runQuery(t, client.ReadPage(), ReadPageInput{URL: "https://shared.example.com/post"})
	requireBranch(t, result, err, ReadPageBranchUnreachable, sdkgo.FailureValidation)
	result, err = runQuery(t, client.ReadPage(), ReadPageInput{URL: "https://missing.example.com/post"})
	requireBranch(t, result, err, ReadPageBranchUnreachable, sdkgo.FailureTransport)
	require.Empty(t, site.recordedRequests(), "no refused or invalid target reached the site")
}

func TestExtractReadableTextHandlesUnusualMarkup(t *testing.T) {
	for _, test := range []struct{ name, html, text string }{
		{"empty article falls back to the body", `<article><nav>Only nav</nav></article><p>Body text.</p>`, "Body text."},
		{"nested articles count as one region", `<article><p>Outer.</p><article><p>Inner.</p></article></article><article><p>x</p></article>`, "Outer.\n\nInner."},
		{"a tie keeps the first article", `<article><p>One.</p></article><article><p>Two.</p></article>`, "One."},
		{"self-closing tags break paragraphs", `<p>Above<hr/>Below<br/>next</p>`, "Above\n\nBelow\nnext"},
		{"entities decode", `<p>Caf&eacute; &lt;tag&gt; &#x2014; done</p>`, "Café <tag> — done"},
		{"comments are dropped", `<p>Visible<!-- hidden --> text</p>`, "Visible text"},
		{"an unclosed nav ends with its parent", `<aside><nav><a>Menu</a><nav><a>More</a></aside><main><p>Kept.</p></main>`, "Kept."},
		{"an unmatched end tag is ignored", `<main><p>One</nav></p><p>Two</p></main>`, "One\n\nTwo"},
		{"paragraphs close implicitly", `<p>One<p>Two<div>Three</div>`, "One\n\nTwo\n\nThree"},
		{"self-closing tags close only in svg", `<svg><path/><title>Icon</title></svg><article><div/>Kept.</article>`, "Kept."},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.Equal(t, test.text, extractReadableText([]byte(test.html), "text/html; charset=utf-8"))
		})
	}
	require.Equal(t, "café", extractReadableText([]byte("<p>caf\xe9</p>"), "text/html; charset=iso-8859-1"))
}
