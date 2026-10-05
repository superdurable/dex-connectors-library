// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func dealJSON(id string, stage string, modifiedTime string) map[string]any {
	return map[string]any{"id": id, "Deal_Name": "Meridian Platform Deal", "Stage": stage, "Modified_Time": modifiedTime, "$editable": true}
}

func TestListModifiedRecordsAdvancesTheCursorPastTheLastRecordIncludingSameSecondTies(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"data": []any{
			dealJSON("4150868000003194003", "Negotiation", "2026-01-28T18:30:00+05:30"),
			dealJSON("4150868000003194011", "Closed Won", "2026-01-28T18:30:05+05:30"),
			dealJSON("4150868000003194012", "Qualification", "2026-01-28T18:30:05+05:30"),
		}, "info": map[string]any{"count": 3, "more_records": true}})
	})
	result, err := sdkgo.RunQuery(newCRMDexContext("list"), newCRMClient(t, provider.URL).ListModifiedRecords(), crmConnection, crm.ListModifiedRecordsInput{
		Module: crm.ModuleDeals, Fields: []string{"Deal_Name", "Stage"}, ModifiedSince: time.Date(2026, 1, 28, 13, 0, 0, 0, time.UTC), Limit: 3,
	})
	require.NoError(t, err)
	require.Equal(t, crm.ListModifiedRecordsBranchListed, result.Branch)
	require.Len(t, result.Value.Records, 3)
	require.True(t, result.Value.HasMore)
	require.Equal(t, "2026-01-28T13:00:05Z/4150868000003194012", result.Value.Cursor)
	require.Equal(t, "select Deal_Name, Stage, Modified_Time from Deals where Modified_Time >= '2026-01-28T13:00:00+00:00'"+
		" order by Modified_Time asc, id asc limit 0, 3", provider.selectQuery(t, 0))
}

func TestListModifiedRecordsKeepsTheCursorForAnEmptyPage(t *testing.T) {
	provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) { response.WriteHeader(http.StatusNoContent) })
	cursor := "2026-01-28T13:00:05Z/4150868000003194012"
	result, err := sdkgo.RunQuery(newCRMDexContext("list"), newCRMClient(t, provider.URL).ListModifiedRecords(), crmConnection, crm.ListModifiedRecordsInput{
		Module: crm.ModuleDeals, Fields: []string{"Stage"}, Cursor: cursor,
	})
	require.NoError(t, err)
	require.Equal(t, crm.ListModifiedRecordsBranchListed, result.Branch)
	require.Empty(t, result.Value.Records)
	require.False(t, result.Value.HasMore)
	require.Equal(t, cursor, result.Value.Cursor, "the next poll continues from the same watermark")
}

func TestListModifiedRecordsRejectsPagesThatWouldMoveTheCursorWrongly(t *testing.T) {
	for name, records := range map[string][]any{
		"out of order":         {dealJSON("4150868000003194012", "Won", "2026-01-28T18:30:05+05:30"), dealJSON("4150868000003194003", "Won", "2026-01-28T18:30:01+05:30")},
		"before the cursor":    {dealJSON("4150868000003194003", "Won", "2026-01-28T18:29:59+05:30")},
		"tie at the cursor id": {dealJSON("4150868000003194003", "Won", "2026-01-28T18:30:00+05:30")},
		"no modified time":     {map[string]any{"id": "4150868000003194003", "Stage": "Won"}},
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingCRM(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeValue(t, response, http.StatusOK, map[string]any{"data": records, "info": map[string]any{"more_records": false}})
			})
			result, err := sdkgo.RunQuery(newCRMDexContext("list"), newCRMClient(t, provider.URL).ListModifiedRecords(), crmConnection, crm.ListModifiedRecordsInput{
				Module: crm.ModuleDeals, Fields: []string{"Stage"}, Cursor: "2026-01-28T13:00:00Z/4150868000003194003",
			})
			require.NoError(t, err)
			require.Equal(t, crm.ListModifiedRecordsBranchInvalidResponse, result.Branch)
			require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
		})
	}
}
