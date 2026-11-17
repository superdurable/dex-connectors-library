// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webfeed

import (
	"bytes"
	"compress/gzip"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const rssFixture = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:content="http://purl.org/rss/1.0/modules/content/" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:media="http://search.yahoo.com/mrss/">
<channel>
  <title>Example &amp;amp; Friends</title>
  <link>https://blog.example.com/</link>
  <atom:link href="https://blog.example.com/feed.xml" rel="self" type="application/rss+xml"/>
  <description><![CDATA[<p>Posts from the <b>example</b> team.</p>]]></description>
  <lastBuildDate>Tue, 10 Nov 2026 09:30:00 GMT</lastBuildDate>
  <item>
    <title>Don&amp;#8217;t repeat yourself</title>
    <media:title>Thumbnail caption that must not win</media:title>
    <link>/posts/dry</link>
    <guid isPermaLink="false">post-42</guid>
    <pubDate>Tue, 3 Nov 2026 10:00:00 EST</pubDate>
    <author>avery@example.com (Avery Chen)</author>
    <dc:creator>Jordan Lee</dc:creator>
    <category>Tooling</category>
    <category>Tooling</category>
    <category>builds</category>
    <description><![CDATA[<p>First <em>paragraph</em>.</p><script>alert(1)</script><p>Second paragraph that is long enough to be cut by the summary bound.</p>]]></description>
  </item>
  <item>
    <title>Permalink only</title>
    <guid>https://blog.example.com/posts/permalink</guid>
    <pubDate>Monday, 02-Nov-2026 08:00:00 GMT</pubDate>
    <content:encoded><![CDATA[<div>Encoded body</div>]]></content:encoded>
  </item>
  <item>
    <title>No identity</title>
    <pubDate>not a date</pubDate>
  </item>
</channel>
</rss>`

func TestReadFeedParsesRSS(t *testing.T) {
	site := newTestSite(t)
	site.handle("https://blog.example.com/feed.xml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Last-Modified", "Tue, 10 Nov 2026 09:30:00 GMT")
		w.Header().Set("Set-Cookie", "session=tracking")
		_, _ = w.Write([]byte(rssFixture))
	})
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/feed.xml#top", Limit: 2, MaxSummaryCharacters: 40})
	requireBranch(t, result, err, ReadFeedBranchRead, "")
	feed := result.Value
	require.Equal(t, FeedFormatRSS, feed.Format)
	require.Equal(t, "Example & Friends", feed.Title)
	require.Equal(t, "https://blog.example.com/", feed.SiteURL)
	require.Equal(t, "Posts from the example team.", feed.Description)
	require.Equal(t, timestamp(t, "2026-11-10T09:30:00Z"), feed.UpdatedAt)
	require.Equal(t, "https://blog.example.com/feed.xml", feed.FinalURL)
	require.Equal(t, `"v1"`, feed.ETag)
	require.Equal(t, "Tue, 10 Nov 2026 09:30:00 GMT", feed.LastModified)
	require.Equal(t, testClockTime, feed.FetchedAt)
	require.Equal(t, 3, feed.ItemCount)
	require.True(t, feed.HasMore)
	require.Len(t, feed.Items, 2)
	require.Equal(t, FeedItem{
		ID: "post-42", Title: "Don’t repeat yourself", Link: "https://blog.example.com/posts/dry",
		PublishedAt: timestamp(t, "2026-11-03T15:00:00Z"), Summary: "First paragraph. Second paragraph that…",
		Authors: []string{"Avery Chen", "Jordan Lee"}, Categories: []string{"Tooling", "builds"},
	}, feed.Items[0])
	require.Equal(t, "https://blog.example.com/posts/permalink", feed.Items[1].ID)
	require.Equal(t, "https://blog.example.com/posts/permalink", feed.Items[1].Link)
	require.Equal(t, timestamp(t, "2026-11-02T08:00:00Z"), feed.Items[1].PublishedAt)
	require.Equal(t, "Encoded body", feed.Items[1].Summary)

	requests := site.recordedRequests()
	require.Len(t, requests, 1)
	require.Equal(t, "/feed.xml", requests[0].path, "the fragment is never sent")
	require.Contains(t, requests[0].header.Get("Accept"), "application/rss+xml")
	require.Equal(t, DefaultConfig().UserAgent, requests[0].header.Get("User-Agent"))
	require.Empty(t, requests[0].header.Get("If-None-Match"))
	require.Empty(t, requests[0].header.Get("Authorization"))
	require.Empty(t, requests[0].header.Get("Cookie"))
	require.Equal(t, []string{"203.0.113.10:443"}, site.dialedAddresses())
}

func TestReadFeedFallsBackToADigestIdentity(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://blog.example.com/feed.xml", "text/xml", rssFixture)
	client := site.newClient(t, Config{})
	first, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/feed.xml"})
	requireBranch(t, first, err, ReadFeedBranchRead, "")
	second, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/feed.xml"})
	requireBranch(t, second, err, ReadFeedBranchRead, "")
	anonymous := first.Value.Items[2]
	require.True(t, strings.HasPrefix(anonymous.ID, "sha256:"), anonymous.ID)
	require.Len(t, anonymous.ID, len("sha256:")+64)
	require.Equal(t, anonymous.ID, second.Value.Items[2].ID, "the fallback identity is stable across reads")
	require.Nil(t, anonymous.PublishedAt, "an unreadable date is omitted")
	require.Empty(t, anonymous.Link)
}

func TestReadFeedParsesAtom(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://blog.example.com/atom.xml", "application/atom+xml", `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom" xml:base="https://blog.example.com/blog/">
  <title type="html">Example &lt;b&gt;Engineering&lt;/b&gt;</title>
  <subtitle>Notes &amp; more</subtitle>
  <link rel="self" href="https://blog.example.com/atom.xml"/>
  <link rel="alternate" type="text/html" href="./"/>
  <updated>2026-11-10T09:30:00+01:00</updated>
  <author><name>Example Team</name></author>
  <entry xml:base="posts/">
    <title type="text">Why a &lt; b matters</title>
    <id>tag:blog.example.com,2026:42</id>
    <link rel="enclosure" type="audio/mpeg" href="episode.mp3"/>
    <link rel="alternate" type="application/pdf" href="post.pdf"/>
    <link rel="alternate" type="text/html" href="faster-builds"/>
    <published>2026-11-09T08:00:00Z</published>
    <updated>2026-11-10T08:00:00.5Z</updated>
    <summary type="text">Plain &lt;text&gt; summary</summary>
    <category term="builds" label="Builds"/>
    <category label="Label only"/>
  </entry>
  <entry>
    <title>XHTML content</title>
    <id>tag:blog.example.com,2026:41</id>
    <link href="https://other.example.com/41"/>
    <author><name>Avery Chen</name></author>
    <updated>2026-11-01</updated>
    <summary type="html"></summary>
    <content type="xhtml"><div xmlns="http://www.w3.org/1999/xhtml">First <b>bold</b> word.</div></content>
  </entry>
</feed>`)
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/atom.xml"})
	requireBranch(t, result, err, ReadFeedBranchRead, "")
	feed := result.Value
	require.Equal(t, FeedFormatAtom, feed.Format)
	require.Equal(t, "Example Engineering", feed.Title)
	require.Equal(t, "Notes & more", feed.Description)
	require.Equal(t, "https://blog.example.com/blog/", feed.SiteURL)
	require.Equal(t, timestamp(t, "2026-11-10T08:30:00Z"), feed.UpdatedAt)
	require.Equal(t, FeedItem{
		ID: "tag:blog.example.com,2026:42", Title: "Why a < b matters", Link: "https://blog.example.com/blog/posts/faster-builds",
		PublishedAt: timestamp(t, "2026-11-09T08:00:00Z"), UpdatedAt: timestamp(t, "2026-11-10T08:00:00.5Z"),
		Summary: "Plain <text> summary", Authors: []string{"Example Team"}, Categories: []string{"builds", "Label only"},
	}, feed.Items[0])
	require.Equal(t, "https://other.example.com/41", feed.Items[1].Link)
	require.Equal(t, []string{"Avery Chen"}, feed.Items[1].Authors)
	require.Equal(t, "First bold word.", feed.Items[1].Summary, "xhtml text keeps document order")
	require.Equal(t, timestamp(t, "2026-11-01T00:00:00Z"), feed.Items[1].UpdatedAt)
	require.Equal(t, 2, feed.ItemCount)
	require.False(t, feed.HasMore)
}

func TestReadFeedParsesRDF(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://news.example.com/index.rdf", "application/rdf+xml", `<?xml version="1.0"?>
<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#" xmlns="http://purl.org/rss/1.0/" xmlns:dc="http://purl.org/dc/elements/1.1/">
  <channel rdf:about="https://news.example.com/">
    <title>Example News</title>
    <link>https://news.example.com/</link>
    <description>Daily notes</description>
    <dc:date>2026-11-10T06:00:00+00:00</dc:date>
  </channel>
  <item rdf:about="https://news.example.com/story/7">
    <title>Story seven</title>
    <link>https://news.example.com/story/7</link>
    <description>Short &lt;i&gt;story&lt;/i&gt;.</description>
    <dc:creator>Sam Rivera</dc:creator>
    <dc:subject>science</dc:subject>
    <dc:date>2026-11-09T21:15:00-05:00</dc:date>
  </item>
</rdf:RDF>`)
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://news.example.com/index.rdf"})
	requireBranch(t, result, err, ReadFeedBranchRead, "")
	require.Equal(t, FeedFormatRDF, result.Value.Format)
	require.Equal(t, "Example News", result.Value.Title)
	require.Equal(t, timestamp(t, "2026-11-10T06:00:00Z"), result.Value.UpdatedAt)
	require.Equal(t, []FeedItem{{
		ID: "https://news.example.com/story/7", Title: "Story seven", Link: "https://news.example.com/story/7",
		PublishedAt: timestamp(t, "2026-11-10T02:15:00Z"), Summary: "Short story.",
		Authors: []string{"Sam Rivera"}, Categories: []string{"science"},
	}}, result.Value.Items)
}

func TestReadFeedParsesJSONFeed(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://blog.example.com/feed.json", "application/feed+json", `{
  "version": "https://jsonfeed.org/version/1.1",
  "title": "Example JSON",
  "home_page_url": "https://blog.example.com/",
  "description": "A JSON feed",
  "authors": [{"name": "Example Team"}],
  "items": [
    {"id": "1", "url": "/posts/one", "title": "One", "summary": "Plain summary", "content_html": "<p>ignored</p>",
     "date_published": "2026-11-09T10:00:00-08:00", "date_modified": "2026-11-09T19:00:00Z", "tags": ["a", "b"],
     "authors": [{"name": "Avery Chen"}]},
    {"id": 2, "external_url": "https://elsewhere.example.com/two", "content_html": "<p>HTML <b>only</b></p>"},
    {"url": "https://blog.example.com/posts/three", "content_text": "Text only", "author": {"name": "Jordan Lee"}}
  ]
}`)
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/feed.json"})
	requireBranch(t, result, err, ReadFeedBranchRead, "")
	feed := result.Value
	require.Equal(t, FeedFormatJSON, feed.Format)
	require.Equal(t, "Example JSON", feed.Title)
	require.Equal(t, "https://blog.example.com/", feed.SiteURL)
	require.Equal(t, FeedItem{
		ID: "1", Title: "One", Link: "https://blog.example.com/posts/one",
		PublishedAt: timestamp(t, "2026-11-09T18:00:00Z"), UpdatedAt: timestamp(t, "2026-11-09T19:00:00Z"),
		Summary: "Plain summary", Authors: []string{"Avery Chen"}, Categories: []string{"a", "b"},
	}, feed.Items[0])
	require.Equal(t, "2", feed.Items[1].ID, "a numeric id is kept as its decimal text")
	require.Equal(t, "https://elsewhere.example.com/two", feed.Items[1].Link)
	require.Equal(t, "HTML only", feed.Items[1].Summary)
	require.Equal(t, []string{"Example Team"}, feed.Items[1].Authors, "items inherit the feed authors")
	require.Equal(t, "https://blog.example.com/posts/three", feed.Items[2].ID)
	require.Equal(t, []string{"Jordan Lee"}, feed.Items[2].Authors, "JSON Feed 1.0 author is read")
}

func TestReadFeedDecodesDeclaredAndHTTPCharsets(t *testing.T) {
	site := newTestSite(t)
	declared := append([]byte(`<?xml version="1.0" encoding="ISO-8859-1"?><rss version="2.0"><channel><title>Caf`), 0xe9)
	declared = append(declared, []byte(`</title></channel></rss>`)...)
	site.serve("https://blog.example.com/latin1.xml", "text/xml", string(declared))
	undeclared := append([]byte(`<rss version="2.0"><channel><title>Na`), 0xef)
	undeclared = append(undeclared, []byte(`ve</title></channel></rss>`)...)
	site.serve("https://blog.example.com/windows.xml", "text/xml; charset=windows-1252", string(undeclared))
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/latin1.xml"})
	requireBranch(t, result, err, ReadFeedBranchRead, "")
	require.Equal(t, "Café", result.Value.Title)
	result, err = runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/windows.xml"})
	requireBranch(t, result, err, ReadFeedBranchRead, "")
	require.Equal(t, "Naïve", result.Value.Title)
	require.Empty(t, result.Value.Items)
	require.NotNil(t, result.Value.Items, "items is an empty list, never null")
}

func TestReadFeedConditionalGet(t *testing.T) {
	site := newTestSite(t)
	site.handle("https://blog.example.com/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"v1"` || r.Header.Get("If-Modified-Since") == "Tue, 10 Nov 2026 09:30:00 GMT" {
			w.Header().Set("ETag", `"v2"`)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusNotModified)
	})
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/feed.xml", ETag: `"v1"`, LastModified: "Mon, 09 Nov 2026 00:00:00 GMT"})
	requireBranch(t, result, err, ReadFeedBranchNotModified, "")
	require.Equal(t, Feed{
		FinalURL: "https://blog.example.com/feed.xml", ETag: `"v2"`, LastModified: "Mon, 09 Nov 2026 00:00:00 GMT",
		FetchedAt: testClockTime, Items: []FeedItem{},
	}, result.Value, "a 304 refreshes the etag and keeps a validator it does not resend")
	requests := site.recordedRequests()
	require.Equal(t, `"v1"`, requests[0].header.Get("If-None-Match"))
	require.Equal(t, "Mon, 09 Nov 2026 00:00:00 GMT", requests[0].header.Get("If-Modified-Since"))

	result, err = runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/feed.xml", LastModified: "Tue, 10 Nov 2026 09:30:00 GMT"})
	requireBranch(t, result, err, ReadFeedBranchNotModified, "")
	require.Empty(t, site.recordedRequests()[1].header.Get("If-None-Match"))

	result, err = runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/feed.xml"})
	requireBranch(t, result, err, ReadFeedBranchInvalidResponse, sdkgo.FailureProtocol)
	require.Contains(t, result.Failure.Message, "without validators")
}

func TestReadFeedFollowsRedirectsThroughTheAddressPolicy(t *testing.T) {
	site := newTestSite(t)
	site.handle("https://old.example.com/feed", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://blog.example.com/moved?format=rss", http.StatusMovedPermanently)
	})
	site.handle("https://blog.example.com/moved", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final.xml", http.StatusTemporaryRedirect)
	})
	site.serve("https://blog.example.com/final.xml", "text/xml", `<rss><channel><title>Moved</title></channel></rss>`)
	site.handle("https://loop.example.com/a", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/a", http.StatusFound)
	})
	site.handle("https://blog.example.com/to-internal", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://internal.example.com/feed", http.StatusFound)
	})
	site.handle("https://blog.example.com/to-ftp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "ftp://blog.example.com/feed")
		w.WriteHeader(http.StatusFound)
	})
	client := site.newClient(t, Config{})

	result, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "http://old.example.com/feed"})
	requireBranch(t, result, err, ReadFeedBranchRead, "")
	require.Equal(t, "Moved", result.Value.Title)
	require.Equal(t, "https://blog.example.com/final.xml", result.Value.FinalURL)
	var paths []string
	for _, request := range site.recordedRequests() {
		paths = append(paths, request.host+request.path)
	}
	require.Equal(t, []string{"old.example.com/feed", "blog.example.com/moved?format=rss", "blog.example.com/final.xml"}, paths, "http targets are requested over https")

	before := len(site.recordedRequests())
	result, err = runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://loop.example.com/a"})
	requireBranch(t, result, err, ReadFeedBranchInvalidResponse, sdkgo.FailureProtocol)
	require.Contains(t, result.Failure.Message, "redirected more than 5 times")
	require.Len(t, site.recordedRequests(), before+6)

	result, err = runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/to-internal"})
	requireBranch(t, result, err, ReadFeedBranchUnreachable, sdkgo.FailureValidation)
	require.Contains(t, result.Failure.Message, "only public addresses are allowed")
	require.NotContains(t, site.dialedAddresses(), "10.0.0.5:443")

	result, err = runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/to-ftp"})
	requireBranch(t, result, err, ReadFeedBranchInvalidResponse, sdkgo.FailureProtocol)
	require.Contains(t, result.Failure.Message, "redirect")
}

func TestReadFeedClassifiesResponses(t *testing.T) {
	site := newTestSite(t)
	status := func(code int, header map[string]string) http.HandlerFunc {
		return func(w http.ResponseWriter, _ *http.Request) {
			for name, value := range header {
				w.Header().Set(name, value)
			}
			w.WriteHeader(code)
		}
	}
	site.handle("https://blog.example.com/404", status(http.StatusNotFound, nil))
	site.handle("https://blog.example.com/410", status(http.StatusGone, nil))
	site.handle("https://blog.example.com/401", status(http.StatusUnauthorized, nil))
	site.handle("https://blog.example.com/403", status(http.StatusForbidden, nil))
	site.handle("https://blog.example.com/204", status(http.StatusNoContent, nil))
	site.serve("https://blog.example.com/image", "image/png", "\x89PNG")
	site.serve("https://blog.example.com/page", "text/html; charset=utf-8", `<!DOCTYPE html><html><head><title>Blog</title></head><body><p>Hi</p></body></html>`)
	site.serve("https://blog.example.com/sitemap", "application/xml", `<urlset><url><loc>https://blog.example.com/</loc></url></urlset>`)
	site.serve("https://blog.example.com/broken", "application/xml", `<rss><channel><title>Broken</title></channel>`+"\x00")
	site.serve("https://blog.example.com/api", "application/json", `{"data": []}`)
	site.serve("https://blog.example.com/empty", "text/xml", "  \n")
	site.serve("https://blog.example.com/large", "text/xml", "<rss>"+strings.Repeat(" ", 2048)+"</rss>")
	site.handle("https://blog.example.com/streamed", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/xml")
		w.WriteHeader(http.StatusOK)
		for range 3 {
			_, _ = w.Write([]byte(strings.Repeat(" ", 1000)))
			w.(http.Flusher).Flush()
		}
	})
	client := site.newClient(t, Config{MaxDocumentBytes: 1024})
	for _, test := range []struct {
		path   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		text   string
	}{
		{"404", ReadFeedBranchNotFound, sdkgo.FailureNotFound, "HTTP 404"},
		{"410", ReadFeedBranchNotFound, sdkgo.FailureNotFound, "HTTP 410"},
		{"401", ReadFeedBranchProviderRejected, sdkgo.FailureProviderRejection, "HTTP 401"},
		{"403", ReadFeedBranchProviderRejected, sdkgo.FailureProviderRejection, "HTTP 403"},
		{"204", ReadFeedBranchInvalidResponse, sdkgo.FailureProtocol, "HTTP 204"},
		{"image", ReadFeedBranchInvalidResponse, sdkgo.FailureProtocol, "serves image/png, not a feed"},
		{"page", ReadFeedBranchInvalidResponse, sdkgo.FailureProtocol, "HTML page"},
		{"sitemap", ReadFeedBranchInvalidResponse, sdkgo.FailureProtocol, "not an RSS, RDF, or Atom feed"},
		{"broken", ReadFeedBranchInvalidResponse, sdkgo.FailureProtocol, "malformed"},
		{"api", ReadFeedBranchInvalidResponse, sdkgo.FailureProtocol, "JSON Feed 1.x"},
		{"empty", ReadFeedBranchInvalidResponse, sdkgo.FailureProtocol, "empty"},
		{"large", ReadFeedBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "larger than the connection's limit"},
		{"streamed", ReadFeedBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "larger than the connection's limit"},
	} {
		t.Run(test.path, func(t *testing.T) {
			result, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/" + test.path})
			requireBranch(t, result, err, test.branch, test.kind)
			require.Contains(t, result.Failure.Message, test.text)
			require.Equal(t, "readFeed", result.Failure.Operation)
		})
	}
}

func TestReadFeedRetriesTransientFailures(t *testing.T) {
	site := newTestSite(t)
	site.handle("https://blog.example.com/429", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	site.handle("https://blog.example.com/503", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", testClockTime.Add(3*time.Second).Format(http.TimeFormat))
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	site.handle("https://blog.example.com/500", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	site.handle("https://blog.example.com/dropped", func(w http.ResponseWriter, _ *http.Request) {
		connection, _, err := w.(http.Hijacker).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	})
	site.handle("https://blog.example.com/truncated", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "500")
		_, _ = w.Write([]byte("<rss>"))
	})
	client := site.newClient(t, Config{})
	for _, test := range []struct {
		path  string
		after time.Duration
	}{{"429", 7 * time.Second}, {"503", 3 * time.Second}, {"500", 0}, {"dropped", 0}, {"truncated", 0}} {
		t.Run(test.path, func(t *testing.T) {
			_, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/" + test.path})
			require.Error(t, err)
			var retryAfter *dex.RetryAfterError
			if test.after > 0 {
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, test.after, retryAfter.After)
			} else {
				require.False(t, errors.As(err, &retryAfter), "no provider delay leaves the Step retry policy in charge")
			}
			require.NotContains(t, err.Error(), "blog.example.com")
		})
	}
}

func TestReadFeedRefusesUnsafeOrInvalidTargetsBeforeSending(t *testing.T) {
	site := newTestSite(t)
	client := site.newClient(t, Config{})
	for _, target := range []string{
		"https://internal.example.com/feed", "https://loopback.example.com/feed", "https://metadata.example.com/latest",
		"https://shared.example.com/feed", "https://ipv6meta.example.com/feed", "https://uniquelocal.example.com/feed",
		"https://127.0.0.1/feed", "https://[::1]/feed", "http://169.254.169.254/latest/meta-data",
	} {
		t.Run("address "+target, func(t *testing.T) {
			result, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: target})
			requireBranch(t, result, err, ReadFeedBranchUnreachable, sdkgo.FailureValidation)
			require.NotContains(t, result.Failure.Message, "example.com")
		})
	}
	for _, test := range []struct{ target, text string }{
		{"", "url is required"},
		{"feed.xml", "absolute http or https URL"},
		{"ftp://blog.example.com/feed", "absolute http or https URL"},
		{"https:///feed", "absolute http or https URL"},
		{"mailto:team@example.com", "absolute http or https URL"},
		{"https://user:secret@blog.example.com/feed", "user information"},
		{"http://blog.example.com:8080/feed", "only port 80"},
		{"https://blog.example.com/a feed", "spaces or control characters"},
		{"https://blog.example.com/" + strings.Repeat("a", maxURLBytes), "at most 2048 bytes"},
	} {
		t.Run("input "+test.text, func(t *testing.T) {
			result, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: test.target})
			requireBranch(t, result, err, ReadFeedBranchDefect, sdkgo.FailureValidation)
			require.Contains(t, result.Failure.Message, test.text)
			require.NotContains(t, result.Failure.Message, "secret")
		})
	}
	for _, input := range []ReadFeedInput{
		{URL: "https://blog.example.com/feed", Limit: 201},
		{URL: "https://blog.example.com/feed", Limit: -1},
		{URL: "https://blog.example.com/feed", MaxSummaryCharacters: 4001},
		{URL: "https://blog.example.com/feed", ETag: "\"café\""},
		{URL: "https://blog.example.com/feed", LastModified: "Tue,\n10 Nov"},
	} {
		result, err := runQuery(t, client.ReadFeed(), input)
		requireBranch(t, result, err, ReadFeedBranchDefect, sdkgo.FailureValidation)
	}
	require.Empty(t, site.recordedRequests(), "no refused or invalid target reached the site")
	require.NotContains(t, site.dialedAddresses(), "10.0.0.5:443")
}

func TestReadFeedSeparatesPermanentTransportFailures(t *testing.T) {
	site := newTestSite(t)
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://missing.example.com/feed"})
	requireBranch(t, result, err, ReadFeedBranchUnreachable, sdkgo.FailureTransport)
	require.Equal(t, "the host does not exist", result.Failure.Message)
	result, err = runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.net/feed"})
	requireBranch(t, result, err, ReadFeedBranchUnreachable, sdkgo.FailureTransport)
	require.Contains(t, result.Failure.Message, "certificate")
	require.Empty(t, site.recordedRequests())
}

func TestReadFeedDecompressesGzipWithinTheLimit(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://blog.example.com/feed.xml.gz", "application/gzip", string(gzipBytes(t, `<rss><channel><title>Compressed</title></channel></rss>`)))
	site.handle("https://blog.example.com/encoded.xml", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Header().Set("Content-Encoding", "gzip")
		_, _ = w.Write(gzipBytes(t, `<rss><channel><title>Encoded</title></channel></rss>`))
	})
	site.serve("https://blog.example.com/bomb.xml.gz", "application/gzip", string(gzipBytes(t, "<rss>"+strings.Repeat(" ", 1<<20)+"</rss>")))
	client := site.newClient(t, Config{MaxDocumentBytes: 64 << 10})
	result, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/feed.xml.gz"})
	requireBranch(t, result, err, ReadFeedBranchRead, "")
	require.Equal(t, "Compressed", result.Value.Title)
	result, err = runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/encoded.xml"})
	requireBranch(t, result, err, ReadFeedBranchRead, "")
	require.Equal(t, "Encoded", result.Value.Title)
	result, err = runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/bomb.xml.gz"})
	requireBranch(t, result, err, ReadFeedBranchInvalidResponse, sdkgo.FailureResponseTooLarge)
}

func TestReadFeedBoundsDeeplyNestedDocuments(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://blog.example.com/deep.xml", "text/xml", "<rss>"+strings.Repeat("<a>", 1000)+strings.Repeat("</a>", 1000)+"</rss>")
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/deep.xml"})
	requireBranch(t, result, err, ReadFeedBranchInvalidResponse, sdkgo.FailureProtocol)
	require.Contains(t, result.Failure.Message, "nested too deeply")
}

func gzipBytes(t *testing.T, contents string) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err := writer.Write([]byte(contents))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	return compressed.Bytes()
}

func TestReadFeedReadsRSSWithADefaultNamespace(t *testing.T) {
	site := newTestSite(t)
	site.serve("https://blog.example.com/rss2.xml", "application/rss+xml", `<?xml version="1.0"?>
<rss version="2.0" xmlns="http://backend.userland.com/rss2">
  <channel><title>Namespaced</title><link>https://blog.example.com/</link>
    <item><title>Inside</title><link>https://blog.example.com/inside</link><guid>inside-1</guid></item>
  </channel>
</rss>`)
	client := site.newClient(t, Config{})
	result, err := runQuery(t, client.ReadFeed(), ReadFeedInput{URL: "https://blog.example.com/rss2.xml"})
	requireBranch(t, result, err, ReadFeedBranchRead, "")
	require.Equal(t, "Namespaced", result.Value.Title)
	require.Equal(t, []FeedItem{{ID: "inside-1", Title: "Inside", Link: "https://blog.example.com/inside"}}, result.Value.Items)
}
