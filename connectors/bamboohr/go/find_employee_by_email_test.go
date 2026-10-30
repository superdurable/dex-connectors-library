// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package bamboohr_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/bamboohr"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// employeePage builds a List Employees page in BambooHR's documented shape.
func employeePage(total int, nextCursor string, employees ...string) string {
	cursor := "null"
	links := `{"self":{"href":"https://acme.bamboohr.com/api/v1/employees"}}`
	if nextCursor != "" {
		cursor = `"` + nextCursor + `"`
		links = `{"self":{"href":"https://acme.bamboohr.com/api/v1/employees"},"next":{"href":"https://acme.bamboohr.com/api/v1/employees?page%5Bafter%5D=` + nextCursor + `"}}`
	}
	return fmt.Sprintf(`{"data":[%s],"meta":{"total":%d,"page":{"limit":100,"nextCursor":%s,"prevCursor":null}},"_links":%s}`,
		strings.Join(employees, ","), total, cursor, links)
}

func employeeEntry(id string, workEmail string, homeEmail string) string {
	return fmt.Sprintf(`{"employeeId":%q,"firstName":"Ava","lastName":"Nguyen","preferredName":null,
		"photoUrl":"https://cdn.example.com/p/%s.jpg?signature=SENTINEL","jobTitleName":"Engineer","status":"Active",
		"workEmail":%q,"homeEmail":%q,"hireDate":"2026-10-13","_restrictedFields":[]}`, id, id, workEmail, homeEmail)
}

func TestFindEmployeeByEmailFiltersBambooHRSubstringMatchesToTheExactAddress(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, employeePage(3, "",
			employeeEntry("120", "ava.nguyen@acme.example.com.au", ""),
			employeeEntry("123", "Ava.Nguyen@acme.example.com", "ava@personal.example.com"),
			employeeEntry("130", "xava.nguyen@acme.example.com", ""),
		))
	})
	result, err := sdkgo.RunQuery(newBambooHRDexContext("find"), newBambooHRClient(t, provider.URL).FindEmployeeByEmail(), bambooHRConnection,
		bamboohr.FindEmployeeByEmailInput{Email: "ava.nguyen@acme.example.com", Status: bamboohr.EmployeeStatusFilterActive, Fields: []string{"hireDate", "workEmail"}})
	require.NoError(t, err)
	require.Equal(t, bamboohr.FindEmployeeByEmailBranchFound, result.Branch)
	require.Equal(t, bamboohr.EmployeeSummary{
		EmployeeID: "123", FirstName: "Ava", LastName: "Nguyen", JobTitleName: "Engineer", Status: "Active",
		WorkEmail: "Ava.Nguyen@acme.example.com", HomeEmail: "ava@personal.example.com", Fields: map[string]string{"hireDate": "2026-10-13"},
		RestrictedFields: []string{},
	}, result.Value.Employee, "letter case is ignored; look-alike addresses are not matches; the signed photo URL is dropped")
	require.Equal(t, 3, result.Value.CandidateCount)
	require.Equal(t, "123", result.Receipt.ProviderObjectID)
	request := provider.request(0)
	require.Equal(t, "/api/v1/employees", request.path)
	require.Equal(t, []string{"ava.nguyen@acme.example.com"}, request.query["filter[workEmail]"])
	require.Equal(t, []string{"active"}, request.query["filter[status]"])
	require.Equal(t, []string{"workEmail,homeEmail,hireDate"}, request.query["fields"])
	require.Equal(t, []string{"employeeId"}, request.query["sort"])
	require.Equal(t, []string{"100"}, request.query["page[limit]"])
	requireNoLeak(t, result)
}

func TestFindEmployeeByEmailMatchesTheHomeEmailOfANewHire(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, employeePage(1, "", employeeEntry("140", "", "ava@personal.example.com")))
	})
	result, err := sdkgo.RunQuery(newBambooHRDexContext("find-home"), newBambooHRClient(t, provider.URL).FindEmployeeByEmail(), bambooHRConnection,
		bamboohr.FindEmployeeByEmailInput{Email: "ava@personal.example.com", EmailField: bamboohr.EmployeeEmailFieldHome})
	require.NoError(t, err)
	require.Equal(t, bamboohr.FindEmployeeByEmailBranchFound, result.Branch)
	require.Equal(t, "140", result.Value.Employee.EmployeeID)
	require.Equal(t, []string{"ava@personal.example.com"}, provider.request(0).query["filter[homeEmail]"])
	require.NotContains(t, provider.request(0).query, "filter[workEmail]")
	require.NotContains(t, provider.request(0).query, "filter[status]")
}

func TestFindEmployeeByEmailSelectsNotFoundOrAmbiguous(t *testing.T) {
	for _, test := range []struct {
		name    string
		page    string
		branch  sdkgo.BranchID
		matches []string
	}{
		{name: "no candidates", page: employeePage(0, ""), branch: bamboohr.FindEmployeeByEmailBranchNotFound},
		{name: "only look-alikes", page: employeePage(1, "", employeeEntry("120", "ava.nguyen@acme.example.com.au", "")),
			branch: bamboohr.FindEmployeeByEmailBranchNotFound},
		{name: "two exact matches", page: employeePage(2, "",
			employeeEntry("123", "ava.nguyen@acme.example.com", ""), employeeEntry("124", "AVA.NGUYEN@acme.example.com", "")),
			branch: bamboohr.FindEmployeeByEmailBranchAmbiguous, matches: []string{"123", "124"}},
		{name: "more candidates than one page", page: employeePage(250, "eyJuZXh0In0=", employeeEntry("123", "ava.nguyen@acme.example.com", "")),
			branch: bamboohr.FindEmployeeByEmailBranchAmbiguous, matches: []string{"123"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, test.page)
			})
			result, err := sdkgo.RunQuery(newBambooHRDexContext("find-"+test.name), newBambooHRClient(t, provider.URL).FindEmployeeByEmail(), bambooHRConnection,
				bamboohr.FindEmployeeByEmailInput{Email: "ava.nguyen@acme.example.com"})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			var matchIDs []string
			for _, match := range result.Value.Matches {
				matchIDs = append(matchIDs, match.EmployeeID)
			}
			require.Equal(t, test.matches, matchIDs)
			require.Empty(t, result.Value.Employee.EmployeeID)
		})
	}
}

func TestFindEmployeeByEmailRejectsInvalidPagesAndInput(t *testing.T) {
	for name, body := range map[string]string{
		"no meta":          `{"data":[]}`,
		"directory shape":  `{"fields":[],"employees":[]}`,
		"invalid id":       employeePage(1, "", strings.Replace(employeeEntry("123", "a@b.example", ""), `"employeeId":"123"`, `"employeeId":"EMP-1"`, 1)),
		"total below page": employeePage(0, "", employeeEntry("123", "a@b.example", "")),
	} {
		t.Run(name, func(t *testing.T) {
			provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, http.StatusOK, body)
			})
			result, err := sdkgo.RunQuery(newBambooHRDexContext("find-invalid"), newBambooHRClient(t, provider.URL).FindEmployeeByEmail(), bambooHRConnection,
				bamboohr.FindEmployeeByEmailInput{Email: "a@b.example"})
			require.NoError(t, err)
			require.Equal(t, bamboohr.FindEmployeeByEmailBranchInvalidResponse, result.Branch)
		})
	}
	provider := newRecordingBambooHR(t, func(http.ResponseWriter, *http.Request, int) { t.Fatal("no request is expected") })
	for name, input := range map[string]bamboohr.FindEmployeeByEmailInput{
		"display address": {Email: "Ava <ava@acme.example.com>"},
		"blank":           {},
		"best email":      {Email: "ava@acme.example.com", EmailField: "bestEmail"},
		"status case":     {Email: "ava@acme.example.com", Status: "Active"},
		"field id":        {Email: "ava@acme.example.com", Fields: []string{"1349"}},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(newBambooHRDexContext("find-defect"), newBambooHRClient(t, provider.URL).FindEmployeeByEmail(), bambooHRConnection, input)
			require.NoError(t, err)
			require.Equal(t, bamboohr.FindEmployeeByEmailBranchDefect, result.Branch)
		})
	}
}

func TestFindEmployeeByEmailRepeatsOnlyBambooHRErrorCodes(t *testing.T) {
	provider := newRecordingBambooHR(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeBambooHRError(t, response, http.StatusUnprocessableEntity, `{"error":{"code":"BadRequest","message":"SENTINEL filter text"}}`)
	})
	result, err := sdkgo.RunQuery(newBambooHRDexContext("find-rejected"), newBambooHRClient(t, provider.URL).FindEmployeeByEmail(), bambooHRConnection,
		bamboohr.FindEmployeeByEmailInput{Email: "ava@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, bamboohr.FindEmployeeByEmailBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	require.Equal(t, "BambooHR rejected the request (HTTP 422) [BadRequest]", result.Failure.Message)
	requireNoLeak(t, result)
}
