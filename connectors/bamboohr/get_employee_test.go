// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr_test

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestGetEmployeeRequestsCommaSeparatedFieldsAndReportsOmittedOnes(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"id":"123","firstName":"Ava","workEmail":null,"canUploadPhoto":false,
			"customStartDate":"2026-10-13","teams":[{"id":1,"label":"Platform"}]}`)
	})
	result, err := sdkgo.RunQuery(newBambooHRDexContext("get"), newBambooHRClient(t, provider.URL).GetEmployee(), bambooHRConnection, bamboohr.GetEmployeeInput{
		EmployeeID: "123", Fields: []string{"firstName", "workEmail", "canUploadPhoto", "customStartDate", "teams", "ssn"},
	})
	require.NoError(t, err)
	require.Equal(t, bamboohr.GetEmployeeBranchFound, result.Branch)
	require.Equal(t, bamboohr.EmployeeRecord{
		EmployeeID: "123",
		Fields: map[string]string{
			"firstName": "Ava", "workEmail": "", "canUploadPhoto": "false", "customStartDate": "2026-10-13",
			"teams": `[{"id":1,"label":"Platform"}]`,
		},
		OmittedFields: []string{"ssn"},
	}, result.Value, "ssn was not visible to the connection, so BambooHR omitted it")
	require.Equal(t, "123", result.Receipt.ProviderObjectID)
	request := provider.request(0)
	require.Equal(t, http.MethodGet, request.method)
	require.Equal(t, "/api/v1/employees/123", request.path)
	require.Equal(t, []string{"firstName,workEmail,canUploadPhoto,customStartDate,teams,ssn"}, request.query["fields"])
	require.NotContains(t, request.query, "onlyCurrent")
	require.Equal(t, expectedAuthorization(), request.header.Get("Authorization"))
	require.Equal(t, "application/json", request.header.Get("Accept"))
}

func TestGetEmployeeCanIncludeFutureDatedValues(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"id":"123","jobTitle":"Engineer"}`)
	})
	_, err := sdkgo.RunQuery(newBambooHRDexContext("get-future"), newBambooHRClient(t, provider.URL).GetEmployee(), bambooHRConnection, bamboohr.GetEmployeeInput{
		EmployeeID: "123", Fields: []string{"jobTitle"}, IncludeFutureValues: true,
	})
	require.NoError(t, err)
	require.Equal(t, []string{"false"}, provider.request(0).query["onlyCurrent"])
}

func TestGetEmployeeMapsBambooHRStatusesWithoutMessageText(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{name: "missing employee", status: http.StatusNotFound, branch: bamboohr.GetEmployeeBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "not permitted", status: http.StatusForbidden, body: `Insufficient Permissions to view this employee SENTINEL`,
			branch: bamboohr.GetEmployeeBranchProviderRejected, kind: sdkgo.FailureAuthorization},
		{name: "missing key", status: http.StatusUnauthorized, branch: bamboohr.GetEmployeeBranchProviderRejected, kind: sdkgo.FailureAuthentication},
		{name: "too many fields", status: http.StatusBadRequest, body: `{"error":{"code":"BadRequest","message":"SENTINEL"}}`,
			branch: bamboohr.GetEmployeeBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "unknown field", status: http.StatusNotAcceptable, branch: bamboohr.GetEmployeeBranchProviderRejected, kind: sdkgo.FailureValidation},
		{name: "redirect", status: http.StatusFound, branch: bamboohr.GetEmployeeBranchProviderRejected, kind: sdkgo.FailureProtocol},
		{name: "another employee", status: http.StatusOK, body: `{"id":"124","firstName":"Sam"}`,
			branch: bamboohr.GetEmployeeBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "not an object", status: http.StatusOK, body: `["SENTINEL"]`, branch: bamboohr.GetEmployeeBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "html page", status: http.StatusOK, body: `<html>SENTINEL</html>`, branch: bamboohr.GetEmployeeBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "reflected key", status: http.StatusOK, body: `{"id":"123","firstName":"` + testAPIKey + `"}`,
			branch: bamboohr.GetEmployeeBranchInvalidResponse, kind: sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				if test.status == http.StatusFound {
					response.Header().Set("Location", "https://acme.bamboohr.com/login.php")
				}
				writeBambooHRError(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunQuery(newBambooHRDexContext("get-"+test.name), newBambooHRClient(t, provider.URL).GetEmployee(), bambooHRConnection,
				bamboohr.GetEmployeeInput{EmployeeID: "123", Fields: []string{"firstName"}})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.Equal(t, 1, provider.requestCount(), "redirects are never followed")
			requireNoLeak(t, result)
		})
	}
}

func TestGetEmployeeRetriesRateLimitsAndOutages(t *testing.T) {
	for _, test := range []struct {
		name       string
		reply      func(http.ResponseWriter)
		kind       sdkgo.FailureKind
		retryAfter time.Duration
	}{
		{name: "rate limited", reply: func(response http.ResponseWriter) {
			response.Header().Set("Retry-After", "17")
			writeBambooHRError(t, response, http.StatusTooManyRequests, ``)
		}, kind: sdkgo.FailureRateLimit, retryAfter: 17 * time.Second},
		{name: "unavailable", reply: func(response http.ResponseWriter) {
			writeBambooHRError(t, response, http.StatusServiceUnavailable, ``)
		}, kind: sdkgo.FailureAvailability},
		{name: "lost response", reply: func(response http.ResponseWriter) { dropConnection(t, response) }, kind: sdkgo.FailureTransport},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(response) })
			_, err := sdkgo.RunQuery(newBambooHRDexContext("get-retry-"+test.name), newBambooHRClient(t, provider.URL).GetEmployee(), bambooHRConnection,
				bamboohr.GetEmployeeInput{EmployeeID: "123", Fields: []string{"firstName"}})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.kind, retry.Failure.Kind)
			require.NotContains(t, err.Error(), "SENTINEL")
			if test.retryAfter > 0 {
				var retryAfter *dex.RetryAfterError
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, test.retryAfter, retryAfter.After)
			}
		})
	}
}

func TestGetEmployeeRejectsUnusableInputWithoutARequest(t *testing.T) {
	provider := newRecordingBambooHR(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	tooMany := make([]string, bamboohr.MaxRequestedFields+1)
	for index := range tooMany {
		tooMany[index] = "field" + strconv.Itoa(index)
	}
	for name, input := range map[string]bamboohr.GetEmployeeInput{
		"caller sentinel":   {EmployeeID: "0", Fields: []string{"firstName"}},
		"employee number":   {EmployeeID: "EMP-7715", Fields: []string{"firstName"}},
		"no fields":         {EmployeeID: "123"},
		"field with comma":  {EmployeeID: "123", Fields: []string{"firstName,ssn"}},
		"duplicate field":   {EmployeeID: "123", Fields: []string{"firstName", "firstName"}},
		"more than 400":     {EmployeeID: "123", Fields: tooMany},
		"path in the field": {EmployeeID: "123", Fields: []string{"../tables"}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(newBambooHRDexContext("get-invalid"), newBambooHRClient(t, provider.URL).GetEmployee(), bambooHRConnection, input)
			require.NoError(t, err)
			require.Equal(t, bamboohr.GetEmployeeBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
}
