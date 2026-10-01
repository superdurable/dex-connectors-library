// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUpdateEmployeeWritesOnlyChangedFieldsAndReadsThemBack(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, request *http.Request, index int) {
		switch index {
		case 0:
			writeJSON(t, response, http.StatusOK, `{"id":"123","mobilePhone":"801-555-0100","state":"UT","customITProvisioning":""}`)
		case 1:
			writeJSON(t, response, http.StatusOK, `{"id":"123","firstName":"Ava"}`)
		default:
			writeJSON(t, response, http.StatusOK, `{"id":"123","mobilePhone":"801-555-0100","state":"PA","customITProvisioning":"Requested for a 2026-10-13 start"}`)
		}
	})
	result, err := sdkgo.RunMutation(newBambooHRDexContext("update"), newBambooHRClient(t, provider.URL).UpdateEmployee(), bambooHRConnection, bamboohr.UpdateEmployeeInput{
		EmployeeID: "123",
		Fields: map[string]string{
			"mobilePhone": "801-555-0100", "state": "Pennsylvania", "customITProvisioning": "Requested for a 2026-10-13 start",
		},
	})
	require.NoError(t, err)
	require.Equal(t, bamboohr.UpdateEmployeeBranchUpdated, result.Branch)
	require.Equal(t, []string{"customITProvisioning", "state"}, result.Value.WrittenFields, "the phone already held its value")
	require.False(t, result.Value.WasAlreadyApplied)
	require.Equal(t, []string{"state"}, result.Value.UnmatchedFields, "BambooHR stores the state's abbreviation")
	require.Equal(t, "Requested for a 2026-10-13 start", result.Value.Employee.Fields["customITProvisioning"])
	require.Equal(t, "123", result.Receipt.ProviderObjectID)
	require.Equal(t, 3, provider.requestCount())
	for _, index := range []int{0, 2} {
		read := provider.request(index)
		require.Equal(t, http.MethodGet, read.method)
		require.Equal(t, "/api/v1/employees/123", read.path)
		require.Equal(t, []string{"customITProvisioning,mobilePhone,state"}, read.query["fields"])
	}
	write := provider.request(1)
	require.Equal(t, http.MethodPost, write.method)
	require.Equal(t, "/api/v1/employees/123", write.path)
	require.Equal(t, "application/json", write.header.Get("Content-Type"), "any other Content-Type makes BambooHR parse XML")
	require.Equal(t, expectedAuthorization(), write.header.Get("Authorization"))
	require.JSONEq(t, `{"state":"Pennsylvania","customITProvisioning":"Requested for a 2026-10-13 start"}`, write.body)
}

func TestUpdateEmployeeFindsValuesAlreadyAppliedAndWritesNothing(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"id":"123","customITProvisioning":"Requested"}`)
	})
	result, err := sdkgo.RunMutation(newBambooHRDexContext("update-applied"), newBambooHRClient(t, provider.URL).UpdateEmployee(), bambooHRConnection,
		bamboohr.UpdateEmployeeInput{EmployeeID: "123", Fields: map[string]string{"customITProvisioning": "Requested"}})
	require.NoError(t, err)
	require.Equal(t, bamboohr.UpdateEmployeeBranchUpdated, result.Branch)
	require.True(t, result.Value.WasAlreadyApplied)
	require.Empty(t, result.Value.WrittenFields)
	require.Equal(t, 1, provider.requestCount(), "an applied value is not rewritten, so the employee's last-changed time does not move")
}

func TestUpdateEmployeeRetriesEveryUnconfirmedWriteBecauseItSetsAbsoluteValues(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(http.ResponseWriter)
		kind  sdkgo.FailureKind
	}{
		{name: "server error", reply: func(response http.ResponseWriter) {
			writeBambooHRError(t, response, http.StatusInternalServerError, ``)
		}, kind: sdkgo.FailureAvailability},
		{name: "lost response", reply: func(response http.ResponseWriter) { dropConnection(t, response) }, kind: sdkgo.FailureTransport},
		{name: "rate limited", reply: func(response http.ResponseWriter) {
			response.Header().Set("Retry-After", "3")
			writeBambooHRError(t, response, http.StatusTooManyRequests, ``)
		}, kind: sdkgo.FailureRateLimit},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				if index == 0 {
					writeJSON(t, response, http.StatusOK, `{"id":"123","customITProvisioning":""}`)
					return
				}
				test.reply(response)
			})
			_, err := sdkgo.RunMutation(newBambooHRDexContext("update-"+test.name), newBambooHRClient(t, provider.URL).UpdateEmployee(), bambooHRConnection,
				bamboohr.UpdateEmployeeInput{EmployeeID: "123", Fields: map[string]string{"customITProvisioning": "Requested"}})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.kind, retry.Failure.Kind)
		})
	}
}

func TestUpdateEmployeeMapsRejectionsWithoutMessageText(t *testing.T) {
	for _, test := range []struct {
		name   string
		read   int
		write  int
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{name: "missing employee", read: http.StatusNotFound, branch: bamboohr.UpdateEmployeeBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "duplicate email", read: http.StatusOK, write: http.StatusConflict, branch: bamboohr.UpdateEmployeeBranchProviderRejected, kind: sdkgo.FailureConflict},
		{name: "not editable", read: http.StatusOK, write: http.StatusForbidden, branch: bamboohr.UpdateEmployeeBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{name: "read-only only", read: http.StatusOK, write: http.StatusBadRequest, branch: bamboohr.UpdateEmployeeBranchProviderRejected, kind: sdkgo.FailureValidation},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, index int) {
				if index == 0 && test.read == http.StatusOK {
					writeJSON(t, response, http.StatusOK, `{"id":"123","workEmail":""}`)
					return
				}
				status := test.read
				if index == 1 {
					status = test.write
				}
				writeBambooHRError(t, response, status, `SENTINEL`)
			})
			result, err := sdkgo.RunMutation(newBambooHRDexContext("update-"+test.name), newBambooHRClient(t, provider.URL).UpdateEmployee(), bambooHRConnection,
				bamboohr.UpdateEmployeeInput{EmployeeID: "123", Fields: map[string]string{"workEmail": "ava.nguyen@acme.example.com"}})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			requireNoLeak(t, result)
		})
	}
}

func TestUpdateEmployeeRefusesHistoryTableAndIdentityFields(t *testing.T) {
	provider := newRecordingBambooHR(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, fields := range map[string]map[string]string{
		"job title":         {"jobTitle": "Engineer"},
		"department":        {"Department": "Engineering"},
		"employment status": {"employmentHistoryStatus": "Terminated"},
		"termination date":  {"terminationDate": "2026-10-31"},
		"pay rate":          {"payRate": "100000"},
		"numeric field id":  {"17": "Engineer"},
		"photo":             {"photoUrl": "https://example.com/p.jpg"},
		"no fields":         {},
		"nul byte":          {"customNote": "a\x00b"},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunMutation(newBambooHRDexContext("update-refused"), newBambooHRClient(t, provider.URL).UpdateEmployee(), bambooHRConnection,
				bamboohr.UpdateEmployeeInput{EmployeeID: "123", Fields: fields})
			require.NoError(t, err)
			require.Equal(t, bamboohr.UpdateEmployeeBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
}
