// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/docs"
	"github.com/superdurable/dex-connectors-library/connectors/google/docs/internal/fakegoogledocs"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func policyDraftInput() docs.CreateDocumentInput {
	return docs.CreateDocumentInput{
		Title: "Acme Refund Policy", ParentFolderID: "fld_policies",
		InitialText:       "# Refund Policy\n\nEffective {{effectiveDate}}\n\n- Refunds above {{approvalThreshold}} need a manager.",
		InitialTextFormat: docs.TextFormatMarkdown,
	}
}

func TestCreateDocumentConvertsTheInitialTextAndTagsTheIdempotencyKey(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"files":[]}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"id":"newDoc1","name":"Acme Refund Policy","mimeType":"application/vnd.google-apps.document",
			"parents":["fld_policies"],"webViewLink":"https://docs.google.com/document/d/newDoc1/edit","createdTime":"2026-10-01T09:00:00Z"}`)
	})
	ctx := newDexContext("create-converted")
	result, err := sdkgo.RunMutation(ctx, newDocsClient(t, server.URL).CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchCreated, result.Branch)
	require.False(t, result.Value.IsFromEarlierAttempt)
	require.Equal(t, "newDoc1", result.Value.Document.DocumentID)
	require.Equal(t, []string{"fld_policies"}, result.Value.Document.Parents)
	require.Equal(t, "newDoc1", result.Receipt.ProviderObjectID)
	require.JSONEq(t, `{"googleDocsDispatchedCallId":"`+string(result.Receipt.CallID)+`"}`, string(ctx.recordedHeartbeat),
		"the dispatch checkpoint is recorded before the create is sent")

	requests := server.recorded()
	require.Len(t, requests, 2)
	lookup := requests[0]
	require.Equal(t, "/drive/v3/files", lookup.path)
	require.Equal(t, []string{"appProperties has { key='dexIdempotencyKey' and value='" + string(result.Receipt.IdempotencyKey) + "' }"}, lookup.query["q"])
	require.Equal(t, []string{"allDrives"}, lookup.query["corpora"])
	create := requests[1]
	require.Equal(t, "/upload/drive/v3/files", create.path)
	require.Equal(t, []string{"multipart"}, create.query["uploadType"])
	require.Equal(t, []string{"true"}, create.query["supportsAllDrives"])
	metadata, mediaType, media := decodeMultipartCreate(t, create)
	require.JSONEq(t, `{"name":"Acme Refund Policy","mimeType":"application/vnd.google-apps.document","parents":["fld_policies"],
		"appProperties":{"dexIdempotencyKey":"`+string(result.Receipt.IdempotencyKey)+`"}}`, string(metadata))
	require.Equal(t, "text/markdown; charset=UTF-8", mediaType)
	require.Equal(t, policyDraftInput().InitialText, string(media))
}

func TestCreateDocumentCreatesAnEmptyDocumentWithMetadataOnly(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"files":[]}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"id":"emptyDoc","name":"Blank","mimeType":"application/vnd.google-apps.document"}`)
	})
	result, err := sdkgo.RunMutation(newDexContext("create-empty"), newDocsClient(t, server.URL).CreateDocument(), docsConnection,
		docs.CreateDocumentInput{Title: "Blank"})
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchCreated, result.Branch)
	create := server.recorded()[1]
	require.Equal(t, "/drive/v3/files", create.path)
	require.Equal(t, "application/json; charset=UTF-8", create.header.Get("Content-Type"))
	require.NotContains(t, string(create.body), "parents")
}

func TestCreateDocumentRetryFindsTheDocumentAnEarlierAttemptCreated(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	provider.QueueCreateResponses(fakegoogledocs.CreateResponseCreatedThenRateLimited)
	client := newDocsClient(t, provider.URL)
	ctx := newDexContext("create-lost-response")

	_, err := sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	requireRetry(t, err, sdkgo.FailureRateLimit)
	require.Nil(t, ctx.recordedHeartbeat, "a 429 clears the checkpoint because Google created nothing")

	recovered, err := sdkgo.RunMutation(ctx.nextAttempt(), client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchCreated, recovered.Branch)
	require.True(t, recovered.Value.IsFromEarlierAttempt)
	require.Equal(t, provider.CreatedDocumentIDs(), []string{recovered.Value.Document.DocumentID})
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountCreatesReceived))

	created, isFound := provider.Document(recovered.Value.Document.DocumentID)
	require.True(t, isFound)
	require.Equal(t, []fakegoogledocs.Paragraph{
		{Text: "Refund Policy", NamedStyleType: "HEADING_1"},
		{Text: "Effective {{effectiveDate}}"},
		{Text: "Refunds above {{approvalThreshold}} need a manager.", IsBullet: true},
	}, created.Paragraphs)

	another, err := sdkgo.RunMutation(newDexContext("create-new-step-execution"), client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.False(t, another.Value.IsFromEarlierAttempt, "a new Step execution is a new logical create")
	require.Len(t, provider.CreatedDocumentIDs(), 2)
}

func TestCreateDocumentRetriesARateLimitThatCreatedNothing(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	provider.QueueCreateResponses(fakegoogledocs.CreateResponseRateLimitedWithoutCreating)
	client := newDocsClient(t, provider.URL)
	ctx := newDexContext("create-rate-limited")

	_, err := sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	requireRetry(t, err, sdkgo.FailureRateLimit)
	result, err := sdkgo.RunMutation(ctx.nextAttempt(), client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchCreated, result.Branch)
	require.False(t, result.Value.IsFromEarlierAttempt)
	require.Len(t, provider.CreatedDocumentIDs(), 1)
	require.Equal(t, 2, provider.Count(fakegoogledocs.CountDuplicateLookups))
}

func TestCreateDocumentSelectsUncertainForAnUnconfirmedCreateAndNeverResends(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	provider.QueueCreateResponses(fakegoogledocs.CreateResponseCreatedThenServerError)
	provider.HideCreatedDocumentsFromLookup()
	client := newDocsClient(t, provider.URL)
	ctx := newDexContext("create-server-error")

	unknown, err := sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchUncertain, unknown.Branch)
	require.Equal(t, sdkgo.FailureAvailability, unknown.Failure.Kind)
	require.NotEmpty(t, ctx.recordedHeartbeat, "the checkpoint stays, so a replayed attempt sends nothing")
	requireSecretFree(t, unknown)

	replayed, err := sdkgo.RunMutation(ctx.nextAttempt(), client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchUncertain, replayed.Branch)
	require.Equal(t, "an earlier attempt of this Step may have created the document, and the duplicate lookup does not show it yet", replayed.Failure.Message)
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountCreatesReceived))
}

func TestCreateDocumentSendsNothingWhenDexCannotRecordTheCheckpoint(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	ctx := newDexContext("create-unrecordable")
	ctx.rejectsHeartbeat = true
	_, err := sdkgo.RunMutation(ctx, newDocsClient(t, provider.URL).CreateDocument(), docsConnection, policyDraftInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Zero(t, provider.Count(fakegoogledocs.CountCreatesReceived))
}

func TestCreateDocumentLookupFailuresNeverCreate(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body       string
		wantBranch sdkgo.BranchID
	}{
		{name: "lookup outage retries", status: http.StatusServiceUnavailable, body: `{}`},
		{name: "incomplete lookup retries", status: http.StatusOK, body: `{"files":[],"incompleteSearch":true}`},
		{name: "lookup rejection", status: http.StatusForbidden, body: `{"error":{"errors":[{"reason":"insufficientPermissions"}]}}`, wantBranch: docs.CreateDocumentBranchProviderRejected},
		{name: "a matching file that is not a Google Doc", status: http.StatusOK, body: `{"files":[{"id":"sheet1","mimeType":"application/vnd.google-apps.spreadsheet"}]}`, wantBranch: docs.CreateDocumentBranchInvalidResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) {
				writeJSON(t, response, test.status, test.body)
			})
			ctx := newDexContext("create-lookup")
			result, err := sdkgo.RunMutation(ctx, newDocsClient(t, server.URL).CreateDocument(), docsConnection, policyDraftInput())
			if test.wantBranch == "" {
				requireRetry(t, err, sdkgo.FailureAvailability)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.wantBranch, result.Branch)
			}
			require.Len(t, server.recorded(), 1)
			require.Zero(t, ctx.heartbeatCount, "no checkpoint before the create may be sent")
		})
	}
}

func TestCreateDocumentRejectsAMissingParentFolderWithoutProviderText(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"files":[]}`)
			return
		}
		writeJSON(t, response, http.StatusNotFound, `{"error":{"errors":[{"reason":"notFound","message":"File not found: SENTINEL"}]}}`)
	})
	result, err := sdkgo.RunMutation(newDexContext("create-missing-parent"), newDocsClient(t, server.URL).CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
	requireSecretFree(t, result)
}

func TestCreateDocumentRejectsInvalidInputWithoutARequest(t *testing.T) {
	server := newRecordingServer(t, func(http.ResponseWriter, recordedRequest) { t.Fatal("no request is expected") })
	for name, change := range map[string]func(*docs.CreateDocumentInput){
		"blank title":            func(input *docs.CreateDocumentInput) { input.Title = " " },
		"multi-line title":       func(input *docs.CreateDocumentInput) { input.Title = "Refund\nPolicy" },
		"invalid parent folder":  func(input *docs.CreateDocumentInput) { input.ParentFolderID = "a' in parents" },
		"unknown format":         func(input *docs.CreateDocumentInput) { input.InitialTextFormat = "html" },
		"carriage returns":       func(input *docs.CreateDocumentInput) { input.InitialText = "a\r\nb" },
		"text above the limit":   func(input *docs.CreateDocumentInput) { input.InitialText = strings.Repeat("a", 65) },
		"invalid UTF-8 in title": func(input *docs.CreateDocumentInput) { input.Title = "\xff" },
	} {
		input := policyDraftInput()
		change(&input)
		ctx := newDexContext("create-invalid")
		result, err := sdkgo.RunMutation(ctx, newDocsClient(t, server.URL, docs.Config{MaxTextBytes: 64}).CreateDocument(), docsConnection, input)
		require.NoError(t, err, name)
		require.Equal(t, docs.CreateDocumentBranchDefect, result.Branch, name)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind, name)
		require.Zero(t, ctx.heartbeatCount, name)
	}
}

func decodeMultipartCreate(t *testing.T, request recordedRequest) ([]byte, string, []byte) {
	t.Helper()
	mediaType, parameters, err := mime.ParseMediaType(request.header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/related", mediaType)
	reader := multipart.NewReader(bytes.NewReader(request.body), parameters["boundary"])
	metadataPart, err := reader.NextPart()
	require.NoError(t, err)
	require.Equal(t, "application/json; charset=UTF-8", metadataPart.Header.Get("Content-Type"))
	metadata, err := io.ReadAll(metadataPart)
	require.NoError(t, err)
	require.True(t, json.Valid(metadata))
	mediaPart, err := reader.NextPart()
	require.NoError(t, err)
	media, err := io.ReadAll(mediaPart)
	require.NoError(t, err)
	_, err = reader.NextPart()
	require.ErrorIs(t, err, io.EOF)
	return metadata, mediaPart.Header.Get("Content-Type"), media
}
