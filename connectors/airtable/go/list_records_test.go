// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable_test

import (
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/airtable"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListRecordsPostsTypedFiltersFieldsSortAndOffset(t *testing.T) {
	provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"records":[
			{"id":"recPolicyStandard","createdTime":"2026-09-12T21:03:48.000Z","fields":{
				"Policy Key":"refund \"vip\"","Approval Limit USD":250.5,"Active":true,
				"Owners":["recOwner000000001","recOwner000000002"],"Tags":["priority","eu"]}},
			{"id":"recPolicyArchived","createdTime":"2026-09-13T08:00:00.000Z","fields":{}}
		],"offset":"itrNextPage/recPolicyArchived"}`)
	})
	client := newTestClient(t, provider.URL, airtable.Config{})
	result, err := sdkgo.RunQuery(newStepDexContext("list-policies"), client.ListRecords(), airtableConnection, airtable.ListRecordsInput{
		BaseID: testBaseID, TableIDOrName: testTableID,
		Filters: []airtable.FieldEqualityFilter{
			airtable.TextFieldEquals("Policy Key", `refund "vip"`),
			airtable.NumberFieldEquals("Approval Limit USD", 250.5),
			airtable.CheckboxFieldEquals("Active", true),
			airtable.CheckboxFieldEquals("fldArchived000001", false),
		},
		Formula:      `{Region} != "EU"`,
		Fields:       []string{"Policy Key", "Approval Limit USD", "Policy Key"},
		Sort:         []airtable.RecordSort{{Field: "Priority", Direction: airtable.SortDirectionDescending}},
		ViewIDOrName: "Grid view", PageSize: 2, Offset: "itrFirstPage/recPolicyStandard",
	})
	require.NoError(t, err)
	require.Equal(t, airtable.ListRecordsBranchListed, result.Branch)

	requests := provider.recordedRequests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].method)
	require.Equal(t, "/v0/"+testBaseID+"/"+testTableID+"/listRecords", requests[0].path)
	require.Empty(t, requests[0].query, "every parameter travels in the JSON body")
	require.Equal(t, "Bearer "+testAccessToken, requests[0].authorization)
	require.Equal(t, "application/json", requests[0].contentType)
	expectedFormula := `AND({Policy Key} = "refund \"vip\"", {Approval Limit USD} = 250.5, {Active} = TRUE(), NOT({fldArchived000001}), ({Region} != "EU"))`
	require.JSONEq(t, `{
		"pageSize":2,"offset":"itrFirstPage/recPolicyStandard",
		"filterByFormula":`+jsonString(t, expectedFormula)+`,
		"fields":["Policy Key","Approval Limit USD"],
		"sort":[{"field":"Priority","direction":"desc"}],
		"view":"Grid view"
	}`, string(requests[0].body))

	page := result.Value
	require.Equal(t, expectedFormula, page.FilterFormula)
	require.Equal(t, "itrNextPage/recPolicyArchived", page.Offset)
	require.Len(t, page.Records, 2)
	policy := page.Records[0]
	require.Equal(t, "recPolicyStandard", policy.ID)
	require.Equal(t, time.Date(2026, time.September, 12, 21, 3, 48, 0, time.UTC), policy.CreatedTime)
	key, isText := policy.Fields["Policy Key"].Text()
	require.True(t, isText)
	require.Equal(t, `refund "vip"`, key)
	limit, isNumber := policy.Fields["Approval Limit USD"].Number()
	require.True(t, isNumber)
	require.Equal(t, 250.5, limit)
	require.True(t, policy.Fields["Active"].Checkbox())
	owners, isList := policy.Fields["Owners"].StringList()
	require.True(t, isList)
	require.Equal(t, []string{"recOwner000000001", "recOwner000000002"}, owners)
	require.False(t, policy.Fields["Missing"].Checkbox(), "Airtable omits an unchecked box")
	require.NotNil(t, page.Records[1].Fields, "a record without non-empty fields has an empty field map")
	require.Equal(t, testBaseID+"/"+testTableID, result.Receipt.ProviderObjectID)
	require.Equal(t, "airtable", result.Receipt.Provider)
}

func TestListRecordsSendsOnlyTheDefaultPageSizeWithoutFilters(t *testing.T) {
	provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"records":[]}`)
	})
	client := newTestClient(t, provider.URL, airtable.Config{})
	result, err := sdkgo.RunQuery(newStepDexContext("list-all"), client.ListRecords(), airtableConnection, airtable.ListRecordsInput{
		BaseID: testBaseID, TableIDOrName: "Refund Policies / 2026",
	})
	require.NoError(t, err)
	require.Equal(t, airtable.ListRecordsBranchListed, result.Branch)
	require.NotNil(t, result.Value.Records, "an empty page is an empty list, not null")
	require.Empty(t, result.Value.Records)
	require.Empty(t, result.Value.Offset)
	require.Empty(t, result.Value.FilterFormula)
	request := provider.recordedRequests()[0]
	require.JSONEq(t, `{"pageSize":100}`, string(request.body))
	require.Equal(t, "/v0/"+testBaseID+"/Refund%20Policies%20%2F%202026/listRecords", request.rawPath, "a table name is one escaped path segment")
}

func TestListRecordsRejectsUnsafeInputWithoutARequest(t *testing.T) {
	provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"records":[]}`)
	})
	client := newTestClient(t, provider.URL, airtable.Config{})
	valid := airtable.ListRecordsInput{BaseID: testBaseID, TableIDOrName: testTableID}
	tooManyFilters := make([]airtable.FieldEqualityFilter, airtable.MaximumFieldEqualityFilters+1)
	for index := range tooManyFilters {
		tooManyFilters[index] = airtable.TextFieldEquals("Name", "value")
	}
	text := "value"
	for name, mutate := range map[string]func(*airtable.ListRecordsInput){
		"base ID without the app prefix": func(input *airtable.ListRecordsInput) { input.BaseID = "base00000000000001" },
		"blank table":                    func(input *airtable.ListRecordsInput) { input.TableIDOrName = "" },
		"field name with a brace": func(input *airtable.ListRecordsInput) {
			input.Filters = []airtable.FieldEqualityFilter{airtable.TextFieldEquals("Key}", "x")}
		},
		"text with a backslash": func(input *airtable.ListRecordsInput) {
			input.Filters = []airtable.FieldEqualityFilter{airtable.TextFieldEquals("Key", `a\"b`)}
		},
		"text with a line break": func(input *airtable.ListRecordsInput) {
			input.Filters = []airtable.FieldEqualityFilter{airtable.TextFieldEquals("Key", "a\nb")}
		},
		"filter with two values": func(input *airtable.ListRecordsInput) {
			filter := airtable.NumberFieldEquals("Key", 1)
			filter.Text = &text
			input.Filters = []airtable.FieldEqualityFilter{filter}
		},
		"filter without a value": func(input *airtable.ListRecordsInput) {
			input.Filters = []airtable.FieldEqualityFilter{{Field: "Key"}}
		},
		"non-finite number": func(input *airtable.ListRecordsInput) {
			input.Filters = []airtable.FieldEqualityFilter{airtable.NumberFieldEquals("Key", math.Inf(1))}
		},
		"too many filters": func(input *airtable.ListRecordsInput) { input.Filters = tooManyFilters },
		"oversized formula": func(input *airtable.ListRecordsInput) {
			input.Formula = strings.Repeat("1", airtable.MaximumFormulaBytes+1)
		},
		"page size above 100": func(input *airtable.ListRecordsInput) { input.PageSize = 101 },
		"negative page size":  func(input *airtable.ListRecordsInput) { input.PageSize = -1 },
		"unknown sort direction": func(input *airtable.ListRecordsInput) {
			input.Sort = []airtable.RecordSort{{Field: "Key", Direction: "up"}}
		},
		"control character view":  func(input *airtable.ListRecordsInput) { input.ViewIDOrName = "Grid\x00" },
		"blank listed field name": func(input *airtable.ListRecordsInput) { input.Fields = []string{""} },
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			result, err := sdkgo.RunQuery(newStepDexContext("list-invalid"), client.ListRecords(), airtableConnection, input)
			require.NoError(t, err)
			require.Equal(t, airtable.ListRecordsBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Empty(t, provider.recordedRequests(), "invalid input never reaches Airtable")
}

func TestListRecordsSelectsOffsetExpiredForAnExpiredIterator(t *testing.T) {
	provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusUnprocessableEntity, `{"error":{"type":"LIST_RECORDS_ITERATOR_NOT_AVAILABLE","message":"`+providerMessageSentinel+`"}}`)
	})
	client := newTestClient(t, provider.URL, airtable.Config{})
	result, err := sdkgo.RunQuery(newStepDexContext("list-expired"), client.ListRecords(), airtableConnection, airtable.ListRecordsInput{
		BaseID: testBaseID, TableIDOrName: testTableID, Offset: "itrStale/recPolicyStandard",
	})
	require.NoError(t, err)
	require.Equal(t, airtable.ListRecordsBranchOffsetExpired, result.Branch)
	requireSecretSafeFailure(t, result.Failure)
	require.Contains(t, result.Failure.Message, "LIST_RECORDS_ITERATOR_NOT_AVAILABLE")
}

func TestListRecordsClassifiesProviderErrorsWithoutMessageText(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		status      int
		body        string
		kind        sdkgo.FailureKind
		messagePart string
	}{
		{"rejected token", http.StatusUnauthorized, `{"error":{"type":"AUTHENTICATION_REQUIRED","message":"` + providerMessageSentinel + `"}}`, sdkgo.FailureAuthentication, "AUTHENTICATION_REQUIRED"},
		{"missing base access", http.StatusForbidden, `{"error":{"type":"INVALID_PERMISSIONS_OR_MODEL_NOT_FOUND","message":"` + providerMessageSentinel + `"}}`, sdkgo.FailureAuthorization, "INVALID_PERMISSIONS_OR_MODEL_NOT_FOUND"},
		{"unknown route", http.StatusNotFound, `{"error":"NOT_FOUND"}`, sdkgo.FailureNotFound, "NOT_FOUND"},
		{"invalid formula", http.StatusUnprocessableEntity, `{"error":{"type":"INVALID_FILTER_BY_FORMULA","message":"` + providerMessageSentinel + `"}}`, sdkgo.FailureValidation, "INVALID_FILTER_BY_FORMULA"},
		{"lowercase error text is not a type", http.StatusBadRequest, `{"error":"` + providerMessageSentinel + `"}`, sdkgo.FailureValidation, "HTTP 400"},
		{"redirect", http.StatusFound, ``, sdkgo.FailureProtocol, "HTTP 302"},
		{"not implemented", http.StatusNotImplemented, `{}`, sdkgo.FailureProviderRejection, "HTTP 501"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
				writeJSON(t, response, testCase.status, testCase.body)
			})
			client := newTestClient(t, provider.URL, airtable.Config{})
			result, err := sdkgo.RunQuery(newStepDexContext("list-error"), client.ListRecords(), airtableConnection, airtable.ListRecordsInput{
				BaseID: testBaseID, TableIDOrName: testTableID,
			})
			require.NoError(t, err)
			require.Equal(t, airtable.ListRecordsBranchProviderRejected, result.Branch)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, testCase.messagePart)
			requireSecretSafeFailure(t, result.Failure)
			require.Len(t, provider.recordedRequests(), 1)
		})
	}
}

func TestListRecordsRetriesRateLimitsOutagesAndDroppedConnections(t *testing.T) {
	t.Run("429 waits Airtable's 30-second cooldown", func(t *testing.T) {
		provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
			writeJSON(t, response, http.StatusTooManyRequests, `{"error":{"type":"RATE_LIMIT_REACHED","message":"`+providerMessageSentinel+`"}}`)
		})
		client := newTestClient(t, provider.URL, airtable.Config{})
		_, err := sdkgo.RunQuery(newStepDexContext("list-limited"), client.ListRecords(), airtableConnection, airtable.ListRecordsInput{BaseID: testBaseID, TableIDOrName: testTableID})
		delay, failure := requireRetry(t, err)
		require.Equal(t, 30*time.Second, delay)
		require.Equal(t, sdkgo.FailureRateLimit, failure.Kind)
		requireSecretSafeFailure(t, &failure)

		_, err = sdkgo.RunQuery(newStepDexContext("list-limited-again"), client.ListRecords(), airtableConnection, airtable.ListRecordsInput{BaseID: testBaseID, TableIDOrName: "Other table"})
		heldDelay, heldFailure := requireRetry(t, err)
		require.Greater(t, heldDelay, 25*time.Second, "later requests to the rate-limited base wait out the cooldown")
		require.Equal(t, sdkgo.FailureRateLimit, heldFailure.Kind)
		require.Len(t, provider.recordedRequests(), 1, "a held base sends nothing until the cooldown ends")
	})
	t.Run("503 honors Retry-After", func(t *testing.T) {
		provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
			response.Header().Set("Retry-After", "7")
			writeJSON(t, response, http.StatusServiceUnavailable, `{"error":{"type":"RETRIABLE_ERROR","message":"`+providerMessageSentinel+`"}}`)
		})
		client := newTestClient(t, provider.URL, airtable.Config{})
		_, err := sdkgo.RunQuery(newStepDexContext("list-unavailable"), client.ListRecords(), airtableConnection, airtable.ListRecordsInput{BaseID: testBaseID, TableIDOrName: testTableID})
		delay, failure := requireRetry(t, err)
		require.Equal(t, 7*time.Second, delay)
		require.Equal(t, sdkgo.FailureAvailability, failure.Kind)
		require.Contains(t, failure.Message, "RETRIABLE_ERROR")
	})
	t.Run("dropped connection", func(t *testing.T) {
		provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
			connection, _, err := response.(http.Hijacker).Hijack()
			require.NoError(t, err)
			require.NoError(t, connection.Close())
		})
		client := newTestClient(t, provider.URL, airtable.Config{})
		_, err := sdkgo.RunQuery(newStepDexContext("list-dropped"), client.ListRecords(), airtableConnection, airtable.ListRecordsInput{BaseID: testBaseID, TableIDOrName: testTableID})
		_, failure := requireRetry(t, err)
		require.Equal(t, sdkgo.FailureTransport, failure.Kind)
	})
}

func TestListRecordsSelectsInvalidResponseForUnusablePages(t *testing.T) {
	for _, testCase := range []struct {
		name string
		body string
	}{
		{"malformed JSON", `{"records":`},
		{"missing records", `{"offset":"itr1"}`},
		{"more records than the page size", `{"records":[` + strings.Repeat(`{"id":"recPolicyStandard","createdTime":"2026-09-12T21:03:48.000Z","fields":{}},`, 2) + `{"id":"recPolicyStandard","createdTime":"2026-09-12T21:03:48.000Z","fields":{}}]}`},
		{"record without a record ID", `{"records":[{"id":"policy-1","createdTime":"2026-09-12T21:03:48.000Z","fields":{}}]}`},
		{"record without a createdTime", `{"records":[{"id":"recPolicyStandard","fields":{}}]}`},
		{"oversized response", `{"records":[],"padding":"` + strings.Repeat("x", 2048) + `"}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
				writeJSON(t, response, http.StatusOK, testCase.body)
			})
			client := newTestClient(t, provider.URL, airtable.Config{MaxResponseBytes: 1024})
			result, err := sdkgo.RunQuery(newStepDexContext("list-invalid-response"), client.ListRecords(), airtableConnection, airtable.ListRecordsInput{
				BaseID: testBaseID, TableIDOrName: testTableID, PageSize: 2,
			})
			require.NoError(t, err)
			require.Equal(t, airtable.ListRecordsBranchInvalidResponse, result.Branch)
			requireSecretSafeFailure(t, result.Failure)
		})
	}
}

func TestRequestsToOneBaseAreSpacedToFivePerSecond(t *testing.T) {
	provider := newRecordingAirtable(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"records":[]}`)
	})
	client := newTestClient(t, provider.URL, airtable.Config{})
	started := time.Now()
	for range 6 {
		_, err := sdkgo.RunQuery(newStepDexContext("list-paced"), client.ListRecords(), airtableConnection, airtable.ListRecordsInput{BaseID: testBaseID, TableIDOrName: testTableID})
		require.NoError(t, err)
	}
	require.GreaterOrEqual(t, time.Since(started), 900*time.Millisecond, "six requests to one base span at least five 200 ms intervals")
	_, err := sdkgo.RunQuery(newStepDexContext("list-other-base"), client.ListRecords(), airtableConnection, airtable.ListRecordsInput{BaseID: "appOtherBase00001", TableIDOrName: testTableID})
	require.NoError(t, err, "another base has its own budget")
}

func jsonString(t *testing.T, value string) string {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	return string(encoded)
}
