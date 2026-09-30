// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jira

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConvertPlainTextToDocumentKeepsParagraphsAndLineBreaks(t *testing.T) {
	document := convertPlainTextToDocument("\r\nFirst line\r\nsecond line\n  \n\n\nNew paragraph\n")
	encoded, err := json.Marshal(document)
	require.NoError(t, err)
	require.JSONEq(t, `{"type":"doc","version":1,"content":[
		{"type":"paragraph","content":[{"type":"text","text":"First line"},{"type":"hardBreak"},{"type":"text","text":"second line"}]},
		{"type":"paragraph","content":[{"type":"text","text":"New paragraph"}]}]}`, string(encoded))
}

func TestExtractDocumentTextReadsCommonNodes(t *testing.T) {
	text, isTruncated, err := extractDocumentText(json.RawMessage(`{"type":"doc","version":1,"content":[
		{"type":"heading","attrs":{"level":2},"content":[{"type":"text","text":"Impact"}]},
		{"type":"bulletList","content":[
			{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"text","text":"Panel B"}]}]},
			{"type":"listItem","content":[{"type":"paragraph","content":[{"type":"emoji","attrs":{"shortName":":fire:","text":"🔥"}}]}]}]},
		{"type":"paragraph","content":[{"type":"inlineCard","attrs":{"url":"https://status.example.com"}},{"type":"text","text":" is green"}]},
		{"type":"mediaSingle","content":[{"type":"media","attrs":{"id":"x"}}]},
		{"type":"rule"}]}`))
	require.NoError(t, err)
	require.False(t, isTruncated)
	require.Equal(t, "Impact\n\n- Panel B\n- 🔥\n\nhttps://status.example.com is green\n\n\n\n---", text)
}

func TestExtractDocumentTextRoundTripsConvertedText(t *testing.T) {
	original := "Line one\nLine two\n\nSecond paragraph"
	encoded, err := json.Marshal(convertPlainTextToDocument(original))
	require.NoError(t, err)
	text, _, err := extractDocumentText(encoded)
	require.NoError(t, err)
	require.Equal(t, original, text)
}

func TestExtractDocumentTextBoundsLengthAndAcceptsLegacyStrings(t *testing.T) {
	encoded, err := json.Marshal(convertPlainTextToDocument(strings.Repeat("x", maximumReadTextCharacters+10)))
	require.NoError(t, err)
	text, isTruncated, err := extractDocumentText(encoded)
	require.NoError(t, err)
	require.True(t, isTruncated)
	require.Len(t, text, maximumReadTextCharacters)

	text, isTruncated, err = extractDocumentText(json.RawMessage(`"plain wiki text"`))
	require.NoError(t, err)
	require.False(t, isTruncated)
	require.Equal(t, "plain wiki text", text)

	text, _, err = extractDocumentText(json.RawMessage(`null`))
	require.NoError(t, err)
	require.Empty(t, text)

	_, _, err = extractDocumentText(json.RawMessage(`[1]`))
	require.Error(t, err)
}
