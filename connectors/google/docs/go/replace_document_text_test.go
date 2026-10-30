// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs_test

import (
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/docs"
	"github.com/superdurable/dex-connectors-library/connectors/google/docs/internal/fakegoogledocs"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const draftDocumentID = "draftDoc1"

func addPolicyDraft(provider *fakegoogledocs.Server) fakegoogledocs.Document {
	draft := fakegoogledocs.Document{ID: draftDocumentID, Title: "Acme Refund Policy", Paragraphs: []fakegoogledocs.Paragraph{
		{Text: "Refund Policy", NamedStyleType: "HEADING_1"},
		{Text: "Effective {{effectiveDate}} for {{companyName}} customers."},
		{Text: "Refunds above {{approvalThreshold}} need a manager.", IsBullet: true},
	}}
	provider.AddDocument(draft)
	stored, _ := provider.Document(draftDocumentID)
	return stored
}

func fillPlaceholdersInput(requiredRevisionID string) docs.ReplaceDocumentTextInput {
	return docs.ReplaceDocumentTextInput{
		DocumentID: draftDocumentID, RequiredRevisionID: requiredRevisionID, Target: docs.ReplaceTargetPlaceholders,
		Placeholders: []docs.PlaceholderReplacement{
			{Placeholder: "{{effectiveDate}}", Text: "2026-10-01"},
			{Placeholder: "{{companyName}}", Text: "Acme"},
			{Placeholder: "{{approvalThreshold}}", Text: "USD 500"},
		},
	}
}

var filledPolicyParagraphs = []fakegoogledocs.Paragraph{
	{Text: "Refund Policy", NamedStyleType: "HEADING_1"},
	{Text: "Effective 2026-10-01 for Acme customers."},
	{Text: "Refunds above USD 500 need a manager.", IsBullet: true},
}

func TestReplaceDocumentTextReplacesPlaceholdersOnlyAtTheRequiredRevision(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	draft := addPolicyDraft(provider)

	result, err := sdkgo.RunMutation(newDexContext("fill"), newDocsClient(t, provider.URL).ReplaceDocumentText(), docsConnection, fillPlaceholdersInput(draft.RevisionID()))
	require.NoError(t, err)
	require.Equal(t, docs.ReplaceDocumentTextBranchReplaced, result.Branch)
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, "rev-draftDoc1-2", result.Value.RevisionID, "the revision after the write comes from Google's write control")
	require.Equal(t, []docs.PlaceholderReplacementResult{
		{Placeholder: "{{effectiveDate}}", OccurrencesChanged: 1}, {Placeholder: "{{companyName}}", OccurrencesChanged: 1},
		{Placeholder: "{{approvalThreshold}}", OccurrencesChanged: 1},
	}, result.Value.Replacements)
	filled, _ := provider.Document(draftDocumentID)
	require.Equal(t, filledPolicyParagraphs, filled.Paragraphs, "replacement keeps the heading and the bullet")
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountBatchUpdatesApplied))
}

func TestReplaceDocumentTextSendsOneGuardedBatchForTheFirstTab(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"documentId":"draftDoc1","revisionId":"rev-7","tabs":[{"tabProperties":{"tabId":"t.0"},
				"documentTab":{"body":{"content":[{"endIndex":1,"sectionBreak":{}},{"startIndex":1,"endIndex":19,"paragraph":{"elements":[{"textRun":{"content":"{{effectiveDate}}\n"}}]}}]}}}]}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"documentId":"draftDoc1","replies":[{"replaceAllText":{"occurrencesChanged":1}}],"writeControl":{"requiredRevisionId":"rev-8"}}`)
	})
	result, err := sdkgo.RunMutation(newDexContext("fill-shape"), newDocsClient(t, server.URL).ReplaceDocumentText(), docsConnection, docs.ReplaceDocumentTextInput{
		DocumentID: draftDocumentID, RequiredRevisionID: "rev-7", Target: docs.ReplaceTargetPlaceholders,
		Placeholders: []docs.PlaceholderReplacement{{Placeholder: "{{effectiveDate}}", Text: "2026-10-01"}},
	})
	require.NoError(t, err)
	require.Equal(t, "rev-8", result.Value.RevisionID)
	requests := server.recorded()
	require.Len(t, requests, 2)
	require.Equal(t, []string{"SUGGESTIONS_INLINE"}, requests[0].query["suggestionsViewMode"], "edit indexes are computed with suggestions inline")
	require.Equal(t, []string{"documentId,revisionId,tabs(tabProperties/tabId,documentTab/body)"}, requests[0].query["fields"])
	require.Equal(t, "/v1/documents/draftDoc1:batchUpdate", requests[1].path)
	require.JSONEq(t, `{"requests":[{"replaceAllText":{"replaceText":"2026-10-01","containsText":{"text":"{{effectiveDate}}","matchCase":true},
		"tabsCriteria":{"tabIds":["t.0"]}}}],"writeControl":{"requiredRevisionId":"rev-7"}}`, string(requests[1].body))
}

func TestReplaceDocumentTextWritesNothingWhenAPlaceholderIsMissing(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	draft := addPolicyDraft(provider)
	input := fillPlaceholdersInput(draft.RevisionID())
	input.Placeholders = append(input.Placeholders, docs.PlaceholderReplacement{Placeholder: "{{policyOwner}}", Text: "finance@acme.example"})

	result, err := sdkgo.RunMutation(newDexContext("fill-missing"), newDocsClient(t, provider.URL).ReplaceDocumentText(), docsConnection, input)
	require.NoError(t, err)
	require.Equal(t, docs.ReplaceDocumentTextBranchPlaceholderNotFound, result.Branch)
	require.Equal(t, []string{"{{policyOwner}}"}, result.Value.MissingPlaceholders)
	require.Equal(t, draft.RevisionID(), result.Value.RevisionID)
	require.Zero(t, provider.Count(fakegoogledocs.CountBatchUpdatesReceived), "the other placeholders are not replaced either")
}

func TestReplaceDocumentTextRetryAfterALostResponseRecognizesItsOwnWrite(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	draft := addPolicyDraft(provider)
	provider.SetWriteBehavior(fakegoogledocs.WriteKindReplace, fakegoogledocs.WriteBehavior{StatusAfterApplying: http.StatusServiceUnavailable})
	client := newDocsClient(t, provider.URL)
	input := fillPlaceholdersInput(draft.RevisionID())

	_, err := sdkgo.RunMutation(newDexContext("fill-lost"), client.ReplaceDocumentText(), docsConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)

	retried, err := sdkgo.RunMutation(newDexContext("fill-lost"), client.ReplaceDocumentText(), docsConnection, input)
	require.NoError(t, err)
	require.Equal(t, docs.ReplaceDocumentTextBranchReplaced, retried.Branch)
	require.True(t, retried.Value.WasAlreadyApplied)
	require.Equal(t, "rev-draftDoc1-2", retried.Value.RevisionID)
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountBatchUpdatesApplied))
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountBatchUpdatesReceived), "the retry sees the moved revision and sends nothing")
}

// Two dispatches that both read the required revision send the same guard; Google applies only the first.
func TestReplaceDocumentTextConcurrentDispatchesApplyOnce(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	draft := addPolicyDraft(provider)
	provider.SetWriteBehavior(fakegoogledocs.WriteKindReplace, fakegoogledocs.WriteBehavior{DelayBeforeApplying: 500 * time.Millisecond})
	client := newDocsClient(t, provider.URL)
	input := fillPlaceholdersInput(draft.RevisionID())

	var slowResult sdkgo.MutationResult[docs.ReplaceDocumentTextOutput]
	var slowErr error
	var waitGroup sync.WaitGroup
	waitGroup.Add(1)
	go func() {
		defer waitGroup.Done()
		slowResult, slowErr = sdkgo.RunMutation(newDexContext("fill-concurrent"), client.ReplaceDocumentText(), docsConnection, input)
	}()
	require.Eventually(t, func() bool { return provider.Count(fakegoogledocs.CountBatchUpdatesReceived) == 1 }, 5*time.Second, 5*time.Millisecond,
		"the slow dispatch's batch is delayed before Google checks its revision")
	fastResult, err := sdkgo.RunMutation(newDexContext("fill-concurrent"), client.ReplaceDocumentText(), docsConnection, input)
	require.NoError(t, err)
	waitGroup.Wait()
	require.NoError(t, slowErr)

	require.Equal(t, docs.ReplaceDocumentTextBranchReplaced, fastResult.Branch)
	require.False(t, fastResult.Value.WasAlreadyApplied)
	require.Equal(t, docs.ReplaceDocumentTextBranchReplaced, slowResult.Branch)
	require.True(t, slowResult.Value.WasAlreadyApplied, "the slow dispatch found the text the fast one wrote")
	require.Equal(t, 2, provider.Count(fakegoogledocs.CountBatchUpdatesReceived))
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountStaleRevisionRejected))
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountBatchUpdatesApplied))
	filled, _ := provider.Document(draftDocumentID)
	require.Equal(t, filledPolicyParagraphs, filled.Paragraphs)
}

func TestReplaceDocumentTextReportsAnotherWritersChange(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	draft := addPolicyDraft(provider)
	provider.EditDocument(draftDocumentID, append([]fakegoogledocs.Paragraph{{Text: "DRAFT - do not publish"}}, draft.Paragraphs...))

	result, err := sdkgo.RunMutation(newDexContext("fill-changed"), newDocsClient(t, provider.URL).ReplaceDocumentText(), docsConnection, fillPlaceholdersInput(draft.RevisionID()))
	require.NoError(t, err)
	require.Equal(t, docs.ReplaceDocumentTextBranchRevisionChanged, result.Branch)
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Equal(t, "rev-draftDoc1-2", result.Value.RevisionID, "the current revision lets the Flow read again")
	require.Zero(t, provider.Count(fakegoogledocs.CountBatchUpdatesReceived))
}

func TestReplaceDocumentTextRejectionAtTheRequiredRevisionIsTerminal(t *testing.T) {
	document := `{"documentId":"draftDoc1","revisionId":"rev-7","tabs":[{"tabProperties":{"tabId":"t.0"},
		"documentTab":{"body":{"content":[{"endIndex":1,"sectionBreak":{}},{"startIndex":1,"endIndex":19,"paragraph":{"elements":[{"textRun":{"content":"{{effectiveDate}}\n"}}]}}]}}}]}`
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, document)
			return
		}
		writeJSON(t, response, http.StatusBadRequest, `{"error":{"code":400,"message":"SENTINEL","status":"INVALID_ARGUMENT"}}`)
	})
	result, err := sdkgo.RunMutation(newDexContext("fill-rejected"), newDocsClient(t, server.URL).ReplaceDocumentText(), docsConnection, docs.ReplaceDocumentTextInput{
		DocumentID: draftDocumentID, RequiredRevisionID: "rev-7", Target: docs.ReplaceTargetPlaceholders,
		Placeholders: []docs.PlaceholderReplacement{{Placeholder: "{{effectiveDate}}", Text: "2026-10-01"}},
	})
	require.NoError(t, err)
	require.Equal(t, docs.ReplaceDocumentTextBranchProviderRejected, result.Branch)
	require.Len(t, server.recorded(), 3, "a 400 is classified by reading the revision again")
	requireSecretFree(t, result)
}

func TestReplaceDocumentTextReplacesTheWholeBodyWithPlainParagraphs(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	draft := addPolicyDraft(provider)
	input := docs.ReplaceDocumentTextInput{DocumentID: draftDocumentID, RequiredRevisionID: draft.RevisionID(), Target: docs.ReplaceTargetWholeBody, Text: "Withdrawn.\nSee the 2027 policy."}

	result, err := sdkgo.RunMutation(newDexContext("replace-body"), newDocsClient(t, provider.URL).ReplaceDocumentText(), docsConnection, input)
	require.NoError(t, err)
	require.Equal(t, docs.ReplaceDocumentTextBranchReplaced, result.Branch)
	replaced, _ := provider.Document(draftDocumentID)
	require.Equal(t, []fakegoogledocs.Paragraph{{Text: "Withdrawn."}, {Text: "See the 2027 policy."}}, replaced.Paragraphs,
		"the inserted paragraphs lose the deleted heading's style")

	repeated, err := sdkgo.RunMutation(newDexContext("replace-body"), newDocsClient(t, provider.URL).ReplaceDocumentText(), docsConnection, input)
	require.NoError(t, err)
	require.Equal(t, docs.ReplaceDocumentTextBranchReplaced, repeated.Branch)
	require.True(t, repeated.Value.WasAlreadyApplied)
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountBatchUpdatesApplied))
}

func TestReplaceDocumentTextWholeBodyRequestsUseFirstTabIndexes(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"documentId":"draftDoc1","revisionId":"rev-7","tabs":[{"tabProperties":{"tabId":"t.0"},
				"documentTab":{"body":{"content":[{"endIndex":1,"sectionBreak":{}},{"startIndex":1,"endIndex":5,"paragraph":{"elements":[{"textRun":{"content":"Old\n"}}]}},
				{"startIndex":5,"endIndex":9,"paragraph":{"elements":[{"textRun":{"content":"Bye\n"}}]}}]}}}]}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"documentId":"draftDoc1","replies":[{},{},{},{},{}],"writeControl":{"requiredRevisionId":"rev-8"}}`)
	})
	_, err := sdkgo.RunMutation(newDexContext("replace-body-shape"), newDocsClient(t, server.URL).ReplaceDocumentText(), docsConnection, docs.ReplaceDocumentTextInput{
		DocumentID: draftDocumentID, RequiredRevisionID: "rev-7", Target: docs.ReplaceTargetWholeBody, Text: "New",
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"requests":[
		{"deleteContentRange":{"range":{"startIndex":1,"endIndex":8,"tabId":"t.0"}}},
		{"insertText":{"text":"New","location":{"index":1,"tabId":"t.0"}}},
		{"deleteParagraphBullets":{"range":{"startIndex":1,"endIndex":4,"tabId":"t.0"}}},
		{"updateParagraphStyle":{"range":{"startIndex":1,"endIndex":4,"tabId":"t.0"},"paragraphStyle":{"namedStyleType":"NORMAL_TEXT"},"fields":"*"}},
		{"updateTextStyle":{"range":{"startIndex":1,"endIndex":4,"tabId":"t.0"},"textStyle":{},"fields":"*"}}
	],"writeControl":{"requiredRevisionId":"rev-7"}}`, string(server.recorded()[1].body))
}

func TestReplaceDocumentTextNeedsEditAccess(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"documentId":"draftDoc1","tabs":[{"tabProperties":{"tabId":"t.0"},
			"documentTab":{"body":{"content":[{"endIndex":1,"sectionBreak":{}},{"startIndex":1,"endIndex":2,"paragraph":{"elements":[{"textRun":{"content":"\n"}}]}}]}}}]}`)
	})
	result, err := sdkgo.RunMutation(newDexContext("replace-viewer"), newDocsClient(t, server.URL).ReplaceDocumentText(), docsConnection, docs.ReplaceDocumentTextInput{
		DocumentID: draftDocumentID, RequiredRevisionID: "rev-7", Target: docs.ReplaceTargetWholeBody, Text: "New",
	})
	require.NoError(t, err)
	require.Equal(t, docs.ReplaceDocumentTextBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	require.Len(t, server.recorded(), 1)
}

func TestReplaceDocumentTextMapsAMissingDocumentToNotFound(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	result, err := sdkgo.RunMutation(newDexContext("replace-missing"), newDocsClient(t, provider.URL).ReplaceDocumentText(), docsConnection, fillPlaceholdersInput("rev-1"))
	require.NoError(t, err)
	require.Equal(t, docs.ReplaceDocumentTextBranchNotFound, result.Branch)
}

func TestReplaceDocumentTextRejectsAmbiguousInputWithoutARequest(t *testing.T) {
	server := newRecordingServer(t, func(http.ResponseWriter, recordedRequest) { t.Fatal("no request is expected") })
	tooMany := make([]docs.PlaceholderReplacement, 51)
	for index := range tooMany {
		tooMany[index] = docs.PlaceholderReplacement{Placeholder: "{{p" + strings.Repeat("x", index) + "}}"}
	}
	for name, change := range map[string]func(*docs.ReplaceDocumentTextInput){
		"missing revision":                  func(input *docs.ReplaceDocumentTextInput) { input.RequiredRevisionID = "" },
		"revision with spaces":              func(input *docs.ReplaceDocumentTextInput) { input.RequiredRevisionID = "rev 1" },
		"unknown target":                    func(input *docs.ReplaceDocumentTextInput) { input.Target = "range" },
		"text with placeholders":            func(input *docs.ReplaceDocumentTextInput) { input.Text = "body" },
		"no placeholders":                   func(input *docs.ReplaceDocumentTextInput) { input.Placeholders = nil },
		"too many placeholders":             func(input *docs.ReplaceDocumentTextInput) { input.Placeholders = tooMany },
		"duplicate placeholder":             func(input *docs.ReplaceDocumentTextInput) { input.Placeholders[1].Placeholder = "{{effectiveDate}}" },
		"placeholder inside another":        func(input *docs.ReplaceDocumentTextInput) { input.Placeholders[1].Placeholder = "{{effectiveDate}}s" },
		"replacement repeats a placeholder": func(input *docs.ReplaceDocumentTextInput) { input.Placeholders[0].Text = "see {{companyName}}" },
		"multi-line placeholder":            func(input *docs.ReplaceDocumentTextInput) { input.Placeholders[0].Placeholder = "{{a\nb}}" },
		"private-use character":             func(input *docs.ReplaceDocumentTextInput) { input.Placeholders[0].Text = string(rune(0xE000)) },
		"whole body with placeholders":      func(input *docs.ReplaceDocumentTextInput) { input.Target = docs.ReplaceTargetWholeBody },
	} {
		input := fillPlaceholdersInput("rev-1")
		change(&input)
		result, err := sdkgo.RunMutation(newDexContext("replace-invalid"), newDocsClient(t, server.URL).ReplaceDocumentText(), docsConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, docs.ReplaceDocumentTextBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
	}
}
