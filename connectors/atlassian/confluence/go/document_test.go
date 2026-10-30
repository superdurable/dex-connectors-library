// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package confluence

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func markdownToStorage(t *testing.T, markdown string) string {
	t.Helper()
	storage, _, err := convertWrittenBody(markdown, TextFormatMarkdown, "body")
	require.NoError(t, err)
	return storage
}

func storageToText(t *testing.T, storage string, format TextFormat) string {
	t.Helper()
	blocks, err := parseStorageDocument(storage)
	require.NoError(t, err)
	text, isTruncated := renderDocumentText(blocks, format, 0)
	require.False(t, isTruncated)
	return text
}

func TestMarkdownBecomesEscapedStorageFormat(t *testing.T) {
	storage := markdownToStorage(t, strings.Join([]string{
		"# Remote work",
		"",
		"Staff may work **remotely** two *days* a week, see [the handbook](https://example.com/handbook?a=1&b=2).",
		"Use `vpn connect` and ~~fax~~ email.",
		"",
		"- Ask your manager",
		"  - in writing",
		"- Log the days",
		"",
		"3. third",
		"4. fourth",
		"",
		"- [x] signed",
		"- [ ] filed",
		"",
		"> Approved by HR",
		"",
		"```bash",
		"echo '<b>' && exit",
		"```",
		"",
		"| Role | Days |",
		"| --- | ---: |",
		"| Staff | 2 |",
		"",
		"---",
		"",
		"Plain <script>alert(1)</script> & snake_case_name, [bad](javascript:alert(1)).",
	}, "\n"))
	require.Equal(t, `<h1>Remote work</h1>`+
		`<p>Staff may work <strong>remotely</strong> two <em>days</em> a week, see <a href="https://example.com/handbook?a=1&amp;b=2">the handbook</a>.<br />`+
		`Use <code>vpn connect</code> and <span style="text-decoration: line-through;">fax</span> email.</p>`+
		`<ul><li>Ask your manager<ul><li>in writing</li></ul></li><li>Log the days</li></ul>`+
		`<ol start="3"><li>third</li><li>fourth</li></ol>`+
		`<ac:task-list><ac:task><ac:task-status>complete</ac:task-status><ac:task-body>signed</ac:task-body></ac:task>`+
		`<ac:task><ac:task-status>incomplete</ac:task-status><ac:task-body>filed</ac:task-body></ac:task></ac:task-list>`+
		`<blockquote><p>Approved by HR</p></blockquote>`+
		`<pre>echo '&lt;b&gt;' &amp;&amp; exit</pre>`+
		`<table><tbody><tr><th>Role</th><th>Days</th></tr><tr><td>Staff</td><td>2</td></tr></tbody></table>`+
		`<hr />`+
		`<p>Plain &lt;script&gt;alert(1)&lt;/script&gt; &amp; snake_case_name, bad.</p>`, storage)
}

func TestPlainTextIsNeverReadAsMarkup(t *testing.T) {
	storage, _, err := convertWrittenBody("\r\n# Not a heading\r\n**not bold** <b>\n  \n\n\nNew paragraph\n", TextFormatPlainText, "body")
	require.NoError(t, err)
	require.Equal(t, `<p># Not a heading<br />**not bold** &lt;b&gt;</p><p>New paragraph</p>`, storage)
}

func TestWrittenTextIsValidated(t *testing.T) {
	for _, test := range []struct {
		body    string
		format  TextFormat
		message string
	}{
		{"  ", TextFormatMarkdown, "body is required"},
		{"bell\a", TextFormatMarkdown, "body cannot contain control characters"},
		{string([]byte{0xff}), TextFormatPlainText, "body must be valid UTF-8"},
		{strings.Repeat("a", maximumWrittenTextCharacters+1), TextFormatPlainText, "body cannot exceed 262144 characters"},
		{"text", TextFormat("html"), "bodyFormat must be markdown or plainText"},
	} {
		_, _, err := convertWrittenBody(test.body, test.format, "body")
		require.EqualError(t, err, test.message)
	}
}

func TestStorageFormatReadsAsMarkdown(t *testing.T) {
	storage := `<h2>Scope&nbsp;and   intent</h2>` +
		`<p>Applies to <strong>all</strong> staff.<br/>See <a href="https://example.com/x">policy</a> and ` +
		`<ac:link><ri:page ri:content-title="Leave policy" /><ac:plain-text-link-body><![CDATA[leave]]></ac:plain-text-link-body></ac:link>.</p>` +
		`<ac:structured-macro ac:name="info" ac:schema-version="1"><ac:rich-text-body><p>Reviewed yearly.</p></ac:rich-text-body></ac:structured-macro>` +
		`<ac:structured-macro ac:name="code"><ac:parameter ac:name="language">go</ac:parameter><ac:plain-text-body><![CDATA[if a < b {` + "\n" + `}]]></ac:plain-text-body></ac:structured-macro>` +
		`<ul><li><p>First</p><ol><li>nested</li></ol></li><li>Second <ac:structured-macro ac:name="status"><ac:parameter ac:name="title">DONE</ac:parameter></ac:structured-macro></li></ul>` +
		`<ac:task-list><ac:task><ac:task-id>1</ac:task-id><ac:task-status>complete</ac:task-status><ac:task-body>Sign</ac:task-body></ac:task></ac:task-list>` +
		`<table><tbody><tr><th>Role</th><th>Days</th></tr><tr><td><p>Staff | team</p></td><td>2</td></tr></tbody></table>` +
		`<ac:structured-macro ac:name="toc"/><p><ac:image><ri:attachment ri:filename="x.png"/></ac:image>1. not a list</p>`
	require.Equal(t, strings.Join([]string{
		"## Scope and intent",
		"",
		"Applies to **all** staff.",
		"See [policy](https://example.com/x) and leave.",
		"",
		"> Reviewed yearly.",
		"",
		"```go",
		"if a < b {",
		"}",
		"```",
		"",
		"- First",
		"",
		"  1. nested",
		"- Second DONE",
		"",
		"- [x] Sign",
		"",
		"| Role | Days |",
		"| --- | --- |",
		`| Staff \| team | 2 |`,
		"",
		`1\. not a list`,
	}, "\n"), storageToText(t, storage, TextFormatMarkdown))
}

func TestStorageFormatReadsAsPlainText(t *testing.T) {
	storage := `<h1>Title</h1><p>One <em>two</em><br />three</p><ul><li>item</li></ul><pre>code  kept</pre>`
	require.Equal(t, "Title\n\nOne two\nthree\n\n- item\n\ncode  kept", storageToText(t, storage, TextFormatPlainText))
}

func TestMarkdownRoundTripsThroughStorage(t *testing.T) {
	markdown := strings.Join([]string{
		"# Remote work",
		"",
		"Staff may work **remotely** two *days* a week.",
		"Second line with `code` and [a link](https://example.com).",
		"",
		"- Ask your manager",
		"- Log the days",
		"",
		"1. first",
		"2. second",
		"",
		"> Approved",
		"",
		"```",
		"x := 1",
		"```",
		"",
		"---",
	}, "\n")
	require.Equal(t, markdown, storageToText(t, markdownToStorage(t, markdown), TextFormatMarkdown))
	// A fence language is not kept: written code becomes a <pre> block, which has no language.
	require.Equal(t, "<pre>x := 1</pre>", markdownToStorage(t, "```go\nx := 1\n```"))
}

func TestMarkdownOutputEscapesTextThatWouldReadAsMarkup(t *testing.T) {
	storage := `<p>a*b* [x] snake_case _lead # not</p><p># hash</p><p>- dash</p>`
	text := storageToText(t, storage, TextFormatMarkdown)
	require.Equal(t, "a\\*b\\* \\[x\\] snake_case \\_lead # not\n\n\\# hash\n\n\\- dash", text)
	reparsed := markdownToStorage(t, text)
	require.Equal(t, storage, reparsed)
}

func TestAtlasDocFormatReadsAsMarkdown(t *testing.T) {
	blocks, err := parseADFDocument(`{"type":"doc","version":1,"content":[
		{"type":"heading","attrs":{"level":3},"content":[{"type":"text","text":"Impact"}]},
		{"type":"paragraph","content":[
			{"type":"text","text":"Bold","marks":[{"type":"strong"}]},{"type":"text","text":" and "},
			{"type":"text","text":"site","marks":[{"type":"link","attrs":{"href":"https://example.com"}}]},
			{"type":"hardBreak"},{"type":"mention","attrs":{"text":"@Ada"}},{"type":"text","text":" "},
			{"type":"date","attrs":{"timestamp":"1790726400000"}}]},
		{"type":"orderedList","attrs":{"order":2},"content":[
			{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"two"}]}]}]},
		{"type":"taskList","content":[{"type":"taskItem","attrs":{"state":"DONE"},"content":[{"type":"text","text":"done"}]}]},
		{"type":"panel","attrs":{"panelType":"info"},"content":[{"type":"paragraph","content":[{"type":"text","text":"Note"}]}]},
		{"type":"codeBlock","attrs":{"language":"sql"},"content":[{"type":"text","text":"select 1"}]},
		{"type":"table","content":[
			{"type":"tableRow","content":[{"type":"tableHeader","content":[{"type":"paragraph","content":[{"type":"text","text":"A"}]}]}]},
			{"type":"tableRow","content":[{"type":"tableCell","content":[{"type":"paragraph","content":[{"type":"text","text":"1"}]}]}]}]},
		{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"x"}}]},
		{"type":"rule"}]}`)
	require.NoError(t, err)
	text, isTruncated := renderDocumentText(blocks, TextFormatMarkdown, 0)
	require.False(t, isTruncated)
	require.Equal(t, strings.Join([]string{
		"### Impact",
		"",
		"**Bold** and [site](https://example.com)",
		"@Ada 2026-09-30",
		"",
		"2. two",
		"",
		"- [x] done",
		"",
		"> Note",
		"",
		"```sql",
		"select 1",
		"```",
		"",
		"| A |",
		"| --- |",
		"| 1 |",
		"",
		"---",
	}, "\n"), text)
	_, err = parseADFDocument(`{"type":"paragraph"}`)
	require.Error(t, err)
}

func TestRenderedTextIsCutAtTheCharacterLimit(t *testing.T) {
	blocks := parsePlainText("héllo wörld")
	text, isTruncated := renderDocumentText(blocks, TextFormatPlainText, 4)
	require.True(t, isTruncated)
	require.Equal(t, "héll", text)
}

func TestComparisonTextIgnoresMarkupThatConfluenceAdds(t *testing.T) {
	ours := markdownToStorage(t, "# Title\n\nOne **two**\n\n- item")
	normalized := `<h1 local-id="a1">Title</h1>` + "\n" + `<p local-id="b2">One  <strong>two</strong></p><ul><li><p>item</p></li></ul>`
	require.Equal(t, storageComparisonText(ours), storageComparisonText(normalized))
	require.NotEqual(t, storageComparisonText(ours), storageComparisonText(`<h1>Title</h1><p>One three</p>`))
}

func TestHostileStorageIsBoundedAndNeverPanics(t *testing.T) {
	deep := strings.Repeat("<div>", 5000) + "deep text" + strings.Repeat("</div>", 5000)
	require.NotPanics(t, func() {
		blocks, err := parseStorageDocument(deep)
		require.NoError(t, err)
		_, _ = renderDocumentText(blocks, TextFormatMarkdown, 0)
	})
	_, err := parseStorageDocument("<p>unterminated <![CDATA[ x")
	require.Error(t, err)
	unclosed, err := parseStorageDocument("<p>open <strong>bold")
	require.NoError(t, err)
	text, _ := renderDocumentText(unclosed, TextFormatMarkdown, 0)
	require.Equal(t, "open **bold**", text)
}
