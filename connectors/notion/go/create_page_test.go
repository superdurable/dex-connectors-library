// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion_test

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/notion/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func validCreatePageInput() notion.CreatePageInput {
	return notion.CreatePageInput{
		DataSourceID: testDataSourceID,
		Properties: map[string]notion.PropertyValue{
			"Name": notion.TitleValue("Ada Lovelace"), "Email": notion.EmailValue("ada@example.com"),
			"Status": notion.SelectValue("New"),
		},
		BodyText: "Hello from the form.\nSecond line.\n\n\nNext paragraph.",
	}
}

type notionCredentialError struct{ err error }

func (credentials notionCredentialError) Resolve(sdkgo.Call) (notion.Credentials, error) {
	return notion.Credentials{}, credentials.err
}

func TestCreatePageSendsADataSourceParentPropertiesAndParagraphs(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
	})
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newNotionDexContext("create"), client.CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchCreated, result.Branch)
	require.Equal(t, testPageID, result.Value.PageID)
	require.Equal(t, testDataSourceID, result.Value.DataSourceID)
	require.NotNil(t, result.Value.Page)
	require.Equal(t, "Ada Lovelace", result.Value.Page.Title)
	require.False(t, result.Value.IsConfirmedAfterTimeout)
	require.Equal(t, testPageID, result.Receipt.ProviderObjectID)
	require.Equal(t, sdkgo.IdempotencyKey(result.Receipt.CallID), result.Receipt.IdempotencyKey)
	require.Equal(t, 1, provider.requestCount())
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/v1/pages", request.path)
	require.JSONEq(t, `{
		"parent":{"type":"data_source_id","data_source_id":"`+testDataSourceID+`"},
		"properties":{
			"Name":{"title":[{"type":"text","text":{"content":"Ada Lovelace"}}]},
			"Email":{"email":"ada@example.com"},
			"Status":{"select":{"name":"New"}}
		},
		"children":[
			{"object":"block","type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"Hello from the form.\nSecond line."}}]}},
			{"object":"block","type":"paragraph","paragraph":{"rich_text":[{"type":"text","text":{"content":"Next paragraph."}}]}}
		]}`, request.body)
}

func TestCreatePageResolvesADatabaseIDBeforeWriting(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, databaseJSON(testDataSourceID))
			return
		}
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
	})
	client := newNotionClient(t, provider.URL)
	input := validCreatePageInput()
	input.DataSourceID, input.DatabaseID, input.BodyText = "", testDatabaseID, ""
	result, err := sdkgo.RunMutation(newNotionDexContext("create-database"), client.CreatePage(), notionConnection, input)
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchCreated, result.Branch)
	require.Equal(t, "/v1/databases/"+testDatabaseID, provider.request(0).path)
	require.NotContains(t, provider.request(1).body, "children")
	require.Contains(t, provider.request(1).body, `"data_source_id":"`+testDataSourceID+`"`)
}

func TestCreatePageWritesNothingWhenTheDatabaseLookupFails(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		branch  sdkgo.BranchID
		isRetry bool
	}{
		{name: "unshared database", status: http.StatusNotFound, body: notionError(404, "object_not_found"), branch: notion.CreatePageBranchNotFound},
		{name: "several data sources", status: http.StatusOK, body: databaseJSON(testDataSourceID, "5d7f9213-4e6f-4081-92a3-445566778899"), branch: notion.CreatePageBranchDefect},
		{name: "invalid database", status: http.StatusOK, body: `{"object":"page"}`, branch: notion.CreatePageBranchInvalidResponse},
		{name: "unavailable lookup", status: http.StatusServiceUnavailable, body: notionError(503, "service_unavailable"), isRetry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newNotionClient(t, provider.URL)
			input := validCreatePageInput()
			input.DataSourceID, input.DatabaseID = "", testDatabaseID
			result, err := sdkgo.RunMutation(newNotionDexContext("lookup"), client.CreatePage(), notionConnection, input)
			if test.isRetry {
				requireRetry(t, err, sdkgo.FailureAvailability)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.branch, result.Branch)
				requireNoSentinel(t, result)
			}
			require.Equal(t, 1, provider.requestCount(), "the create is never sent")
		})
	}
}

func TestCreatePageRetriesAfterAnUnavailableDatabaseLookupWithoutDuplicatingThePost(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if index == 0 {
			require.Equal(t, http.MethodGet, request.Method)
			writeJSON(t, response, http.StatusServiceUnavailable, notionError(503, "service_unavailable"))
			return
		}
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, databaseJSON(testDataSourceID))
			return
		}
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
	})
	input := validCreatePageInput()
	input.DataSourceID, input.DatabaseID = "", testDatabaseID
	ctx := singledispatchtest.NewContext("notion-flow", "create-page")
	client := newNotionClient(t, provider.URL)
	_, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, input)
	requireRetry(t, err, sdkgo.FailureAvailability)
	require.Equal(t, 1, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(0).method)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, input)
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchCreated, result.Branch)
	require.Equal(t, 3, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(1).method)
	require.Equal(t, http.MethodPost, provider.request(2).method)
}

// TestCreatePageMapsTheFirstSendOutcome covers definite refusals, accepted answers, and unknown outcomes.
func TestCreatePageMapsTheFirstSendOutcome(t *testing.T) {
	for _, test := range []struct {
		name    string
		status  int
		body    string
		branch  sdkgo.BranchID
		kind    sdkgo.FailureKind
		isRetry bool
	}{
		{name: "validation error", status: http.StatusBadRequest, body: notionError(400, "validation_error"), branch: notion.CreatePageBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "missing capability or block limit", status: http.StatusForbidden, body: notionError(403, "restricted_resource"), branch: notion.CreatePageBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{name: "unshared data source", status: http.StatusNotFound, body: notionError(404, "object_not_found"), branch: notion.CreatePageBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "row limit", status: http.StatusNotAcceptable, body: notionError(406, "row_limit_exceeded"), isRetry: true, kind: sdkgo.FailureAvailability},
		{name: "internal error", status: http.StatusInternalServerError, body: notionError(500, "internal_server_error"), isRetry: true, kind: sdkgo.FailureAvailability},
		{name: "bad gateway", status: http.StatusBadGateway, body: notionError(502, "bad_gateway"), isRetry: true, kind: sdkgo.FailureAvailability},
		{name: "503 without a saved page", status: http.StatusServiceUnavailable, body: notionError(503, "service_unavailable"), isRetry: true, kind: sdkgo.FailureAvailability},
		{name: "gateway timeout", status: http.StatusGatewayTimeout, body: notionError(504, "gateway_timeout"), isRetry: true, kind: sdkgo.FailureAvailability},
		{name: "unusable 200", status: http.StatusOK, body: `{"object":"page"}`, branch: notion.CreatePageBranchUncertain, kind: sdkgo.FailureProtocol},
		{name: "unparsable 200", status: http.StatusOK, body: `{"object":`, branch: notion.CreatePageBranchUncertain, kind: sdkgo.FailureProtocol},
		{name: "rate limited", status: http.StatusTooManyRequests, body: notionError(429, "rate_limited"), isRetry: true, kind: sdkgo.FailureRateLimit},
		{name: "overloaded", status: 529, body: notionError(529, "service_overload"), isRetry: true, kind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newNotionClient(t, provider.URL)
			result, err := sdkgo.RunMutation(newNotionDexContext("create-"+test.name), client.CreatePage(), notionConnection, validCreatePageInput())
			if test.isRetry {
				requireRetry(t, err, test.kind)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.branch, result.Branch)
				require.Equal(t, test.kind, result.Failure.Kind)
				require.Empty(t, result.Value.PageID)
				require.Equal(t, testDataSourceID, result.Value.DataSourceID)
				requireNoSentinel(t, result)
			}
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestCreatePageResendsAnUnknownOutcomeOnlyBelowTheSendCap(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusConflict, 529} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				writeJSON(t, response, status, notionError(status, "unknown"))
			})
			client := newNotionClient(t, provider.URL)
			ctx := singledispatchtest.NewContext("notion-flow", "create-page-cap")
			for attempt := 1; attempt < 3; attempt++ {
				_, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry, "attempt %d is below the send cap of 3", attempt)
				ctx.AdvanceAttempt()
			}
			result, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
			require.NoError(t, err)
			require.Equal(t, notion.CreatePageBranchUncertain, result.Branch)
			ctx.AdvanceAttempt()
			result, err = sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
			require.NoError(t, err)
			require.Equal(t, notion.CreatePageBranchUncertain, result.Branch, "no send is allowed at the cap")
			posts := 0
			for index := range provider.requestCount() {
				if provider.request(index).method == http.MethodPost {
					posts++
				}
			}
			require.Equal(t, 3, posts)
		})
	}
}

func TestCreatePageRecordsCanResendBeforeADefiniteRefusal(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusBadRequest, notionError(400, "validation_error"))
	})
	ctx := singledispatchtest.NewContext("notion-flow", "create-page-refused")
	result, err := sdkgo.RunMutation(ctx, newNotionClient(t, provider.URL).CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchProviderRejected, result.Branch)
	heartbeats := ctx.Heartbeats()
	require.Contains(t, string(heartbeats[len(heartbeats)-1]), `"state":"canResend"`)
}

func TestCreatePageSelectsUncertainForARefusalAfterAPossibleSend(t *testing.T) {
	var posts int
	provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method != http.MethodPost {
			return
		}
		posts++
		if posts == 1 {
			writeJSON(t, response, http.StatusBadGateway, notionError(502, "bad_gateway"))
			return
		}
		writeJSON(t, response, http.StatusBadRequest, notionError(400, "validation_error"))
	})
	client := newNotionClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("notion-flow", "create-page-refused-after-send")
	_, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchUncertain, result.Branch, "the first send may have created the page")
	require.Equal(t, 2, posts)
}

func TestCreatePageResendsAnUncommitted409BelowTheSendCap(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusConflict, notionError(409, "conflict_error"))
			return
		}
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
	})
	client := newNotionClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("notion-flow", "create-page")
	_, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
	requireRetry(t, err, sdkgo.FailureConflict)
	require.Equal(t, 1, provider.requestCount())
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchCreated, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodPost, provider.request(0).method)
	require.Equal(t, http.MethodPost, provider.request(1).method)
}

func TestCreatePageConfirmsAPageNotionSavedBeforeA503(t *testing.T) {
	const savedPageID = "6e8a0324-5f70-4192-a3b4-556677889900"
	saved := `{"object":"error","status":503,"code":"service_unavailable","message":"SENTINEL saved",` +
		`"additional_data":{"retry_guidance":["Read the object again to confirm the saved change.","Do not repeat the write."],` +
		`"committed_resource_id":"` + savedPageID + `"}}`
	for _, test := range []struct {
		name         string
		readStatus   int
		isPageLoaded bool
	}{
		{name: "the saved page is read back", readStatus: http.StatusOK, isPageLoaded: true},
		{name: "the read-back also fails", readStatus: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
				if request.Method == http.MethodPost {
					writeJSON(t, response, http.StatusServiceUnavailable, saved)
					return
				}
				if test.readStatus != http.StatusOK {
					writeJSON(t, response, test.readStatus, notionError(test.readStatus, "service_unavailable"))
					return
				}
				writeJSON(t, response, http.StatusOK, pageJSON(savedPageID, "Ada Lovelace"))
			})
			client := newNotionClient(t, provider.URL)
			result, err := sdkgo.RunMutation(newNotionDexContext("saved"), client.CreatePage(), notionConnection, validCreatePageInput())
			if test.isPageLoaded {
				require.NoError(t, err)
				require.Equal(t, notion.CreatePageBranchCreated, result.Branch)
				require.Equal(t, savedPageID, result.Value.PageID)
				require.True(t, result.Value.IsConfirmedAfterTimeout)
				require.NotNil(t, result.Value.Page)
				require.Equal(t, savedPageID, result.Receipt.ProviderObjectID)
			} else {
				requireRetry(t, err, sdkgo.FailureAvailability)
			}
			require.Equal(t, 2, provider.requestCount())
			require.Equal(t, http.MethodGet, provider.request(1).method)
			require.Equal(t, "/v1/pages/"+savedPageID, provider.request(1).path)
			requireNoSentinel(t, result)
		})
	}
}

func TestCreatePageReadBackKeepsTheResolvedDataSourceForAnotherParentType(t *testing.T) {
	const savedPageID = "6e8a0324-5f70-4192-a3b4-556677889900"
	page := pageJSON(savedPageID, "Ada Lovelace")
	pageWithOtherParent := strings.Replace(page,
		`"parent":{"type":"data_source_id","data_source_id":"`+testDataSourceID+`","database_id":"`+testDatabaseID+`"}`,
		`"parent":{"type":"page_id","page_id":"`+testPageID+`"}`, 1)
	require.NotEqual(t, page, pageWithOtherParent)
	provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusServiceUnavailable, `{"object":"error","status":503,"code":"service_unavailable","additional_data":{"committed_resource_id":"`+savedPageID+`"}}`)
			return
		}
		writeJSON(t, response, http.StatusOK, pageWithOtherParent)
	})
	result, err := sdkgo.RunMutation(newNotionDexContext("parent-type"), newNotionClient(t, provider.URL).CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchCreated, result.Branch)
	require.Equal(t, testDataSourceID, result.Value.DataSourceID)
	require.Equal(t, savedPageID, result.Value.PageID)
}

func TestCreatePageFenceRetries429And529ThenCreatesThePage(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, 529} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				if index == 0 {
					writeJSON(t, response, status, notionError(status, "rate_limited"))
					return
				}
				writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
			})
			client := newNotionClient(t, provider.URL)
			ctx := singledispatchtest.NewContext("notion-flow", "create-page")
			_, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
			if status == http.StatusTooManyRequests {
				requireRetry(t, err, sdkgo.FailureRateLimit)
			} else {
				// A 529 is a 5xx, so the claim counts it as a possible send.
				requireRetry(t, err, sdkgo.FailureAvailability)
			}
			ctx.AdvanceAttempt()
			result, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
			require.NoError(t, err)
			require.Equal(t, notion.CreatePageBranchCreated, result.Branch)
			require.Equal(t, 2, provider.requestCount())
			require.Equal(t, provider.request(0).body, provider.request(1).body)
		})
	}
}

func TestCreatePageFenceCountsALostClaimAsAPossibleSend(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusServiceUnavailable, notionError(503, "service_unavailable"))
	})
	client := newNotionClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("notion-flow", "create-page")
	ctx.LoseNextHeartbeat()
	for attempt := 1; attempt < 3; attempt++ {
		_, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
		requireRetry(t, err, sdkgo.FailureAvailability)
		ctx.AdvanceAttempt()
	}
	result, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchUncertain, result.Branch, "the lost claim's attempt still counts toward the cap")
	require.Equal(t, 3, provider.requestCount())
}

func TestCreatePageFenceCredentialFailureRecordsNotSent(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
	})
	failedClient, err := notion.New(notion.Config{Endpoint: provider.URL}, notionCredentialError{err: projectconfig.ErrCredentialBrokerUnavailable})
	require.NoError(t, err)
	ctx := singledispatchtest.NewContext("notion-flow", "create-page")
	_, err = sdkgo.RunMutation(ctx, failedClient.CreatePage(), notionConnection, validCreatePageInput())
	requireRetry(t, err, sdkgo.FailureAuthentication)
	require.Zero(t, provider.requestCount())
	ctx.AdvanceAttempt()
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchCreated, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreatePageTerminalCredentialFailuresSelectDefectAfterNotSent(t *testing.T) {
	for _, test := range []struct {
		name        string
		credentials sdkgo.CredentialProvider[notion.Credentials]
	}{
		{name: "reauthorization required", credentials: notionCredentialError{err: sdkgo.ErrReauthorizationRequired}},
		{name: "connection not selected", credentials: notionCredentialError{err: projectconfig.ErrConnectionNotSelected}},
		{name: "broker rejected credentials", credentials: notionCredentialError{err: projectconfig.ErrCredentialBrokerRejected}},
		{name: "unclassified credential failure", credentials: notionCredentialError{err: errors.New("credential unavailable")}},
		{name: "invalid API token", credentials: sdkgo.StaticCredentialProvider[notion.Credentials]{
			notionConnection: {APIToken: sdkgo.NewSecretString("invalid\nheader")},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
			})
			client, err := notion.New(notion.Config{Endpoint: provider.URL}, test.credentials)
			require.NoError(t, err)
			ctx := singledispatchtest.NewContext("notion-flow", "create-page")
			result, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
			require.NoError(t, err)
			require.Equal(t, notion.CreatePageBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			require.Zero(t, provider.requestCount())
			ctx.AdvanceAttempt()
			result, err = sdkgo.RunMutation(ctx, newNotionClient(t, provider.URL).CreatePage(), notionConnection, validCreatePageInput())
			require.NoError(t, err)
			require.Equal(t, notion.CreatePageBranchCreated, result.Branch)
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

// TestCreatePageCheckpointWriteFailuresRetryAndCountThePossibleSend shows that a lost checkpoint
// returns Retry; the next attempt counts the possible send and, below the cap, posts again.
func TestCreatePageCheckpointWriteFailuresRetryAndCountThePossibleSend(t *testing.T) {
	checkpointFailure := errors.New("checkpoint unavailable")
	for _, test := range []struct {
		name       string
		prepare    func(*singledispatchtest.Context)
		respond    func(*singledispatchtest.Context, http.ResponseWriter, int)
		credential sdkgo.CredentialProvider[notion.Credentials]
		wantPosts  int
		wantKind   sdkgo.FailureKind
	}{
		{name: "claim", prepare: func(ctx *singledispatchtest.Context) { ctx.SetWriteError(checkpointFailure) }, wantPosts: 0, wantKind: sdkgo.FailureTransport},
		{name: "canResend", respond: func(ctx *singledispatchtest.Context, response http.ResponseWriter, index int) {
			if index == 0 {
				ctx.SetWriteError(checkpointFailure)
				writeJSON(t, response, http.StatusTooManyRequests, notionError(429, "rate_limited"))
				return
			}
			writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
		}, wantPosts: 1, wantKind: sdkgo.FailureAvailability},
		{name: "notSent", prepare: func(ctx *singledispatchtest.Context) { ctx.SetWriteError(checkpointFailure) },
			credential: notionCredentialError{err: projectconfig.ErrCredentialBrokerUnavailable}, wantPosts: 0, wantKind: sdkgo.FailureAvailability},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := singledispatchtest.NewContext("notion-flow", "create-page")
			provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				if test.respond != nil {
					test.respond(ctx, response, index)
					return
				}
				writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
			})
			if test.prepare != nil {
				test.prepare(ctx)
			}
			credentials := test.credential
			if credentials == nil {
				credentials = staticNotionCredentials()
			}
			client, err := notion.New(notion.Config{Endpoint: provider.URL}, credentials)
			require.NoError(t, err)
			_, err = sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
			requireRetry(t, err, test.wantKind)
			require.Equal(t, test.wantPosts, provider.requestCount())
			ctx.SetWriteError(nil)
			ctx.AdvanceAttempt()
			result, err := sdkgo.RunMutation(ctx, newNotionClient(t, provider.URL).CreatePage(), notionConnection, validCreatePageInput())
			require.NoError(t, err)
			require.Equal(t, notion.CreatePageBranchCreated, result.Branch, "one possible send is below the cap")
			require.Equal(t, test.wantPosts+1, provider.requestCount())
		})
	}
}

func TestCreatePageFenceReadsAcceptedPageWithoutAnotherPost(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
			return
		}
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
	})
	client := newNotionClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("notion-flow", "create-page")
	_, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchCreated, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(1).method)
}

func TestCreatePageFenceRetriesOnlyReadBackAfterCommitted503(t *testing.T) {
	const savedPageID = "6e8a0324-5f70-4192-a3b4-556677889900"
	provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, index int) {
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusServiceUnavailable, `{"object":"error","status":503,"code":"service_unavailable","additional_data":{"committed_resource_id":"`+savedPageID+`"}}`)
			return
		}
		if index == 1 {
			writeJSON(t, response, http.StatusServiceUnavailable, notionError(503, "service_unavailable"))
			return
		}
		writeJSON(t, response, http.StatusOK, pageJSON(savedPageID, "Ada Lovelace"))
	})
	client := newNotionClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("notion-flow", "create-page")
	_, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
	requireRetry(t, err, sdkgo.FailureAvailability)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchCreated, result.Branch)
	require.Equal(t, savedPageID, result.Value.PageID)
	require.Equal(t, 3, provider.requestCount())
	require.Equal(t, http.MethodPost, provider.request(0).method)
	require.Equal(t, http.MethodGet, provider.request(1).method)
	require.Equal(t, http.MethodGet, provider.request(2).method)
}

func TestCreatePageKeepsUncertaintyWhenAcceptedPageReadBackIsNotFound(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusServiceUnavailable, `{"object":"error","status":503,"code":"service_unavailable","additional_data":{"committed_resource_id":"`+testPageID+`"}}`)
			return
		}
		writeJSON(t, response, http.StatusNotFound, notionError(404, "object_not_found"))
	})
	client := newNotionClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("notion-flow", "create-page")
	result, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchUncertain, result.Branch)
	require.Equal(t, 2, provider.requestCount())
	ctx.AdvanceAttempt()
	result, err = sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchUncertain, result.Branch)
	require.Equal(t, 3, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(2).method)
}

func TestCreatePageReadsBackAnAcceptedPageWithAnUnusableBody(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodPost {
			writeJSON(t, response, http.StatusOK, `{"object":"page","id":"`+testPageID+`","properties":"bad"}`)
			return
		}
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
	})
	client := newNotionClient(t, provider.URL)
	result, err := sdkgo.RunMutation(newNotionDexContext("create-unusable"), client.CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchCreated, result.Branch)
	require.Equal(t, testPageID, result.Value.PageID)
	require.Equal(t, 2, provider.requestCount())
	require.Equal(t, http.MethodGet, provider.request(1).method)
}

func TestCreatePageDoesNotResendAnOversizedAcceptedPage(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
	})
	client, err := notion.New(notion.Config{Endpoint: provider.URL, MaxResponseBytes: 64}, staticNotionCredentials())
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(newNotionDexContext("oversized"), client.CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreatePageFenceCutsOffAnInFlightWrite(t *testing.T) {
	provider := newRecordingNotion(t, func(_ http.ResponseWriter, request *http.Request, _ int) {
		<-request.Context().Done()
	})
	client := newNotionClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("notion-flow", "create-page")
	stepDeadline := time.Now().Add(1200 * time.Millisecond)
	cancel := ctx.SetDeadline(stepDeadline)
	t.Cleanup(cancel)
	_, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	require.Equal(t, 1, provider.requestCount())
	require.Greater(t, time.Until(stepDeadline), 100*time.Millisecond, "the fence must leave a Step deadline margin")
}

func TestCreatePageDoesNotReplayOnAReusedConnectionWhenReplyIsLost(t *testing.T) {
	// Retain this guard if a future request header makes Go's transport replay POSTs on reused connections.
	provider := newRecordingNotion(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		if request.Method == http.MethodGet {
			writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
			return
		}
		connection, _, err := response.(http.Hijacker).Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	})
	httpClient := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 1}}
	t.Cleanup(httpClient.CloseIdleConnections)
	client := newNotionClient(t, provider.URL, notion.WithHTTPClient(httpClient))
	_, err := sdkgo.RunQuery(newNotionDexContext("prime"), client.GetPage(), notionConnection, notion.GetPageInput{
		PageID: testPageID, ShouldSkipContent: true,
	})
	require.NoError(t, err)
	_, err = sdkgo.RunMutation(newNotionDexContext("create-reused"), client.CreatePage(), notionConnection, validCreatePageInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	require.Equal(t, 2, provider.requestCount(), "net/http did not replay the POST")
	require.Equal(t, http.MethodPost, provider.request(1).method)
}

func TestCreatePageFenceRejectsStaleAndUnreadableMarkers(t *testing.T) {
	for _, test := range []struct {
		name    string
		prepare func(*singledispatchtest.Context)
	}{
		{name: "foreign", prepare: func(ctx *singledispatchtest.Context) {
			require.NoError(t, ctx.SetPersistedCheckpoint(singledispatch.Marker{CallID: "wrong", Attempt: 1, State: singledispatch.StateCanResend}))
		}},
		{name: "unreadable", prepare: func(ctx *singledispatchtest.Context) { ctx.SetReadError(errors.New("checkpoint unavailable")) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingNotion(t, func(http.ResponseWriter, *http.Request, int) {})
			ctx := singledispatchtest.NewContext("notion-flow", "create-page")
			ctx.AdvanceAttempt()
			test.prepare(ctx)
			_, err := sdkgo.RunMutation(ctx, newNotionClient(t, provider.URL).CreatePage(), notionConnection, validCreatePageInput())
			requireRetry(t, err, sdkgo.FailureAvailability)
			require.Zero(t, provider.requestCount(), "FailClosed never sends or selects a terminal branch")
		})
	}
}

func TestCreatePageFenceNeverUsesAStaleResendMarker(t *testing.T) {
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada Lovelace"))
	})
	client := newNotionClient(t, provider.URL)
	ctx := singledispatchtest.NewContext("notion-flow", "create-page")
	result, err := sdkgo.RunMutation(ctx, client.CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	ctx.AdvanceAttempt()
	require.NoError(t, ctx.SetPersistedCheckpoint(singledispatch.Marker{
		CallID: result.Receipt.CallID, Attempt: 1, State: singledispatch.StateCanResend,
	}))
	ctx.AdvanceAttempt()
	failedClient, err := notion.New(notion.Config{Endpoint: provider.URL}, failingCredentials{})
	require.NoError(t, err)
	result, err = sdkgo.RunMutation(ctx, failedClient.CreatePage(), notionConnection, validCreatePageInput())
	require.NoError(t, err)
	require.Equal(t, notion.CreatePageBranchUncertain, result.Branch)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreatePageTimeoutAfterDispatchIsResentBelowTheSendCap(t *testing.T) {
	release := make(chan struct{})
	provider := newRecordingNotion(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		<-release
		writeJSON(t, response, http.StatusOK, pageJSON(testPageID, "Ada"))
	})
	t.Cleanup(func() { close(release) })
	client := newNotionClient(t, provider.URL, notion.WithHTTPClient(&http.Client{Timeout: 200 * time.Millisecond}))
	_, err := sdkgo.RunMutation(newNotionDexContext("timeout"), client.CreatePage(), notionConnection, validCreatePageInput())
	requireRetry(t, err, sdkgo.FailureTransport)
	require.Equal(t, 1, provider.requestCount())
}

func TestCreatePageRetriesWhenNotionWasNeverReached(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	client := newNotionClient(t, "http://"+address)
	_, err = sdkgo.RunMutation(newNotionDexContext("unreachable"), client.CreatePage(), notionConnection, validCreatePageInput())
	retry := requireRetry(t, err, sdkgo.FailureTransport)
	require.Contains(t, retry.Failure.Message, "no page creation was written")
}

func TestCreatePageRejectsInvalidInputWithoutARequest(t *testing.T) {
	provider := newRecordingNotion(t, func(http.ResponseWriter, *http.Request, int) {})
	client := newNotionClient(t, provider.URL)
	tooManyParagraphs := strings.Repeat("paragraph\n\n", 101)
	for name, input := range map[string]notion.CreatePageInput{
		"no parent":           {Properties: map[string]notion.PropertyValue{"Name": notion.TitleValue("Ada")}},
		"two parents":         {DataSourceID: testDataSourceID, DatabaseID: testDatabaseID},
		"an invalid value":    {DataSourceID: testDataSourceID, Properties: map[string]notion.PropertyValue{"Email": notion.EmailValue("not an email")}},
		"too many paragraphs": {DataSourceID: testDataSourceID, BodyText: tooManyParagraphs},
		"too much body text":  {DataSourceID: testDataSourceID, BodyText: strings.Repeat("a", notion.MaximumBodyTextCharacters+1)},
	} {
		result, err := sdkgo.RunMutation(newNotionDexContext("invalid"), client.CreatePage(), notionConnection, input)
		require.NoError(t, err)
		require.Equal(t, notion.CreatePageBranchDefect, result.Branch, name)
	}
	require.Zero(t, provider.requestCount())
}
