// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/monday"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func validCreateItemInput() monday.CreateItemInput {
	return monday.CreateItemInput{
		BoardID: testBoardID, GroupID: "topics", ItemName: "Monthly Fire Drill Checklist - February",
		ColumnValues: map[string]monday.ColumnValue{
			"status": monday.StatusLabelValue("Working on it"),
			"date4":  monday.DateValue("2026-02-18"),
			"person": monday.PeopleValue(48202303),
		},
	}
}

func TestCreateItemSendsTypedColumnValuesAsOneJSONString(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		response.Header().Set("Idempotency-Replayed", "true")
		writeData(t, response, map[string]any{"create_item": itemJSON("5550001", "Monthly Fire Drill Checklist - February")})
	})
	result, err := sdkgo.RunMutation(newMondayDexContext("create"), newMondayClient(t, provider.URL).CreateItem(), mondayConnection, validCreateItemInput())
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchCreated, result.Branch)
	require.Equal(t, "5550001", result.Value.ItemID)
	require.Equal(t, "5550001", result.Value.Item.ID)
	require.True(t, result.Value.IsReplayed)
	require.Equal(t, map[string]string{monday.IdempotencyReplayedReceiptKey: "true"}, result.Receipt.Metadata)
	require.Equal(t, "5550001", result.Receipt.ProviderObjectID)

	request := provider.request(0)
	require.Contains(t, request.query, "create_item(board_id: $boardId, group_id: $groupId, item_name: $itemName, column_values: $columnValues")
	require.NotContains(t, request.query, "column_values(ids", "the create selection stays far below the 1 MB replay-cache limit")
	columnValues, isString := request.variables["columnValues"].(string)
	require.True(t, isString, "monday.com documents column_values as a JSON-encoded string")
	require.JSONEq(t, `{"date4":{"date":"2026-02-18"},"person":{"personsAndTeams":[{"id":48202303,"kind":"person"}]},"status":{"label":"Working on it"}}`, columnValues)
	require.Equal(t, testBoardID, request.variables["boardId"])
	require.Equal(t, "topics", request.variables["groupId"])
	require.Equal(t, false, request.variables["createLabelsIfMissing"])
}

func TestCreateItemWithoutGroupOrColumnsSendsNeither(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"create_item": itemJSON("5550002", "Plain")})
	})
	_, err := sdkgo.RunMutation(newMondayDexContext("create-plain"), newMondayClient(t, provider.URL).CreateItem(), mondayConnection, monday.CreateItemInput{
		BoardID: testBoardID, ItemName: "  Plain  ", CreatesLabelsIfMissing: true,
	})
	require.NoError(t, err)
	require.Equal(t, map[string]any{"boardId": testBoardID, "itemName": "Plain", "createLabelsIfMissing": true}, provider.request(0).variables)
}

func TestCreateItemKeepsAnItemReturnedBesideANestedError(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		item := itemJSON("5550003", "Partial")
		item["group"] = nil
		encoded, err := json.Marshal(map[string]any{"data": map[string]any{"create_item": item},
			"errors": []any{map[string]any{"message": "SENTINEL", "path": []any{"create_item", "group"}, "extensions": map[string]any{"code": "SomethingNested"}}}})
		require.NoError(t, err)
		writeJSON(t, response, http.StatusOK, string(encoded))
	})
	result, err := sdkgo.RunMutation(newMondayDexContext("partial"), newMondayClient(t, provider.URL).CreateItem(), mondayConnection, validCreateItemInput())
	require.NoError(t, err)
	require.Equal(t, monday.CreateItemBranchCreated, result.Branch, "the item exists, so reporting a rejection would invite a duplicate")
	require.Equal(t, "5550003", result.Value.ItemID)
}

func TestCreateItemRetriesEveryOutcomeTheIdempotencyKeyCovers(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(http.ResponseWriter)
		kind  sdkgo.FailureKind
		delay time.Duration
	}{
		{name: "in-flight duplicate", reply: func(response http.ResponseWriter) {
			response.Header().Set("Retry-After", "2")
			writeJSON(t, response, http.StatusConflict, `{"errors":[{"message":"A request with this idempotency key is currently being processed","extensions":{"code":"IDEMPOTENCY_CONFLICT"}}]}`)
		}, kind: sdkgo.FailureConflict, delay: 2 * time.Second},
		{name: "server error", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusInternalServerError, `{"errors":[{"message":"SENTINEL"}]}`)
		}, kind: sdkgo.FailureAvailability},
		{name: "lost response", reply: func(response http.ResponseWriter) { dropConnection(t, response) }, kind: sdkgo.FailureTransport},
		{name: "complexity", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusTooManyRequests, `{"errors":[{"message":"x","extensions":{"code":"COMPLEXITY_BUDGET_EXHAUSTED","retry_in_seconds":30}}]}`)
		}, kind: sdkgo.FailureRateLimit, delay: 30 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) { test.reply(response) })
			_, err := sdkgo.RunMutation(newMondayDexContext("create-"+test.name), newMondayClient(t, provider.URL).CreateItem(), mondayConnection, validCreateItemInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.kind, retry.Failure.Kind)
			if test.delay > 0 {
				var retryAfter *dex.RetryAfterError
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, test.delay, retryAfter.After)
			}
			require.NotEmpty(t, provider.request(0).header.Get("Idempotency-Key"))
		})
	}
}

func TestCreateItemMapsRejectionsAndUnusableAnswers(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{name: "invalid board", status: 200, body: `{"data":{"create_item":null},"errors":[{"message":"SENTINEL","extensions":{"code":"InvalidBoardIdException","status_code":200}}]}`,
			branch: monday.CreateItemBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "missing status label", status: 200, body: `{"data":{"create_item":null},"errors":[{"message":"SENTINEL","extensions":{"code":"ColumnValueException","error_data":{"column_id":"status"}}}]}`,
			branch: monday.CreateItemBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "item limit", status: 200, body: `{"errors":[{"message":"SENTINEL","extensions":{"code":"ItemsLimitationException"}}]}`,
			branch: monday.CreateItemBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "permission", status: 403, body: `{"errors":[{"message":"SENTINEL","extensions":{"code":"UserUnauthorizedException","status_code":403}}]}`,
			branch: monday.CreateItemBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{name: "malformed item", status: 200, body: `{"data":{"create_item":{"id":"SENTINEL"}}}`,
			branch: monday.CreateItemBranchUncertain, kind: sdkgo.FailureProtocol},
		{name: "null item without errors", status: 200, body: `{"data":{"create_item":null}}`,
			branch: monday.CreateItemBranchUncertain, kind: sdkgo.FailureProtocol},
		{name: "reflected token", status: 200, body: `{"data":{"create_item":{"id":"1","name":"` + testAPIToken + `"}}}`,
			branch: monday.CreateItemBranchUncertain, kind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunMutation(newMondayDexContext("create-"+test.name), newMondayClient(t, provider.URL).CreateItem(), mondayConnection, validCreateItemInput())
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Empty(t, result.Value.ItemID)
			require.Equal(t, "Monthly Fire Drill Checklist - February", result.Value.ItemName, "the request is echoed for reconciliation")
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "SENTINEL")
			require.NotContains(t, string(encoded), testAPIToken)
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestCreateItemValidatesInputWithoutARequest(t *testing.T) {
	provider := newRecordingMonday(t, func(http.ResponseWriter, recordedRequest, int) { t.Fatal("no request may be sent") })
	client := newMondayClient(t, provider.URL)
	for name, change := range map[string]func(*monday.CreateItemInput){
		"blank name":         func(input *monday.CreateItemInput) { input.ItemName = " " },
		"two-line name":      func(input *monday.CreateItemInput) { input.ItemName = "first\nsecond" },
		"name too long":      func(input *monday.CreateItemInput) { input.ItemName = string(make([]rune, 256)) },
		"board key":          func(input *monday.CreateItemInput) { input.BoardID = "OPS" },
		"group with a space": func(input *monday.CreateItemInput) { input.GroupID = "new group" },
		"name column":        func(input *monday.CreateItemInput) { input.ColumnValues["name"] = monday.TextValue("x") },
		"bad date":           func(input *monday.CreateItemInput) { input.ColumnValues["date4"] = monday.DateValue("18/02/2026") },
		"mixed fields": func(input *monday.CreateItemInput) {
			input.ColumnValues["status"] = monday.ColumnValue{Type: monday.ColumnTypeStatus, StatusLabel: "Done", Text: "Done"}
		},
	} {
		t.Run(name, func(t *testing.T) {
			input := validCreateItemInput()
			change(&input)
			result, err := sdkgo.RunMutation(newMondayDexContext("invalid"), client.CreateItem(), mondayConnection, input)
			require.NoError(t, err)
			require.Equal(t, monday.CreateItemBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
}
