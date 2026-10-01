// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const changedEmployeesBody = `{"latest":"2026-09-30T18:05:00+00:00","employees":{
	"9":{"id":"9","action":"Updated","lastChanged":"2026-09-30T18:05:00+00:00"},
	"130":{"id":"130","action":"Inserted","lastChanged":"2026-09-30T16:00:00+00:00"},
	"12":{"id":"12","action":"Inserted","lastChanged":"2026-09-30T16:00:00+00:00"},
	"40":{"id":"40","action":"Deleted","lastChanged":"2026-09-30T09:59:59+00:00"}}}`

func TestListEmployeeChangesReturnsTheOldestChangesAfterTheCursor(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, changedEmployeesBody)
	})
	since := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	result, err := sdkgo.RunQuery(newBambooHRDexContext("changes"), newBambooHRClient(t, provider.URL).ListEmployeeChanges(), bambooHRConnection,
		bamboohr.ListEmployeeChangesInput{Since: since, Limit: 2})
	require.NoError(t, err)
	require.Equal(t, bamboohr.ListEmployeeChangesBranchListed, result.Branch)
	fourPM := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)
	require.Equal(t, bamboohr.ListEmployeeChangesOutput{
		Changes: []bamboohr.EmployeeChange{
			{EmployeeID: "12", Action: bamboohr.EmployeeChangeActionInserted, LastChanged: fourPM},
			{EmployeeID: "130", Action: bamboohr.EmployeeChangeActionInserted, LastChanged: fourPM},
		},
		NextCursor: bamboohr.EmployeeChangeCursor{Since: fourPM, AfterEmployeeID: "130"},
		HasMore:    true,
	}, result.Value, "employee 40 changed before the cursor; IDs order numerically, so 12 precedes 130")
	request := provider.request(0)
	require.Equal(t, "/api/v1/employees/changed", request.path)
	require.Equal(t, []string{"2026-09-30T09:59:59Z"}, request.query["since"], "one second of overlap covers an exclusive since")
	require.NotContains(t, request.query, "type")
}

func TestListEmployeeChangesContinuesFromATieBreakingCursor(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, changedEmployeesBody)
	})
	fourPM := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)
	result, err := sdkgo.RunQuery(newBambooHRDexContext("changes-next"), newBambooHRClient(t, provider.URL).ListEmployeeChanges(), bambooHRConnection,
		bamboohr.ListEmployeeChangesInput{Since: fourPM, AfterEmployeeID: "12", ChangeType: bamboohr.EmployeeChangeTypeInserted})
	require.NoError(t, err)
	sixPM := time.Date(2026, 9, 30, 18, 5, 0, 0, time.UTC)
	require.Equal(t, []bamboohr.EmployeeChange{
		{EmployeeID: "130", Action: bamboohr.EmployeeChangeActionInserted, LastChanged: fourPM},
		{EmployeeID: "9", Action: bamboohr.EmployeeChangeActionUpdated, LastChanged: sixPM},
	}, result.Value.Changes, "employee 12 at the cursor instant was already returned")
	require.Equal(t, bamboohr.EmployeeChangeCursor{Since: sixPM, AfterEmployeeID: "9"}, result.Value.NextCursor)
	require.False(t, result.Value.HasMore)
	require.Equal(t, []string{"inserted"}, provider.request(0).query["type"])
}

func TestListEmployeeChangesKeepsTheCursorWhenNothingChanged(t *testing.T) {
	for name, body := range map[string]string{
		"empty object": `{"latest":"","employees":{}}`,
		"empty array":  `{"latest":null,"employees":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, body)
			})
			since := time.Date(2026, 9, 30, 10, 0, 0, 0, time.FixedZone("MDT", -6*60*60))
			result, err := sdkgo.RunQuery(newBambooHRDexContext("changes-empty"), newBambooHRClient(t, provider.URL).ListEmployeeChanges(), bambooHRConnection,
				bamboohr.ListEmployeeChangesInput{Since: since, AfterEmployeeID: "77"})
			require.NoError(t, err)
			require.Equal(t, bamboohr.ListEmployeeChangesBranchListed, result.Branch)
			require.Empty(t, result.Value.Changes)
			require.Equal(t, bamboohr.EmployeeChangeCursor{Since: since.UTC(), AfterEmployeeID: "77"}, result.Value.NextCursor)
			require.Equal(t, []string{"2026-09-30T15:59:59Z"}, provider.request(0).query["since"])
		})
	}
}

func TestListEmployeeChangesRejectsInvalidChangeLists(t *testing.T) {
	for name, body := range map[string]string{
		"no employees":   `{"latest":"2026-09-30T18:05:00+00:00"}`,
		"key mismatch":   `{"employees":{"9":{"id":"10","action":"Updated","lastChanged":"2026-09-30T18:05:00+00:00"}}}`,
		"invalid id":     `{"employees":{"x9":{"action":"Updated","lastChanged":"2026-09-30T18:05:00+00:00"}}}`,
		"invalid time":   `{"employees":{"9":{"id":"9","action":"Updated","lastChanged":"yesterday"}}}`,
		"invalid action": `{"employees":{"9":{"id":"9","action":"SENTINEL text","lastChanged":"2026-09-30T18:05:00+00:00"}}}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, body)
			})
			result, err := sdkgo.RunQuery(newBambooHRDexContext("changes-invalid"), newBambooHRClient(t, provider.URL).ListEmployeeChanges(), bambooHRConnection,
				bamboohr.ListEmployeeChangesInput{Since: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC)})
			require.NoError(t, err)
			require.Equal(t, bamboohr.ListEmployeeChangesBranchInvalidResponse, result.Branch)
			requireNoLeak(t, result)
		})
	}
	provider := newRecordingBambooHR(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, input := range map[string]bamboohr.ListEmployeeChangesInput{
		"no since":     {},
		"bad cursor":   {Since: time.Now(), AfterEmployeeID: "EMP-1"},
		"bad type":     {Since: time.Now(), ChangeType: "all"},
		"limit 1001":   {Since: time.Now(), Limit: 1001},
		"negative cap": {Since: time.Now(), Limit: -1},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(newBambooHRDexContext("changes-defect"), newBambooHRClient(t, provider.URL).ListEmployeeChanges(), bambooHRConnection, input)
			require.NoError(t, err)
			require.Equal(t, bamboohr.ListEmployeeChangesBranchDefect, result.Branch)
		})
	}
}
