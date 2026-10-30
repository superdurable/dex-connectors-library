// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/monday"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestAddUpdateEscapesPlainTextIntoTheHTMLBody(t *testing.T) {
	provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"create_update": map[string]any{
			"id": "3300001", "item_id": testItemID, "created_at": "2026-01-28T10:06:00Z", "creator_id": "48202303",
		}})
	})
	result, err := sdkgo.RunMutation(newMondayDexContext("update"), newMondayClient(t, provider.URL).AddUpdate(), mondayConnection, monday.AddUpdateInput{
		ItemID: testItemID, Body: "Scheduled for <b>Feb 18</b> & checked.\r\nOwner: facilities",
	})
	require.NoError(t, err)
	require.Equal(t, monday.AddUpdateBranchAdded, result.Branch)
	require.Equal(t, monday.AddUpdateOutput{
		ItemID: testItemID, UpdateID: "3300001", CreatorID: "48202303", CreatedAt: time.Date(2026, 1, 28, 10, 6, 0, 0, time.UTC),
	}, result.Value)
	require.Equal(t, "3300001", result.Receipt.ProviderObjectID)
	request := provider.request(0)
	require.Contains(t, request.query, "create_update(item_id: $itemId, body: $body)")
	require.NotEmpty(t, request.header.Get("Idempotency-Key"))
	require.Equal(t, "Scheduled for &lt;b&gt;Feb 18&lt;/b&gt; &amp; checked.<br>Owner: facilities", request.variables["body"])
}

func TestAddUpdateMapsOutcomes(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{name: "missing item", status: 200, body: `{"data":{"create_update":null},"errors":[{"message":"SENTINEL","extensions":{"code":"InvalidItemIdException"}}]}`, branch: monday.AddUpdateBranchNotFound},
		{name: "permission", status: 200, body: `{"errors":[{"message":"SENTINEL","extensions":{"code":"UserUnauthorizedException","status_code":403}}]}`, branch: monday.AddUpdateBranchProviderRejected},
		{name: "malformed update", status: 200, body: `{"data":{"create_update":{"id":"x"}}}`, branch: monday.AddUpdateBranchUncertain},
		{name: "update on another item", status: 200, body: `{"data":{"create_update":{"id":"3300001","item_id":"1111111111"}}}`, branch: monday.AddUpdateBranchUncertain},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingMonday(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunMutation(newMondayDexContext("update-"+test.name), newMondayClient(t, provider.URL).AddUpdate(), mondayConnection, monday.AddUpdateInput{ItemID: testItemID, Body: "Note"})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Empty(t, result.Value.UpdateID)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
		})
	}
}

func TestAddUpdateValidatesTheBody(t *testing.T) {
	provider := newRecordingMonday(t, func(http.ResponseWriter, recordedRequest, int) { t.Fatal("no request may be sent") })
	for name, body := range map[string]string{
		"blank":            " \n ",
		"too long":         strings.Repeat("x", monday.MaxUpdateBodyCharacters+1),
		"control":          "bell\a",
		"invalid encoding": "\xff",
	} {
		result, err := sdkgo.RunMutation(newMondayDexContext("invalid"), newMondayClient(t, provider.URL).AddUpdate(), mondayConnection, monday.AddUpdateInput{ItemID: testItemID, Body: body})
		require.NoError(t, err)
		require.Equal(t, monday.AddUpdateBranchDefect, result.Branch, name)
	}
}
