// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/docs"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const expectedPolicyMarkdown = `# Refund Policy

## Finance operations

Effective {{effectiveDate}}

\# not a heading

1\. not a list

## Approvals

1. First step
    1. Sub step
2. Second step

Between lists

- Owner: Ana Diaz
- See [Policy FAQ](https://docs.google.com/document/d/faq)

Line one
Line two

---

| Tier | Limit |
| --- | --- |
| Gold \| VIP | USD 500[^1] |

Reviewed Oct 1, 2026`

const expectedPolicyPlainText = "Refund Policy\nFinance operations\nEffective {{effectiveDate}}\n# not a heading\n1. not a list\nApprovals\n" +
	"1. First step\n  1. Sub step\n2. Second step\nBetween lists\n- Owner: Ana Diaz\n- See Policy FAQ\nLine one\nLine two\n\n" +
	"Tier\tLimit\nGold | VIP\tUSD 500[1]\nReviewed Oct 1, 2026"

func textRun(content string) map[string]any {
	return map[string]any{"textRun": map[string]any{"content": content}}
}

func paragraph(style string, bullet map[string]any, elements ...map[string]any) map[string]any {
	value := map[string]any{"elements": elements, "paragraphStyle": map[string]any{"namedStyleType": style}}
	if bullet != nil {
		value["bullet"] = bullet
	}
	return map[string]any{"paragraph": value}
}

func listBullet(listID string, nestingLevel int) map[string]any {
	return map[string]any{"listId": listID, "nestingLevel": nestingLevel}
}

func cell(text string, elements ...map[string]any) map[string]any {
	return map[string]any{"content": []any{paragraph("NORMAL_TEXT", nil, append([]map[string]any{textRun(text)}, elements...)...)}}
}

// policyDocumentJSON exercises every element the renderer maps.
func policyDocumentJSON(t *testing.T) string {
	t.Helper()
	body := []any{
		map[string]any{"endIndex": 1, "sectionBreak": map[string]any{}},
		paragraph("TITLE", nil, textRun("Refund Policy\n")),
		paragraph("SUBTITLE", nil, textRun("Finance operations\n")),
		map[string]any{"tableOfContents": map[string]any{"content": []any{paragraph("NORMAL_TEXT", nil, textRun("Refund Policy\n"))}}},
		paragraph("NORMAL_TEXT", nil, textRun("Effective "), textRun("{{effectiveDate}}\n")),
		paragraph("NORMAL_TEXT", nil, textRun("# not a heading\n")),
		paragraph("NORMAL_TEXT", nil, textRun("1. not a list\n")),
		paragraph("HEADING_2", nil, textRun("Approvals\n")),
		paragraph("NORMAL_TEXT", listBullet("ordered", 0), textRun("First step\n")),
		paragraph("NORMAL_TEXT", listBullet("ordered", 1), textRun("Sub step\n")),
		paragraph("NORMAL_TEXT", listBullet("ordered", 0), textRun("Second step\n")),
		paragraph("NORMAL_TEXT", nil, textRun("Between lists\n")),
		paragraph("NORMAL_TEXT", listBullet("unordered", 0), textRun("Owner: "),
			map[string]any{"person": map[string]any{"personProperties": map[string]any{"name": "Ana Diaz", "email": "ana@example.com"}}}, textRun("\n")),
		paragraph("NORMAL_TEXT", listBullet("unordered", 0), textRun("See "),
			map[string]any{"richLink": map[string]any{"richLinkProperties": map[string]any{"title": "Policy FAQ", "uri": "https://docs.google.com/document/d/faq"}}}, textRun("\n")),
		paragraph("NORMAL_TEXT", nil, textRun("Line one\vLine two\n")),
		paragraph("NORMAL_TEXT", nil, map[string]any{"horizontalRule": map[string]any{}}, textRun("\n")),
		map[string]any{"table": map[string]any{"tableRows": []any{
			map[string]any{"tableCells": []any{cell("Tier\n"), cell("Limit\n")}},
			map[string]any{"tableCells": []any{cell("Gold | VIP\n"), cell("USD 500", map[string]any{"footnoteReference": map[string]any{"footnoteNumber": "1"}}, textRun("\n"))}},
		}}},
		paragraph("NORMAL_TEXT", nil, textRun("Reviewed "),
			map[string]any{"dateElement": map[string]any{"dateElementProperties": map[string]any{"displayText": "Oct 1, 2026"}}}, textRun("\n")),
		map[string]any{"endIndex": 400, "paragraph": map[string]any{"elements": []any{textRun("\n")}}},
	}
	lists := map[string]any{
		"ordered":   map[string]any{"listProperties": map[string]any{"nestingLevels": []any{map[string]any{"glyphType": "DECIMAL"}, map[string]any{"glyphType": "ALPHA"}}}},
		"unordered": map[string]any{"listProperties": map[string]any{"nestingLevels": []any{map[string]any{"glyphSymbol": "-"}}}},
	}
	encoded, err := json.Marshal(map[string]any{
		"documentId": "policyDoc1", "title": "Refund Policy Template", "revisionId": "rev-policy-7",
		"tabs": []any{
			map[string]any{"tabProperties": map[string]any{"tabId": "t.0"}, "documentTab": map[string]any{"body": map[string]any{"content": body}, "lists": lists}},
			map[string]any{"tabProperties": map[string]any{"tabId": "t.1"}, "documentTab": map[string]any{"body": map[string]any{"content": []any{}}}},
		},
	})
	require.NoError(t, err)
	return string(encoded)
}

func TestGetDocumentTextRendersMarkdownWithoutPendingSuggestions(t *testing.T) {
	fixture := policyDocumentJSON(t)
	server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, fixture)
	})
	result, err := sdkgo.RunQuery(newDexContext("read-markdown"), newDocsClient(t, server.URL).GetDocumentText(), docsConnection,
		docs.GetDocumentTextInput{DocumentID: "policyDoc1"})
	require.NoError(t, err)
	require.Equal(t, docs.GetDocumentTextBranchRead, result.Branch)
	require.Equal(t, expectedPolicyMarkdown, result.Value.Text)
	require.Equal(t, docs.TextFormatMarkdown, result.Value.Format)
	require.Equal(t, int64(len(expectedPolicyMarkdown)), result.Value.ByteCount)
	require.Equal(t, "Refund Policy Template", result.Value.Title)
	require.Equal(t, "rev-policy-7", result.Value.RevisionID)
	require.True(t, result.Value.HasOtherTabs)
	require.Equal(t, "policyDoc1", result.Receipt.ProviderObjectID)

	requests := server.recorded()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodGet, requests[0].method)
	require.Equal(t, "/v1/documents/policyDoc1", requests[0].path)
	require.Equal(t, []string{"true"}, requests[0].query["includeTabsContent"])
	require.Equal(t, []string{"PREVIEW_WITHOUT_SUGGESTIONS"}, requests[0].query["suggestionsViewMode"])
	require.Equal(t, []string{"documentId,title,revisionId,tabs(tabProperties/tabId,childTabs/tabProperties/tabId,documentTab(body,lists))"}, requests[0].query["fields"])
	require.Equal(t, "Bearer "+docsTestToken, requests[0].header.Get("Authorization"))
}

func TestGetDocumentTextRendersPlainText(t *testing.T) {
	fixture := policyDocumentJSON(t)
	server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, fixture)
	})
	result, err := sdkgo.RunQuery(newDexContext("read-plain"), newDocsClient(t, server.URL).GetDocumentText(), docsConnection,
		docs.GetDocumentTextInput{DocumentID: "policyDoc1", Format: docs.TextFormatPlainText})
	require.NoError(t, err)
	require.Equal(t, docs.GetDocumentTextBranchRead, result.Branch)
	require.Equal(t, expectedPolicyPlainText, result.Value.Text)
	require.Equal(t, docs.TextFormatPlainText, result.Value.Format)
}

func TestGetDocumentTextBoundsTheResponseAndTheRenderedText(t *testing.T) {
	fixture := policyDocumentJSON(t)
	server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, fixture)
	})
	input := docs.GetDocumentTextInput{DocumentID: "policyDoc1"}

	longText, err := sdkgo.RunQuery(newDexContext("read-long-text"), newDocsClient(t, server.URL, docs.Config{MaxTextBytes: 64}).GetDocumentText(), docsConnection, input)
	require.NoError(t, err)
	require.Equal(t, docs.GetDocumentTextBranchTooLarge, longText.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, longText.Failure.Kind)
	require.Equal(t, "Refund Policy Template", longText.Value.Title)
	require.Equal(t, "rev-policy-7", longText.Value.RevisionID)
	require.Empty(t, longText.Value.Text)

	largeDocument, err := sdkgo.RunQuery(newDexContext("read-large-document"), newDocsClient(t, server.URL, docs.Config{MaxResponseBytes: 128}).GetDocumentText(), docsConnection, input)
	require.NoError(t, err)
	require.Equal(t, docs.GetDocumentTextBranchTooLarge, largeDocument.Branch)
	require.Empty(t, largeDocument.Value.DocumentID)
}

func TestGetDocumentTextClassifiesGoogleFailuresWithoutProviderText(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body       string
		retryAfter string
		wantBranch sdkgo.BranchID
		wantKind   sdkgo.FailureKind
		wantDelay  time.Duration
	}{
		{name: "missing document", status: http.StatusNotFound, body: `{"error":{"code":404,"message":"SENTINEL","status":"NOT_FOUND"}}`, wantBranch: docs.GetDocumentTextBranchNotFound, wantKind: sdkgo.FailureNotFound},
		{name: "no permission", status: http.StatusForbidden, body: `{"error":{"code":403,"message":"SENTINEL","status":"PERMISSION_DENIED"}}`, wantBranch: docs.GetDocumentTextBranchProviderRejected, wantKind: sdkgo.FailureAuthorization},
		{name: "rate limit", status: http.StatusTooManyRequests, body: `{"error":{"code":429,"status":"RESOURCE_EXHAUSTED"}}`, retryAfter: "7", wantKind: sdkgo.FailureRateLimit, wantDelay: 7 * time.Second},
		{name: "Drive-style 403 rate limit", status: http.StatusForbidden, body: `{"error":{"errors":[{"reason":"userRateLimitExceeded"}]}}`, wantKind: sdkgo.FailureRateLimit},
		{name: "outage", status: http.StatusServiceUnavailable, body: `{}`, wantKind: sdkgo.FailureAvailability},
		{name: "invalid JSON", status: http.StatusOK, body: `{"documentId":`, wantBranch: docs.GetDocumentTextBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "no tabs", status: http.StatusOK, body: `{"documentId":"policyDoc1","tabs":[]}`, wantBranch: docs.GetDocumentTextBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
				if test.retryAfter != "" {
					response.Header().Set("Retry-After", test.retryAfter)
				}
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunQuery(newDexContext("read-failure"), newDocsClient(t, server.URL).GetDocumentText(), docsConnection,
				docs.GetDocumentTextInput{DocumentID: "policyDoc1"})
			if test.wantBranch == "" {
				requireRetry(t, err, test.wantKind)
				var retryAfter *dex.RetryAfterError
				if test.wantDelay > 0 {
					require.ErrorAs(t, err, &retryAfter)
					require.Equal(t, test.wantDelay, retryAfter.After)
				}
				require.NotContains(t, err.Error(), "SENTINEL")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, test.wantKind, result.Failure.Kind)
			requireSecretFree(t, result)
		})
	}
}

func TestGetDocumentTextRejectsInvalidInputWithoutARequest(t *testing.T) {
	server := newRecordingServer(t, func(http.ResponseWriter, recordedRequest) { t.Fatal("no request is expected") })
	for name, input := range map[string]docs.GetDocumentTextInput{
		"blank document ID":    {},
		"document URL":         {DocumentID: "https://docs.google.com/document/d/policyDoc1/edit"},
		"unknown format":       {DocumentID: "policyDoc1", Format: "html"},
		"path traversal in ID": {DocumentID: "../files"},
	} {
		result, err := sdkgo.RunQuery(newDexContext("read-invalid"), newDocsClient(t, server.URL).GetDocumentText(), docsConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, docs.GetDocumentTextBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}
}

func TestGetDocumentTextReportsAViewerWithoutARevision(t *testing.T) {
	fixture := strings.Replace(policyDocumentJSON(t), `"revisionId":"rev-policy-7",`, "", 1)
	server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, fixture)
	})
	result, err := sdkgo.RunQuery(newDexContext("read-viewer"), newDocsClient(t, server.URL).GetDocumentText(), docsConnection,
		docs.GetDocumentTextInput{DocumentID: "policyDoc1"})
	require.NoError(t, err)
	require.Equal(t, docs.GetDocumentTextBranchRead, result.Branch)
	require.Empty(t, result.Value.RevisionID)
}
