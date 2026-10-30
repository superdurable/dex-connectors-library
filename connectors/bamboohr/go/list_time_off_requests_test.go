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

const timeOffPageBody = `{"data":[{"id":5521,"employeeId":123,"categoryId":78,"startDate":"2026-10-20","endDate":"2026-10-21",
	"amount":2,"unit":"DAYS","status":"APPROVED","requestedAt":"2026-09-02T15:04:05Z",
	"dailyAmounts":[{"date":"2026-10-20","amount":1},{"date":"2026-10-21","amount":1}],
	"employeeNote":"SENTINEL medical appointment","managerNote":"SENTINEL ok","statusUpdatedAt":"2026-09-03T08:00:00Z","statusUpdatedBy":7}],
	"meta":{"page":1,"pageSize":50,"totalPages":2,"totalItems":51},"_links":{"next":{"href":"https://acme.bamboohr.com/api/v1/time-off/requests?page=2"}}}`

func TestListTimeOffRequestsSendsAnOverlapFilterAndDropsNotes(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, timeOffPageBody)
	})
	result, err := sdkgo.RunQuery(newBambooHRDexContext("time-off"), newBambooHRClient(t, provider.URL).ListTimeOffRequests(), bambooHRConnection, bamboohr.ListTimeOffRequestsInput{
		StartDate: "2026-10-13", EndDate: "2026-10-26", EmployeeID: "123",
		Statuses: []bamboohr.TimeOffRequestStatus{bamboohr.TimeOffRequestStatusRequested, bamboohr.TimeOffRequestStatusApproved}, PageSize: 50,
	})
	require.NoError(t, err)
	require.Equal(t, bamboohr.ListTimeOffRequestsBranchListed, result.Branch)
	statusUpdatedAt := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	require.Equal(t, bamboohr.ListTimeOffRequestsOutput{
		Requests: []bamboohr.TimeOffRequest{{
			ID: 5521, EmployeeID: "123", CategoryID: 78, StartDate: "2026-10-20", EndDate: "2026-10-21", Amount: 2,
			Unit: bamboohr.TimeOffUnitDays, Status: bamboohr.TimeOffRequestStatusApproved,
			RequestedAt: time.Date(2026, 9, 2, 15, 4, 5, 0, time.UTC), StatusUpdatedAt: &statusUpdatedAt,
			DailyAmounts: []bamboohr.TimeOffDailyAmount{{Date: "2026-10-20", Amount: 1}, {Date: "2026-10-21", Amount: 1}},
		}},
		Page: 1, TotalItems: 51, NextPage: 2,
	}, result.Value)
	requireNoLeak(t, result)
	request := provider.request(0)
	require.Equal(t, "/api/v1/time-off/requests", request.path)
	require.Equal(t, []string{"startDate le '2026-10-26' and endDate ge '2026-10-13' and employeeId eq 123 and status in ('requested', 'approved')"},
		request.query["filter"])
	require.Equal(t, []string{"1"}, request.query["page"])
	require.Equal(t, []string{"50"}, request.query["pageSize"])
}

func TestBuildTimeOffRequestFilterWritesOneStatusAsEquality(t *testing.T) {
	require.Equal(t, "startDate le '2026-10-01' and endDate ge '2026-10-01' and status eq 'approved'",
		bamboohr.BuildTimeOffRequestFilter(bamboohr.ListTimeOffRequestsInput{
			StartDate: "2026-10-01", EndDate: "2026-10-01", Statuses: []bamboohr.TimeOffRequestStatus{bamboohr.TimeOffRequestStatusApproved},
		}))
	require.Equal(t, "startDate le '2026-12-31' and endDate ge '2026-10-01'",
		bamboohr.BuildTimeOffRequestFilter(bamboohr.ListTimeOffRequestsInput{StartDate: "2026-10-01", EndDate: "2026-12-31"}))
}

func TestListTimeOffRequestsReadsTheLastPage(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"data":[],"meta":{"page":3,"pageSize":100,"totalPages":3,"totalItems":201},"_links":{}}`)
	})
	result, err := sdkgo.RunQuery(newBambooHRDexContext("time-off-last"), newBambooHRClient(t, provider.URL).ListTimeOffRequests(), bambooHRConnection,
		bamboohr.ListTimeOffRequestsInput{StartDate: "2026-10-01", EndDate: "2026-12-31", Page: 3})
	require.NoError(t, err)
	require.Equal(t, bamboohr.ListTimeOffRequestsOutput{Requests: []bamboohr.TimeOffRequest{}, Page: 3, TotalItems: 201}, result.Value)
	require.Equal(t, []string{"100"}, provider.request(0).query["pageSize"])
}

func TestListTimeOffRequestsRejectsInvalidPagesAndInput(t *testing.T) {
	for name, body := range map[string]string{
		"legacy array":   `[{"id":"5521","employeeId":"123"}]`,
		"unknown status": `{"data":[{"id":1,"employeeId":123,"startDate":"2026-10-20","endDate":"2026-10-20","unit":"DAYS","status":"SUPERCEDED"}],"meta":{"page":1,"totalPages":1,"totalItems":1}}`,
		"bad date":       `{"data":[{"id":1,"employeeId":123,"startDate":"10/20/2026","endDate":"2026-10-20","unit":"DAYS","status":"APPROVED"}],"meta":{"page":1,"totalPages":1,"totalItems":1}}`,
		"no employee":    `{"data":[{"id":1,"startDate":"2026-10-20","endDate":"2026-10-20","unit":"DAYS","status":"APPROVED"}],"meta":{"page":1,"totalPages":1,"totalItems":1}}`,
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, body)
			})
			result, err := sdkgo.RunQuery(newBambooHRDexContext("time-off-invalid"), newBambooHRClient(t, provider.URL).ListTimeOffRequests(), bambooHRConnection,
				bamboohr.ListTimeOffRequestsInput{StartDate: "2026-10-01", EndDate: "2026-10-31"})
			require.NoError(t, err)
			require.Equal(t, bamboohr.ListTimeOffRequestsBranchInvalidResponse, result.Branch)
		})
	}
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeBambooHRError(t, response, http.StatusUnprocessableEntity, `{"type":"about:blank","title":"SENTINEL","detail":"SENTINEL","code":"invalid_filter"}`)
	})
	result, err := sdkgo.RunQuery(newBambooHRDexContext("time-off-rejected"), newBambooHRClient(t, provider.URL).ListTimeOffRequests(), bambooHRConnection,
		bamboohr.ListTimeOffRequestsInput{StartDate: "2026-10-01", EndDate: "2026-10-31"})
	require.NoError(t, err)
	require.Equal(t, bamboohr.ListTimeOffRequestsBranchProviderRejected, result.Branch)
	require.Equal(t, "BambooHR rejected the request (HTTP 422) [invalid_filter]", result.Failure.Message)

	for name, input := range map[string]bamboohr.ListTimeOffRequestsInput{
		"no window":      {},
		"reversed":       {StartDate: "2026-10-31", EndDate: "2026-10-01"},
		"quote in date":  {StartDate: "2026-10-01' or 1 eq 1", EndDate: "2026-10-31"},
		"bad employee":   {StartDate: "2026-10-01", EndDate: "2026-10-31", EmployeeID: "123 or employeeId ne 0"},
		"legacy status":  {StartDate: "2026-10-01", EndDate: "2026-10-31", Statuses: []bamboohr.TimeOffRequestStatus{"superceded"}},
		"page size 201":  {StartDate: "2026-10-01", EndDate: "2026-10-31", PageSize: 201},
		"negative page":  {StartDate: "2026-10-01", EndDate: "2026-10-31", Page: -1},
		"impossible day": {StartDate: "2026-02-30", EndDate: "2026-03-01"},
	} {
		t.Run(name, func(t *testing.T) {
			defect, err := sdkgo.RunQuery(newBambooHRDexContext("time-off-defect"), newBambooHRClient(t, closedLoopbackURL(t)).ListTimeOffRequests(), bambooHRConnection, input)
			require.NoError(t, err)
			require.Equal(t, bamboohr.ListTimeOffRequestsBranchDefect, defect.Branch)
		})
	}
}
