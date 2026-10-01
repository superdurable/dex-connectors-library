// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr_test

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

func validAddEmployeeInput() bamboohr.AddEmployeeInput {
	return bamboohr.AddEmployeeInput{
		FirstName: "Ava", LastName: "Nguyen", HomeEmail: "ava@personal.example.com", HireDate: "2026-10-13",
		Fields: map[string]string{"department": "Engineering", "employmentHistoryStatus": "Full-Time"},
	}
}

func TestAddEmployeeSendsOneRequestAfterRecordingTheDispatchMarker(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("Location", "https://acme.bamboohr.com/employees/employee.php?id=140")
		writeJSON(t, response, http.StatusCreated, `{"id":"140","firstName":"Ava","lastName":"Nguyen"}`)
	})
	ctx := newBambooHRDexContext("add")
	result, err := sdkgo.RunMutation(ctx, newBambooHRClient(t, provider.URL).AddEmployee(), bambooHRConnection, validAddEmployeeInput())
	require.NoError(t, err)
	require.Equal(t, bamboohr.AddEmployeeBranchCreated, result.Branch)
	require.Equal(t, bamboohr.AddEmployeeOutput{EmployeeID: "140"}, result.Value)
	require.Equal(t, "140", result.Receipt.ProviderObjectID)
	require.Equal(t, 1, provider.requestCount())
	request := provider.request(0)
	require.Equal(t, http.MethodPost, request.method)
	require.Equal(t, "/api/v1/employees", request.path)
	require.Equal(t, "application/json", request.header.Get("Content-Type"))
	require.Empty(t, request.header.Get("Idempotency-Key"), "BambooHR documents no key, and Go would retry a keyed POST on its own")
	require.JSONEq(t, `{"firstName":"Ava","lastName":"Nguyen","homeEmail":"ava@personal.example.com","hireDate":"2026-10-13",
		"department":"Engineering","employmentHistoryStatus":"Full-Time"}`, request.body)
	require.JSONEq(t, `{"bamboohrDispatchedCallId":"`+string(result.Receipt.CallID)+`"}`, string(ctx.recordedHeartbeat),
		"the dispatch marker is recorded before the request")
}

func TestAddEmployeeReadsTheIDFromTheLocationHeaderWhenTheBodyLacksIt(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("Location", "https://acme.bamboohr.com/api/v1/employees/141")
		writeJSON(t, response, http.StatusCreated, ``)
	})
	result, err := sdkgo.RunMutation(newBambooHRDexContext("add-location"), newBambooHRClient(t, provider.URL).AddEmployee(), bambooHRConnection,
		bamboohr.AddEmployeeInput{FirstName: "Sam", LastName: "Park"})
	require.NoError(t, err)
	require.Equal(t, bamboohr.AddEmployeeBranchCreated, result.Branch)
	require.Equal(t, "141", result.Value.EmployeeID)
	require.JSONEq(t, `{"firstName":"Sam","lastName":"Park"}`, provider.request(0).body)
}

func TestAddEmployeeNeverResendsARequestWhoseOutcomeIsUnknown(t *testing.T) {
	for _, test := range []struct {
		name   string
		reply  func(http.ResponseWriter)
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
	}{
		{name: "server error", reply: func(response http.ResponseWriter) {
			writeBambooHRError(t, response, http.StatusInternalServerError, `SENTINEL`)
		}, branch: bamboohr.AddEmployeeBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "unavailable", reply: func(response http.ResponseWriter) {
			response.Header().Set("Retry-After", "5")
			writeBambooHRError(t, response, http.StatusServiceUnavailable, ``)
		}, branch: bamboohr.AddEmployeeBranchUncertain, kind: sdkgo.FailureAvailability},
		{name: "lost response", reply: func(response http.ResponseWriter) { dropConnection(t, response) },
			branch: bamboohr.AddEmployeeBranchUncertain, kind: sdkgo.FailureTransport},
		{name: "no employee id", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusCreated, `{"firstName":"SENTINEL"}`)
		}, branch: bamboohr.AddEmployeeBranchUncertain, kind: sdkgo.FailureProtocol},
		{name: "reflected key", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusCreated, `{"id":"140","firstName":"`+testAPIKey+`"}`)
		}, branch: bamboohr.AddEmployeeBranchUncertain, kind: sdkgo.FailureProtocol},
		{name: "duplicate email", reply: func(response http.ResponseWriter) {
			writeBambooHRError(t, response, http.StatusConflict, ``)
		}, branch: bamboohr.AddEmployeeBranchProviderRejected, kind: sdkgo.FailureConflict},
		{name: "employee limit", reply: func(response http.ResponseWriter) {
			writeBambooHRError(t, response, http.StatusTooManyRequests, ``)
		}, branch: bamboohr.AddEmployeeBranchProviderRejected, kind: sdkgo.FailureQuotaExhausted},
		{name: "not permitted", reply: func(response http.ResponseWriter) {
			writeBambooHRError(t, response, http.StatusForbidden, ``)
		}, branch: bamboohr.AddEmployeeBranchProviderRejected, kind: sdkgo.FailureAuthorization},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) { test.reply(response) })
			ctx := newBambooHRDexContext("add-" + test.name)
			result, err := sdkgo.RunMutation(ctx, newBambooHRClient(t, provider.URL).AddEmployee(), bambooHRConnection, validAddEmployeeInput())
			require.NoError(t, err, "only a provable non-application is retried")
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			requireNoLeak(t, result)
			require.NotEmpty(t, ctx.recordedHeartbeat, "the marker stays, so a replayed attempt sends nothing")
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestAddEmployeeRetriesOnlyWhatBambooHRProvablyDidNotApply(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		response.Header().Set("Retry-After", "34")
		writeBambooHRError(t, response, http.StatusTooManyRequests, ``)
	})
	ctx := newBambooHRDexContext("add-rate-limited")
	_, err := sdkgo.RunMutation(ctx, newBambooHRClient(t, provider.URL).AddEmployee(), bambooHRConnection, validAddEmployeeInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 34*time.Second, retryAfter.After)
	require.Nil(t, ctx.recordedHeartbeat, "a rate-limited 429 clears the marker so the retry may send")
	require.Equal(t, 2, ctx.heartbeatCount)

	refused := newBambooHRDexContext("add-refused")
	_, err = sdkgo.RunMutation(refused, newBambooHRClient(t, closedLoopbackURL(t)).AddEmployee(), bambooHRConnection, validAddEmployeeInput())
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
	require.Nil(t, refused.recordedHeartbeat, "a refused connection sent nothing")

	next := ctx.nextAttempt()
	created := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusCreated, `{"id":"142"}`)
	})
	result, err := sdkgo.RunMutation(next, newBambooHRClient(t, created.URL).AddEmployee(), bambooHRConnection, validAddEmployeeInput())
	require.NoError(t, err)
	require.Equal(t, bamboohr.AddEmployeeBranchCreated, result.Branch)
}

func TestAddEmployeeAfterAnEarlierDispatchSendsNothing(t *testing.T) {
	provider := newRecordingBambooHR(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	first := newBambooHRDexContext("add-replay")
	first.recordedHeartbeat = json.RawMessage(`{"bamboohrDispatchedCallId":"earlier"}`)
	result, err := sdkgo.RunMutation(first.nextAttempt(), newBambooHRClient(t, provider.URL).AddEmployee(), bambooHRConnection, validAddEmployeeInput())
	require.NoError(t, err)
	require.Equal(t, bamboohr.AddEmployeeBranchUncertain, result.Branch)
	require.Equal(t, "an earlier attempt of this Step may have sent the request, so it is not sent again", result.Failure.Message)

	rejecting := newBambooHRDexContext("add-heartbeat-closed")
	rejecting.rejectsHeartbeat = true
	_, err = sdkgo.RunMutation(rejecting, newBambooHRClient(t, provider.URL).AddEmployee(), bambooHRConnection, validAddEmployeeInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "nothing is sent until Dex records the marker")
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
}

func TestAddEmployeeRejectsUnusableInputWithoutARequest(t *testing.T) {
	provider := newRecordingBambooHR(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, change := range map[string]func(*bamboohr.AddEmployeeInput){
		"no first name":        func(input *bamboohr.AddEmployeeInput) { input.FirstName = "" },
		"padded last name":     func(input *bamboohr.AddEmployeeInput) { input.LastName = " Nguyen" },
		"display work email":   func(input *bamboohr.AddEmployeeInput) { input.WorkEmail = "Ava <ava@acme.example.com>" },
		"hire date format":     func(input *bamboohr.AddEmployeeInput) { input.HireDate = "10/13/2026" },
		"typed field repeated": func(input *bamboohr.AddEmployeeInput) { input.Fields = map[string]string{"firstName": "Eve"} },
		"numeric field id":     func(input *bamboohr.AddEmployeeInput) { input.Fields = map[string]string{"17": "Engineer"} },
		"photo":                func(input *bamboohr.AddEmployeeInput) { input.Fields = map[string]string{"photo": "x"} },
		"control character":    func(input *bamboohr.AddEmployeeInput) { input.FirstName = "Ava\nNguyen" },
	} {
		t.Run(name, func(t *testing.T) {
			input := validAddEmployeeInput()
			change(&input)
			ctx := newBambooHRDexContext("add-invalid")
			result, err := sdkgo.RunMutation(ctx, newBambooHRClient(t, provider.URL).AddEmployee(), bambooHRConnection, input)
			require.NoError(t, err)
			require.Equal(t, bamboohr.AddEmployeeBranchDefect, result.Branch)
			require.Nil(t, ctx.recordedHeartbeat, "invalid input never claims the dispatch")
		})
	}
}
