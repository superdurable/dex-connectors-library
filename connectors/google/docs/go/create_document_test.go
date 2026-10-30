// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/google/docs/go"
	"github.com/superdurable/dex-connectors-library/connectors/google/docs/go/internal/fakegoogledocs"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
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
	require.JSONEq(t, `{"callId":"`+string(result.Receipt.CallID)+`","attempt":1,"state":"claimed","providerId":"newDoc1"}`, string(ctx.recordedHeartbeat))

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

func TestCreateDocumentRateLimitReadBackFindsTheDocumentInTheSameAttempt(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	provider.QueueCreateResponses(fakegoogledocs.CreateResponseCreatedThenRateLimited)
	client := newDocsClient(t, provider.URL)
	ctx := newDexContext("create-lost-response")

	recovered, err := sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchCreated, recovered.Branch)
	require.False(t, recovered.Value.IsFromEarlierAttempt)
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
	require.Equal(t, 3, provider.Count(fakegoogledocs.CountDuplicateLookups))
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
			if test.wantBranch == "" {
				require.Contains(t, string(ctx.recordedHeartbeat), `"state":"notSent"`)
			} else {
				require.Zero(t, ctx.heartbeatCount, "terminal lookup errors need no checkpoint")
			}
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

func TestCreateDocumentFenceRetries503OnceWithStableKey(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	provider.QueueCreateResponses(fakegoogledocs.CreateResponseUnavailableWithoutCreating)
	client := newDocsClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("docs-flow", "create-503")
	_, err := sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	ctx.AdvanceAttempt()
	created, err := sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchCreated, created.Branch)
	require.Equal(t, 2, provider.Count(fakegoogledocs.CountCreatesReceived))
	keys := provider.CreateKeys()
	require.Len(t, keys, 2)
	require.NotEmpty(t, keys[0])
	require.Equal(t, keys[0], keys[1])
}

func TestCreateDocumentRetries503WhenReadBackIsUnavailable(t *testing.T) {
	var lookupCount, createCount int
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			lookupCount++
			if lookupCount == 2 {
				writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
				return
			}
			writeJSON(t, response, http.StatusOK, `{"files":[]}`)
			return
		}
		createCount++
		if createCount == 1 {
			writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"id":"newDoc1","name":"Acme Refund Policy","mimeType":"application/vnd.google-apps.document"}`)
	})
	ctx := singledispatchtest.NewContext("docs-flow", "create-503-lookup-503")
	client := newDocsClient(t, server.URL)
	_, err := sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	frames := ctx.Heartbeats()
	require.NotEmpty(t, frames)
	require.Contains(t, string(frames[len(frames)-1]), `"state":"canResend"`)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchCreated, result.Branch)
	require.Equal(t, 2, countRecordedCreates(server.recorded()))
	require.Equal(t, 3, lookupCount)
}

func TestCreateDocumentPriorClaimLookupErrorsSelectUncertain(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{"rejected", http.StatusForbidden, `{"error":{"errors":[{"reason":"insufficientPermissions"}]}}`},
		{"invalid", http.StatusOK, `{"files":[{"id":"sheet1","mimeType":"application/vnd.google-apps.spreadsheet"}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
				if request.path == "/drive/v3/files" {
					writeJSON(t, response, test.status, test.body)
					return
				}
				writeJSON(t, response, http.StatusNotFound, `{}`)
			})
			ctx := singledispatchtest.NewContext("docs-flow", "prior-claim-"+test.name)
			ctx.AdvanceAttempt()
			result, err := sdkgo.RunMutation(ctx, newDocsClient(t, server.URL).CreateDocument(), docsConnection, policyDraftInput())
			require.NoError(t, err)
			require.Equal(t, docs.CreateDocumentBranchUncertain, result.Branch)
			require.Zero(t, countRecordedCreates(server.recorded()))
		})
	}
}

func TestCreateDocumentLegacyCheckpointReadsBackWithoutSending(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body       string
		wantBranch sdkgo.BranchID
		wantRetry  bool
	}{
		{"found", http.StatusOK, `{"files":[{"id":"oldDoc1","name":"Existing","mimeType":"application/vnd.google-apps.document"}]}`, docs.CreateDocumentBranchCreated, false},
		{"unavailable", http.StatusServiceUnavailable, `{}`, "", true},
		{"missing", http.StatusOK, `{"files":[]}`, docs.CreateDocumentBranchUncertain, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newRecordingServer(t, func(response http.ResponseWriter, _ recordedRequest) { writeJSON(t, response, test.status, test.body) })
			ctx := singledispatchtest.NewContext("docs-flow", "legacy-"+test.name)
			require.NoError(t, ctx.SetPersistedCheckpoint(map[string]string{"googleDocsDispatchedCallId": "legacy-call"}))
			ctx.AdvanceAttempt()
			result, err := sdkgo.RunMutation(ctx, newDocsClient(t, server.URL).CreateDocument(), docsConnection, policyDraftInput())
			if test.wantRetry {
				requireRetry(t, err, sdkgo.FailureAvailability)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.wantBranch, result.Branch)
			}
			require.Zero(t, countRecordedCreates(server.recorded()))
		})
	}
}

type refreshingCreateCredentials struct {
	token     string
	refreshes int
}

func (credentials *refreshingCreateCredentials) Resolve(sdkgo.Call) (docs.Credentials, error) {
	return docs.Credentials{AccessToken: sdkgo.NewSecretString(credentials.token)}, nil
}

func (credentials *refreshingCreateCredentials) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[docs.Credentials]) (docs.Credentials, error) {
	return credentials.Resolve(call)
}

func (credentials *refreshingCreateCredentials) ResolveAfterRejection(context.Context, sdkgo.Call, sdkgo.CredentialRefreshDriver[docs.Credentials]) (docs.Credentials, error) {
	credentials.refreshes++
	credentials.token = "token-b"
	return docs.Credentials{AccessToken: sdkgo.NewSecretString(credentials.token)}, nil
}

func TestCreateDocumentRefreshes401AndRetriesOnNextAttempt(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"files":[]}`)
			return
		}
		if request.header.Get("Authorization") == "Bearer token-a" {
			writeJSON(t, response, http.StatusUnauthorized, `{}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"id":"newDoc1","name":"Acme Refund Policy","mimeType":"application/vnd.google-apps.document"}`)
	})
	credentials := &refreshingCreateCredentials{token: "token-a"}
	client, err := docs.New(docs.Config{DocsEndpoint: server.URL, DriveEndpoint: server.URL}, credentials)
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("docs-flow", "refresh-401")
	_, err = sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	requireRetry(t, err, sdkgo.FailureAuthentication)
	require.Equal(t, 1, credentials.refreshes)
	frames := ctx.Heartbeats()
	require.Contains(t, string(frames[len(frames)-1]), `"state":"canResend"`)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchCreated, result.Branch)
	require.Equal(t, 2, countRecordedCreates(server.recorded()))
}

func TestCreateDocumentFenceRefusesStaleResendMarker(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	provider.QueueCreateResponses(fakegoogledocs.CreateResponseUnavailableWithoutCreating)
	client := newDocsClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("docs-flow", "stale-create")
	_, err := sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	ctx.AdvanceAttempt()
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountCreatesReceived))
}

func TestCreateDocumentFenceRecordsCredentialFailureBeforeSend(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	credentials := sdkgo.StaticCredentialProvider[docs.Credentials]{}
	client, err := docs.New(docs.Config{DocsEndpoint: provider.URL, DriveEndpoint: provider.URL}, credentials)
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("docs-flow", "credential-create")
	_, err = sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	requireRetry(t, err, sdkgo.FailureAuthentication)
	require.Zero(t, provider.Count(fakegoogledocs.CountCreatesReceived))
	credentials[docsConnection] = docs.Credentials{AccessToken: sdkgo.NewSecretString(docsTestToken)}
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchCreated, result.Branch)
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountCreatesReceived))
}

func TestCreateDocumentFenceReadsAcceptedDocumentOnLaterAttempt(t *testing.T) {
	provider := fakegoogledocs.New(t, docsTestToken)
	client := newDocsClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("docs-flow", "accepted-create")
	_, err := sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchCreated, result.Branch)
	require.True(t, result.Value.IsFromEarlierAttempt)
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountCreatesReceived))
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountFileReads))
	require.Equal(t, 1, provider.Count(fakegoogledocs.CountDuplicateLookups))
}

func TestCreateDocumentUnusableAcceptedBodyRequiresReadBack(t *testing.T) {
	lookupCount := 0
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			lookupCount++
			if lookupCount == 2 {
				writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
				return
			}
			writeJSON(t, response, http.StatusOK, `{"files":[]}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"name":"Missing ID"}`)
	})
	ctx := singledispatchtest.NewContext("docs-flow", "unusable-accepted")
	result, err := sdkgo.RunMutation(ctx, newDocsClient(t, server.URL).CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchUncertain, result.Branch)
	require.Equal(t, 1, countRecordedCreates(server.recorded()))
}

func TestCreateDocumentMidRequestDeadlineDoesNotResend(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"files":[]}`)
			return
		}
		<-request.requestContext.Done()
	})
	ctx := singledispatchtest.NewContext("docs-flow", "deadline-create")
	defer ctx.SetDeadline(time.Now().Add(2 * time.Second))()
	result, err := sdkgo.RunMutation(ctx, newDocsClient(t, server.URL).CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchUncertain, result.Branch)
	ctx.AdvanceAttempt()
	result, err = sdkgo.RunMutation(ctx, newDocsClient(t, server.URL).CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchUncertain, result.Branch)
	require.Equal(t, 1, countRecordedCreates(server.recorded()))
}

func countRecordedCreates(requests []recordedRequest) int {
	count := 0
	for _, request := range requests {
		if request.method == http.MethodPost {
			count++
		}
	}
	return count
}

type inspectingCreateTransport struct {
	base *http.Transport
	t    *testing.T
}

func (transport inspectingCreateTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost {
		require.Nil(transport.t, request.GetBody)
	}
	return transport.base.RoundTrip(request)
}

func TestCreateDocumentDroppedReplyDoesNotReplayHTTPWrite(t *testing.T) {
	var createCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"files":[]}`)
			return
		}
		createCount.Add(1)
		connection, _, err := response.(http.Hijacker).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	}))
	defer server.Close()
	base := http.DefaultTransport.(*http.Transport).Clone()
	defer base.CloseIdleConnections()
	client, err := docs.New(docs.Config{DocsEndpoint: server.URL, DriveEndpoint: server.URL},
		sdkgo.StaticCredentialProvider[docs.Credentials]{docsConnection: {AccessToken: sdkgo.NewSecretString(docsTestToken)}},
		docs.WithHTTPClient(&http.Client{Transport: inspectingCreateTransport{base: base, t: t}}))
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("docs-flow", "drop-create-reply"), client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchUncertain, result.Branch)
	require.EqualValues(t, 1, createCount.Load())
}

func TestCreateDocumentRetriesProvenConnectFailureBeforeWrite(t *testing.T) {
	var creates atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"files":[]}`)
			return
		}
		creates.Add(1)
		writeJSON(t, response, http.StatusOK, `{"id":"newDoc1","name":"Acme Refund Policy","mimeType":"application/vnd.google-apps.document"}`)
	}))
	defer server.Close()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	client, err := docs.New(docs.Config{DocsEndpoint: server.URL, DriveEndpoint: server.URL},
		sdkgo.StaticCredentialProvider[docs.Credentials]{docsConnection: {AccessToken: sdkgo.NewSecretString(docsTestToken)}},
		docs.WithHTTPClient(&http.Client{Transport: &failFirstCreateConnection{base: transport}}))
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("docs-flow", "connect-failed-create")
	_, err = sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	frames := ctx.Heartbeats()
	require.NotEmpty(t, frames)
	require.Contains(t, string(frames[len(frames)-1]), `"state":"canResend"`)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchCreated, result.Branch)
	require.EqualValues(t, 1, creates.Load())
}

type failFirstCreateConnection struct {
	base    http.RoundTripper
	creates atomic.Int32
}

func (transport *failFirstCreateConnection) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost && transport.creates.Add(1) == 1 {
		err := errors.New("connection refused before a request was sent")
		if trace := httptrace.ContextClientTrace(request.Context()); trace != nil && trace.ConnectDone != nil {
			trace.ConnectDone("tcp", request.URL.Host, err)
		}
		return nil, err
	}
	return transport.base.RoundTrip(request)
}

func TestCreateDocumentMalformedAcceptedBodyReadsBackBeforeReturning(t *testing.T) {
	lookupCount := 0
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			lookupCount++
			if lookupCount == 1 {
				writeJSON(t, response, http.StatusOK, `{"files":[]}`)
			} else {
				writeJSON(t, response, http.StatusOK, `{"files":[{"id":"foundDoc1","name":"Acme Refund Policy","mimeType":"application/vnd.google-apps.document"}]}`)
			}
			return
		}
		writeJSON(t, response, http.StatusOK, `{"id":`)
	})
	ctx := singledispatchtest.NewContext("docs-flow", "malformed-accepted-create")
	result, err := sdkgo.RunMutation(ctx, newDocsClient(t, server.URL).CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchCreated, result.Branch)
	require.Equal(t, "foundDoc1", result.Value.Document.DocumentID)
	require.Equal(t, 1, countRecordedCreates(server.recorded()))
	ctx.AdvanceAttempt()
	result, err = sdkgo.RunMutation(ctx, newDocsClient(t, server.URL).CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchCreated, result.Branch)
	require.Equal(t, 1, countRecordedCreates(server.recorded()))
}

func TestCreateDocumentLostClaimFailsClosedAfterUnusableAcceptedBody(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"files":[]}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"name":"Missing ID"}`)
	})
	client := newDocsClient(t, server.URL)
	ctx := singledispatchtest.NewContext("docs-flow", "lost-claim-create")
	ctx.LoseNextHeartbeat()
	result, err := sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchUncertain, result.Branch)
	ctx.AdvanceAttempt()
	result, err = sdkgo.RunMutation(ctx, client.CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchUncertain, result.Branch)
	require.Equal(t, 1, countRecordedCreates(server.recorded()))
}

func TestCreateDocumentOversizedAcceptedBodyIsNeverRetried(t *testing.T) {
	server := newRecordingServer(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, `{"files":[]}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"id":"`+strings.Repeat("x", 200)+`"}`)
	})
	ctx := singledispatchtest.NewContext("docs-flow", "oversized-accepted-create")
	result, err := sdkgo.RunMutation(ctx, newDocsClient(t, server.URL, docs.Config{MaxResponseBytes: 64}).CreateDocument(), docsConnection, policyDraftInput())
	require.NoError(t, err)
	require.Equal(t, docs.CreateDocumentBranchUncertain, result.Branch)
	require.Equal(t, 1, countRecordedCreates(server.recorded()))
}
