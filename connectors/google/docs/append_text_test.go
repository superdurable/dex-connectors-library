// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/docs"
	"github.com/superdurable/dex-connectors-library/connectors/google/docs/internal/fakegoogledocs"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const publicationStamp = "Published by Dex on 2026-10-01."

func TestAppendTextAddsAPlainParagraphAfterTheLastOne(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	draft := addPolicyDraft(provider)

	result, err := sdkgo.RunMutation(newDexContext("append"), newDocsClient(t, provider.URL).AppendText(), docsConnection, docs.AppendTextInput{
		DocumentID: draftDocumentID, RequiredRevisionID: draft.RevisionID(), Text: publicationStamp,
	})
	require.NoError(t, err)
	require.Equal(t, docs.AppendTextBranchAppended, result.Branch)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, "rev-draftDoc1-2", result.Value.RevisionID)
	appended, _ := provider.Document(draftDocumentID)
	require.Equal(t, append(append([]fakegoogledocs.Paragraph(nil), draft.Paragraphs...), fakegoogledocs.Paragraph{Text: publicationStamp}), appended.Paragraphs,
		"the stamp does not join the bullet list it follows")
}

func TestAppendTextRequestsInsertBeforeTheFinalNewline(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"documentId":"draftDoc1","revisionId":"rev-7","tabs":[{"tabProperties":{"tabId":"t.0"},
				"documentTab":{"body":{"content":[{"endIndex":1,"sectionBreak":{}},{"startIndex":1,"endIndex":5,"paragraph":{"elements":[{"textRun":{"content":"Old\n"}}]}}]}}}]}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"documentId":"draftDoc1","replies":[{},{},{},{}],"writeControl":{"requiredRevisionId":"rev-8"}}`)
	})
	_, err := sdkgo.RunMutation(newDexContext("append-shape"), newDocsClient(t, server.URL).AppendText(), docsConnection, docs.AppendTextInput{
		DocumentID: draftDocumentID, RequiredRevisionID: "rev-7", Text: "New",
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"requests":[
		{"insertText":{"text":"\nNew","location":{"index":4,"tabId":"t.0"}}},
		{"deleteParagraphBullets":{"range":{"startIndex":5,"endIndex":8,"tabId":"t.0"}}},
		{"updateParagraphStyle":{"range":{"startIndex":5,"endIndex":8,"tabId":"t.0"},"paragraphStyle":{"namedStyleType":"NORMAL_TEXT"},"fields":"*"}},
		{"updateTextStyle":{"range":{"startIndex":5,"endIndex":8,"tabId":"t.0"},"textStyle":{},"fields":"*"}}
	],"writeControl":{"requiredRevisionId":"rev-7"}}`, string(server.recorded()[1].body))
}

func TestAppendTextFillsAnEmptyDocumentWithoutALeadingBlankParagraph(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	provider.AddDocument(fakegoogledocs.Document{ID: "emptyDoc", Title: "Blank", Paragraphs: []fakegoogledocs.Paragraph{{}}})
	empty, _ := provider.Document("emptyDoc")

	result, err := sdkgo.RunMutation(newDexContext("append-empty"), newDocsClient(t, provider.URL).AppendText(), docsConnection, docs.AppendTextInput{
		DocumentID: "emptyDoc", RequiredRevisionID: empty.RevisionID(), Text: "First line\nSecond line",
	})
	require.NoError(t, err)
	require.Equal(t, docs.AppendTextBranchAppended, result.Branch)
	filled, _ := provider.Document("emptyDoc")
	require.Equal(t, []fakegoogledocs.Paragraph{{Text: "First line"}, {Text: "Second line"}}, filled.Paragraphs)
}

func TestAppendTextRetryAfterALostResponseAppendsOnce(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	draft := addPolicyDraft(provider)
	provider.SetWriteBehavior(fakegoogledocs.WriteKindAppend, fakegoogledocs.WriteBehavior{StatusAfterApplying: http.StatusInternalServerError})
	client := newDocsClient(t, provider.URL)
	input := docs.AppendTextInput{DocumentID: draftDocumentID, RequiredRevisionID: draft.RevisionID(), Text: publicationStamp}

	_, err := sdkgo.RunMutation(newDexContext("append-lost"), client.AppendText(), docsConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	retried, err := sdkgo.RunMutation(newDexContext("append-lost"), client.AppendText(), docsConnection, input)
	require.NoError(t, err)
	require.Equal(t, docs.AppendTextBranchAppended, retried.Branch)
	require.True(t, retried.Value.WasAlreadyApplied)
	appended, _ := provider.Document(draftDocumentID)
	require.Len(t, appended.Paragraphs, len(draft.Paragraphs)+1, "the stamp appears once")
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountBatchUpdatesApplied))
}

func TestAppendTextReportsAnotherWritersChange(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	draft := addPolicyDraft(provider)
	provider.EditDocument(draftDocumentID, append(append([]fakegoogledocs.Paragraph(nil), draft.Paragraphs...), fakegoogledocs.Paragraph{Text: "Reviewer note"}))

	result, err := sdkgo.RunMutation(newDexContext("append-changed"), newDocsClient(t, provider.URL).AppendText(), docsConnection, docs.AppendTextInput{
		DocumentID: draftDocumentID, RequiredRevisionID: draft.RevisionID(), Text: publicationStamp,
	})
	require.NoError(t, err)
	require.Equal(t, docs.AppendTextBranchRevisionChanged, result.Branch)
	require.Equal(t, "rev-draftDoc1-2", result.Value.RevisionID)
	require.Zero(t, provider.Count(fakegoogledocs.CountBatchUpdatesReceived))
}

func TestAppendTextRejectsInvalidInputWithoutARequest(t *testing.T) {
	server := newRecordingServer(t, func(http.ResponseWriter, recordedRequest) { t.Fatal("no request is expected") })
	for name, input := range map[string]docs.AppendTextInput{
		"blank text":       {DocumentID: draftDocumentID, RequiredRevisionID: "rev-1", Text: " \n"},
		"missing revision": {DocumentID: draftDocumentID, Text: "a"},
		"invalid document": {DocumentID: "a/b", RequiredRevisionID: "rev-1", Text: "a"},
		"control text":     {DocumentID: draftDocumentID, RequiredRevisionID: "rev-1", Text: "a\x00b"},
	} {
		result, err := sdkgo.RunMutation(newDexContext("append-invalid"), newDocsClient(t, server.URL).AppendText(), docsConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, docs.AppendTextBranchDefect, result.Branch, name)
	}
}
