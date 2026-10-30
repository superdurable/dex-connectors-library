// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package teams

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractMessageTextKeepsOnlyVisibleText(t *testing.T) {
	for name, test := range map[string]struct {
		contentType ContentType
		content     string
		text        string
	}{
		"plain text is literal":   {ContentTypeText, "  <b>not markup</b>\r\nsecond line ", "<b>not markup</b>\nsecond line"},
		"paragraphs become lines": {ContentTypeHTML, "<p>First</p><p>Second &amp; third</p>", "First\nSecond & third"},
		"mentions keep names":     {ContentTypeHTML, `<p><at id="0">Ada</at>&nbsp;please ack</p>`, "Ada please ack"},
		"emoji keep characters":   {ContentTypeHTML, `<p><emoji id="like" alt="👍" title="Like"></emoji></p>`, "👍"},
		"images keep alt text":    {ContentTypeHTML, `<img alt="graph" src="https://example.com/x.png">`, "graph"},
		"scripts are dropped":     {ContentTypeHTML, `<p>ok</p><script>alert(1)</script><style>p{}</style>`, "ok"},
		"blank lines collapse":    {ContentTypeHTML, "<div>a</div><br><br><br><div>b</div>", "a\n\nb"},
		"list items become lines": {ContentTypeHTML, "<ul><li>one</li><li>two</li></ul>", "one\ntwo"},
		"unclosed tags are safe":  {ContentTypeHTML, "<p>unclosed <b>bold", "unclosed bold"},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.text, extractMessageText(test.contentType, test.content))
		})
	}
}

func TestComparisonTextIgnoresTeamsMarkupChanges(t *testing.T) {
	sent, err := buildMessageContent("<p>p95 is <b>4.2 s</b>.</p><p>Owner: payments</p>", ContentTypeHTML, "", "", 1<<20)
	require.NoError(t, err)
	stored := comparisonTextOf(extractMessageText(ContentTypeHTML, "<div><p>p95 is <strong>4.2&nbsp;s</strong>.</p>\n<p>Owner:  payments</p></div>"))
	require.Equal(t, sent.comparisonText, stored)

	plain, err := buildMessageContent("Status: investigating.\nReply ack.", ContentTypeText, "", "", 1<<20)
	require.NoError(t, err)
	require.Equal(t, plain.comparisonText, comparisonTextOf(extractMessageText(ContentTypeHTML, "<p>Status: investigating.<br>Reply ack.</p>")),
		"Teams may store a text post as HTML")
}

func TestBuildMessageContentCountsContentAndSubjectBytes(t *testing.T) {
	_, err := buildMessageContent("ééé", ContentTypeText, "", "ab", 8)
	require.NoError(t, err, "six content bytes and two subject bytes fit eight")
	_, err = buildMessageContent("ééé", ContentTypeText, "", "abc", 8)
	require.ErrorContains(t, err, "maxMessageBytes")
	_, err = buildMessageContent("ok", ContentTypeText, "", "", 8)
	require.NoError(t, err)
	_, err = buildMessageContent("bad \xff", ContentTypeText, "", "", 8)
	require.ErrorContains(t, err, "UTF-8")
}
