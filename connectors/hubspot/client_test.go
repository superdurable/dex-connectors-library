// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hubspot_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/hubspot"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestSearchObjectsSendsTypedFiltersAndReturnsOnePage(t *testing.T) {
	provider := newRecordingHubSpot(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"total":3,"results":[
			{"id":"9001","properties":{"dealname":"Acme renewal","dealstage":"appointmentscheduled","amount":null,"hs_object_id":"9001"},
			 "createdAt":"2026-01-17T19:55:04.281Z","updatedAt":"2026-09-11T13:27:39.356Z","archived":false,
			 "url":"https://app.hubspot.com/contacts/123/record/0-3/9001"},
			{"id":"9002","properties":{"dealname":"Acme expansion"},"archived":false,"url":"javascript:alert(1)"}
		],"paging":{"next":{"after":"2"}}}`)
	})
	client := newStaticTokenClient(t, provider.URL, hubspot.Config{})
	result, err := sdkgo.RunQuery(newStepDexContext("search-deals"), client.SearchObjects(), hubspotConnection, hubspot.SearchObjectsInput{
		ObjectType: hubspot.ObjectTypeDeals,
		FilterGroups: []hubspot.SearchFilterGroup{{Filters: []hubspot.SearchFilter{
			{PropertyName: "associations.contact", Operator: hubspot.FilterOperatorEqual, Value: "501"},
			{PropertyName: "amount", Operator: hubspot.FilterOperatorBetween, Value: "1000", HighValue: "5000"},
		}}, {Filters: []hubspot.SearchFilter{
			{PropertyName: "dealstage", Operator: hubspot.FilterOperatorIn, Values: []string{"appointmentscheduled", "qualifiedtobuy"}},
		}}},
		Sort:       &hubspot.SearchSort{PropertyName: "hs_lastmodifieddate", Direction: hubspot.SortDirectionDescending},
		Properties: []string{"dealname", "dealstage", "dealname"},
		Limit:      2,
	})
	require.NoError(t, err)
	require.Equal(t, hubspot.SearchObjectsBranchSearched, result.Branch)
	requests := provider.recordedRequests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].method)
	require.Equal(t, "/crm/objects/2026-09/deals/search", requests[0].path)
	require.Equal(t, "Bearer "+testAccessToken, requests[0].authorization)
	require.Equal(t, "application/json", requests[0].contentType)
	require.JSONEq(t, `{
		"filterGroups":[
			{"filters":[{"propertyName":"associations.contact","operator":"EQ","value":"501"},
			            {"propertyName":"amount","operator":"BETWEEN","value":"1000","highValue":"5000"}]},
			{"filters":[{"propertyName":"dealstage","operator":"IN","values":["appointmentscheduled","qualifiedtobuy"]}]}
		],
		"sorts":[{"propertyName":"hs_lastmodifieddate","direction":"DESCENDING"}],
		"properties":["dealname","dealstage"],
		"limit":2
	}`, string(requests[0].body))

	page := result.Value
	require.Equal(t, hubspot.ObjectTypeDeals, page.ObjectType)
	require.Equal(t, 3, page.Total)
	require.Equal(t, "2", page.NextAfter)
	require.Len(t, page.Objects, 2)
	require.Equal(t, "9001", page.Objects[0].ID)
	require.Equal(t, map[string]string{"dealname": "Acme renewal", "dealstage": "appointmentscheduled", "hs_object_id": "9001"}, page.Objects[0].Properties)
	require.Equal(t, time.Date(2026, time.January, 17, 19, 55, 4, 281000000, time.UTC), page.Objects[0].CreatedAt)
	require.Equal(t, "https://app.hubspot.com/contacts/123/record/0-3/9001", page.Objects[0].URL)
	require.Empty(t, page.Objects[1].URL, "only HTTPS HubSpot app links are kept")
	require.Equal(t, "c033cdaa-2c40-4a64-ae48-b4cec88dad24", result.Receipt.ProviderRequestID)
}

func TestSearchObjectsSendsDefaultLimitAndCursor(t *testing.T) {
	provider := newRecordingHubSpot(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"total":0,"results":[]}`)
	})
	client := newStaticTokenClient(t, provider.URL, hubspot.Config{})
	result, err := sdkgo.RunQuery(newStepDexContext("search-contacts"), client.SearchObjects(), hubspotConnection, hubspot.SearchObjectsInput{
		ObjectType: hubspot.ObjectTypeContacts, Query: "acme", After: "20",
	})
	require.NoError(t, err)
	require.Equal(t, hubspot.SearchObjectsBranchSearched, result.Branch)
	require.Empty(t, result.Value.Objects)
	require.NotNil(t, result.Value.Objects, "an empty page is an empty list, not null")
	require.Empty(t, result.Value.NextAfter)
	require.JSONEq(t, `{"query":"acme","limit":10,"after":"20"}`, string(provider.recordedRequests()[0].body))
}

func TestSearchObjectsRejectsInvalidInputWithoutCallingHubSpot(t *testing.T) {
	provider := newRecordingHubSpot(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"total":0,"results":[]}`)
	})
	client := newStaticTokenClient(t, provider.URL, hubspot.Config{})
	oneFilter := func(filter hubspot.SearchFilter) []hubspot.SearchFilterGroup {
		return []hubspot.SearchFilterGroup{{Filters: []hubspot.SearchFilter{filter}}}
	}
	manyGroups := make([]hubspot.SearchFilterGroup, hubspot.MaximumFilterGroups+1)
	for index := range manyGroups {
		manyGroups[index] = hubspot.SearchFilterGroup{Filters: []hubspot.SearchFilter{{PropertyName: "email", Operator: hubspot.FilterOperatorHasProperty}}}
	}
	for _, test := range []struct {
		name  string
		input hubspot.SearchObjectsInput
	}{
		{name: "unknown object type", input: hubspot.SearchObjectsInput{ObjectType: "tickets"}},
		{name: "too many groups", input: hubspot.SearchObjectsInput{ObjectType: hubspot.ObjectTypeContacts, FilterGroups: manyGroups}},
		{name: "empty group", input: hubspot.SearchObjectsInput{ObjectType: hubspot.ObjectTypeContacts, FilterGroups: []hubspot.SearchFilterGroup{{}}}},
		{name: "unsafe property name", input: hubspot.SearchObjectsInput{ObjectType: hubspot.ObjectTypeContacts, FilterGroups: oneFilter(hubspot.SearchFilter{PropertyName: "email\"}", Operator: hubspot.FilterOperatorEqual, Value: "x"})}},
		{name: "equality without a value", input: hubspot.SearchObjectsInput{ObjectType: hubspot.ObjectTypeContacts, FilterGroups: oneFilter(hubspot.SearchFilter{PropertyName: "email", Operator: hubspot.FilterOperatorEqual})}},
		{name: "between without a high value", input: hubspot.SearchObjectsInput{ObjectType: hubspot.ObjectTypeDeals, FilterGroups: oneFilter(hubspot.SearchFilter{PropertyName: "amount", Operator: hubspot.FilterOperatorBetween, Value: "1"})}},
		{name: "in without values", input: hubspot.SearchObjectsInput{ObjectType: hubspot.ObjectTypeDeals, FilterGroups: oneFilter(hubspot.SearchFilter{PropertyName: "dealstage", Operator: hubspot.FilterOperatorIn})}},
		{name: "has property with a value", input: hubspot.SearchObjectsInput{ObjectType: hubspot.ObjectTypeDeals, FilterGroups: oneFilter(hubspot.SearchFilter{PropertyName: "dealstage", Operator: hubspot.FilterOperatorHasProperty, Value: "x"})}},
		{name: "unknown operator", input: hubspot.SearchObjectsInput{ObjectType: hubspot.ObjectTypeDeals, FilterGroups: oneFilter(hubspot.SearchFilter{PropertyName: "dealstage", Operator: "LIKE", Value: "x"})}},
		{name: "invalid sort", input: hubspot.SearchObjectsInput{ObjectType: hubspot.ObjectTypeDeals, Sort: &hubspot.SearchSort{PropertyName: "amount", Direction: "UP"}}},
		{name: "limit above maximum", input: hubspot.SearchObjectsInput{ObjectType: hubspot.ObjectTypeDeals, Limit: hubspot.MaximumSearchLimit + 1}},
		{name: "non-numeric cursor", input: hubspot.SearchObjectsInput{ObjectType: hubspot.ObjectTypeDeals, After: "abc"}},
		{name: "paging past ten thousand", input: hubspot.SearchObjectsInput{ObjectType: hubspot.ObjectTypeDeals, After: "9995", Limit: 10}},
		{name: "body above three thousand characters", input: hubspot.SearchObjectsInput{ObjectType: hubspot.ObjectTypeDeals, Query: strings.Repeat("q", hubspot.MaximumSearchRequestBytes)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := sdkgo.RunQuery(newStepDexContext("invalid-search"), client.SearchObjects(), hubspotConnection, test.input)
			require.NoError(t, err)
			require.Equal(t, hubspot.SearchObjectsBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
		})
	}
	require.Empty(t, provider.recordedRequests())
}

func TestGetObjectReadsRequestedPropertiesAndClassifiesMissingRecord(t *testing.T) {
	provider := newRecordingHubSpot(t, func(response http.ResponseWriter, request recordedRequest) {
		if strings.HasSuffix(request.path, "/404") {
			writeJSON(t, response, http.StatusNotFound, `{"status":"error","message":"`+providerMessageSentinel+`","category":"OBJECT_NOT_FOUND"}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"id":"501","properties":{"email":"ada@example.com","firstname":"Ada"},"archived":false}`)
	})
	client := newStaticTokenClient(t, provider.URL, hubspot.Config{})
	found, err := sdkgo.RunQuery(newStepDexContext("get-contact"), client.GetObject(), hubspotConnection, hubspot.GetObjectInput{
		ObjectType: hubspot.ObjectTypeContacts, ObjectID: "501", Properties: []string{"email", "firstname"},
	})
	require.NoError(t, err)
	require.Equal(t, hubspot.GetObjectBranchFound, found.Branch)
	require.Equal(t, "ada@example.com", found.Value.Properties["email"])
	require.Equal(t, "501", found.Receipt.ProviderObjectID)
	requests := provider.recordedRequests()
	require.Equal(t, http.MethodGet, requests[0].method)
	require.Equal(t, "/crm/objects/2026-09/contacts/501", requests[0].path)
	query, err := url.ParseQuery(requests[0].query)
	require.NoError(t, err)
	require.Equal(t, url.Values{"archived": {"false"}, "properties": {"email,firstname"}}, query)
	require.Empty(t, requests[0].contentType)

	missing, err := sdkgo.RunQuery(newStepDexContext("get-missing"), client.GetObject(), hubspotConnection, hubspot.GetObjectInput{
		ObjectType: hubspot.ObjectTypeContacts, ObjectID: "404",
	})
	require.NoError(t, err)
	require.Equal(t, hubspot.GetObjectBranchNotFound, missing.Branch)
	require.Equal(t, sdkgo.FailureNotFound, missing.Failure.Kind)
	require.Contains(t, missing.Failure.Message, "OBJECT_NOT_FOUND")
	require.NotContains(t, missing.Failure.Message, providerMessageSentinel)

	invalid, err := sdkgo.RunQuery(newStepDexContext("get-invalid"), client.GetObject(), hubspotConnection, hubspot.GetObjectInput{
		ObjectType: hubspot.ObjectTypeContacts, ObjectID: "../deals/1",
	})
	require.NoError(t, err)
	require.Equal(t, hubspot.GetObjectBranchDefect, invalid.Branch)
	require.Len(t, provider.recordedRequests(), 2, "an invalid record ID sends nothing")
}

func TestProviderErrorsMapToBranchesAndRetriesWithoutMessageText(t *testing.T) {
	for _, test := range []struct {
		name        string
		status      int
		header      map[string]string
		body        string
		wantBranch  sdkgo.BranchID
		wantKind    sdkgo.FailureKind
		wantRetry   bool
		wantDelay   time.Duration
		wantMessage string
	}{
		{name: "validation error", status: http.StatusBadRequest, body: `{"category":"VALIDATION_ERROR","errors":[{"code":"PROPERTY_DOESNT_EXIST","message":"` + providerMessageSentinel + `"}]}`,
			wantBranch: hubspot.GetObjectBranchProviderRejected, wantKind: sdkgo.FailureValidation, wantMessage: "(VALIDATION_ERROR, PROPERTY_DOESNT_EXIST)"},
		{name: "missing scopes", status: http.StatusForbidden, body: `{"category":"MISSING_SCOPES"}`,
			wantBranch: hubspot.GetObjectBranchProviderRejected, wantKind: sdkgo.FailureAuthorization, wantMessage: "lacks a required scope (MISSING_SCOPES)"},
		{name: "rejected static token", status: http.StatusUnauthorized, body: `{"category":"INVALID_AUTHENTICATION"}`,
			wantBranch: hubspot.GetObjectBranchProviderRejected, wantKind: sdkgo.FailureAuthentication},
		{name: "daily limit", status: http.StatusTooManyRequests, body: `{"errorType":"RATE_LIMIT","policyName":"DAILY"}`,
			wantBranch: hubspot.GetObjectBranchProviderRejected, wantKind: sdkgo.FailureQuotaExhausted},
		{name: "ten-second limit with Retry-After", status: http.StatusTooManyRequests, header: map[string]string{"Retry-After": "3"},
			body: `{"policyName":"TEN_SECONDLY_ROLLING"}`, wantRetry: true, wantKind: sdkgo.FailureRateLimit, wantDelay: 3 * time.Second},
		{name: "ten-second limit with interval header", status: http.StatusTooManyRequests,
			header: map[string]string{"X-HubSpot-RateLimit-Interval-Milliseconds": "10000"}, body: `{"category":"RATE_LIMITS"}`,
			wantRetry: true, wantKind: sdkgo.FailureRateLimit, wantDelay: 10 * time.Second},
		{name: "locked records", status: http.StatusLocked, wantRetry: true, wantKind: sdkgo.FailureRateLimit, wantDelay: 2 * time.Second},
		{name: "gateway timeout", status: http.StatusGatewayTimeout, wantRetry: true, wantKind: sdkgo.FailureAvailability},
		{name: "account migration", status: 477, header: map[string]string{"Retry-After": "60"}, wantRetry: true, wantKind: sdkgo.FailureAvailability, wantDelay: time.Minute},
		{name: "not implemented", status: http.StatusNotImplemented, wantBranch: hubspot.GetObjectBranchProviderRejected, wantKind: sdkgo.FailureProviderRejection},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingHubSpot(t, func(response http.ResponseWriter, _ recordedRequest) {
				for name, value := range test.header {
					response.Header().Set(name, value)
				}
				writeJSON(t, response, test.status, test.body)
			})
			client := newStaticTokenClient(t, provider.URL, hubspot.Config{})
			result, err := sdkgo.RunQuery(newStepDexContext("classify"), client.GetObject(), hubspotConnection, hubspot.GetObjectInput{
				ObjectType: hubspot.ObjectTypeContacts, ObjectID: "501",
			})
			if test.wantRetry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, test.wantKind, retry.Failure.Kind)
				require.NotContains(t, retry.Error(), providerMessageSentinel)
				if test.wantDelay > 0 {
					var retryAfter *dex.RetryAfterError
					require.ErrorAs(t, err, &retryAfter)
					require.Equal(t, test.wantDelay, retryAfter.After)
				}
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, test.wantKind, result.Failure.Kind)
			require.Contains(t, result.Failure.Message, test.wantMessage)
			require.NotContains(t, result.Failure.Message, providerMessageSentinel)
			require.Equal(t, "hubspot", result.Receipt.Provider)
		})
	}
}

func TestUpsertObjectSendsOneUniqueInputAndReportsCreation(t *testing.T) {
	provider := newRecordingHubSpot(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"status":"COMPLETE","results":[{"id":"501","new":true,
			"properties":{"email":"ada@example.com","firstname":"Ada","hubspot_owner_id":"77"},"archived":false}],
			"startedAt":"2026-09-30T10:00:00Z","completedAt":"2026-09-30T10:00:01Z"}`)
	})
	client := newStaticTokenClient(t, provider.URL, hubspot.Config{})
	result, err := sdkgo.RunMutation(newStepDexContext("upsert-contact"), client.UpsertObject(), hubspotConnection, hubspot.UpsertObjectInput{
		ObjectType: hubspot.ObjectTypeContacts, IDProperty: "email", IDValue: "ada@example.com",
		Properties: map[string]string{"firstname": "Ada", "hubspot_owner_id": "77"},
	})
	require.NoError(t, err)
	require.Equal(t, hubspot.UpsertObjectBranchUpserted, result.Branch)
	require.True(t, result.Value.Created)
	require.Equal(t, "501", result.Value.Object.ID)
	require.Equal(t, hubspot.ObjectTypeContacts, result.Value.Object.ObjectType)
	require.Equal(t, "501", result.Receipt.ProviderObjectID)
	require.NotEmpty(t, result.Receipt.IdempotencyKey)
	requests := provider.recordedRequests()
	require.Len(t, requests, 1)
	require.Equal(t, "/crm/objects/2026-09/contacts/batch/upsert", requests[0].path)
	require.JSONEq(t, `{"inputs":[{"idProperty":"email","id":"ada@example.com","properties":{"firstname":"Ada","hubspot_owner_id":"77"}}]}`, string(requests[0].body))
}

func TestUpsertObjectResendsOnceAfterAConflict(t *testing.T) {
	for _, test := range []struct {
		name         string
		conflicts    int
		wantBranch   sdkgo.BranchID
		wantRequests int
	}{
		{name: "a concurrent create is followed by an update", conflicts: 1, wantBranch: hubspot.UpsertObjectBranchUpserted, wantRequests: 2},
		{name: "a lasting conflict selects conflict", conflicts: 2, wantBranch: hubspot.UpsertObjectBranchConflict, wantRequests: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			var provider *recordingHubSpot
			provider = newRecordingHubSpot(t, func(response http.ResponseWriter, _ recordedRequest) {
				if len(provider.recordedRequests()) <= test.conflicts {
					writeJSON(t, response, http.StatusConflict, `{"category":"CONFLICT","message":"`+providerMessageSentinel+`"}`)
					return
				}
				writeJSON(t, response, http.StatusOK, `{"status":"COMPLETE","results":[{"id":"501","new":false,"properties":{"email":"ada@example.com"}}]}`)
			})
			client := newStaticTokenClient(t, provider.URL, hubspot.Config{})
			result, err := sdkgo.RunMutation(newStepDexContext("upsert-conflict"), client.UpsertObject(), hubspotConnection, hubspot.UpsertObjectInput{
				ObjectType: hubspot.ObjectTypeContacts, IDProperty: "email", IDValue: "ada@example.com",
			})
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Len(t, provider.recordedRequests(), test.wantRequests)
			if test.wantBranch == hubspot.UpsertObjectBranchConflict {
				require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, providerMessageSentinel)
			} else {
				require.False(t, result.Value.Created)
			}
		})
	}
}

func TestUpsertObjectClassifiesBatchErrorsAndMalformedResults(t *testing.T) {
	for _, test := range []struct {
		name       string
		status     int
		body       string
		wantBranch sdkgo.BranchID
		wantKind   sdkgo.FailureKind
	}{
		{name: "multi-status validation error", status: http.StatusMultiStatus,
			body:       `{"status":"COMPLETE","results":[],"numErrors":1,"errors":[{"status":"error","category":"VALIDATION_ERROR","message":"` + providerMessageSentinel + `"}]}`,
			wantBranch: hubspot.UpsertObjectBranchProviderRejected, wantKind: sdkgo.FailureValidation},
		{name: "no record", status: http.StatusOK, body: `{"status":"COMPLETE","results":[]}`,
			wantBranch: hubspot.UpsertObjectBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "two records", status: http.StatusOK, body: `{"results":[{"id":"1"},{"id":"2"}]}`,
			wantBranch: hubspot.UpsertObjectBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "record without ID", status: http.StatusOK, body: `{"results":[{"id":"x"}]}`,
			wantBranch: hubspot.UpsertObjectBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "malformed JSON", status: http.StatusOK, body: `{"results":`,
			wantBranch: hubspot.UpsertObjectBranchInvalidResponse, wantKind: sdkgo.FailureProtocol},
		{name: "unknown ID property", status: http.StatusBadRequest, body: `{"category":"VALIDATION_ERROR"}`,
			wantBranch: hubspot.UpsertObjectBranchProviderRejected, wantKind: sdkgo.FailureValidation},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingHubSpot(t, func(response http.ResponseWriter, _ recordedRequest) {
				writeJSON(t, response, test.status, test.body)
			})
			client := newStaticTokenClient(t, provider.URL, hubspot.Config{})
			result, err := sdkgo.RunMutation(newStepDexContext("upsert-errors"), client.UpsertObject(), hubspotConnection, hubspot.UpsertObjectInput{
				ObjectType: hubspot.ObjectTypeCompanies, IDProperty: "erp_company_id", IDValue: "ERP-1",
				Properties: map[string]string{"name": "Acme"},
			})
			require.NoError(t, err)
			require.Equal(t, test.wantBranch, result.Branch)
			require.Equal(t, test.wantKind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, providerMessageSentinel)
			require.Len(t, provider.recordedRequests(), 1)
		})
	}
}

func TestUpsertAndUpdateRejectInvalidInputWithoutCallingHubSpot(t *testing.T) {
	provider := newRecordingHubSpot(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	client := newStaticTokenClient(t, provider.URL, hubspot.Config{})
	tooManyProperties := map[string]string{}
	for index := 0; index <= hubspot.MaximumProperties; index++ {
		tooManyProperties["property_"+strings.Repeat("x", index%5)+string(rune('a'+index%26))+strings.Repeat("y", index/26)] = "value"
	}
	for _, input := range []hubspot.UpsertObjectInput{
		{ObjectType: hubspot.ObjectTypeContacts, IDProperty: "email"},
		{ObjectType: hubspot.ObjectTypeContacts, IDProperty: "e-mail", IDValue: "ada@example.com"},
		{ObjectType: "leads", IDProperty: "email", IDValue: "ada@example.com"},
		{ObjectType: hubspot.ObjectTypeContacts, IDProperty: "email", IDValue: "ada@example.com", Properties: map[string]string{"bad name": "x"}},
		{ObjectType: hubspot.ObjectTypeContacts, IDProperty: "email", IDValue: "ada@example.com", Properties: map[string]string{"notes": strings.Repeat("x", hubspot.MaximumPropertyValueCharacters+1)}},
		{ObjectType: hubspot.ObjectTypeContacts, IDProperty: "email", IDValue: "ada@example.com", Properties: tooManyProperties},
	} {
		result, err := sdkgo.RunMutation(newStepDexContext("invalid-upsert"), client.UpsertObject(), hubspotConnection, input)
		require.NoError(t, err)
		require.Equal(t, hubspot.UpsertObjectBranchDefect, result.Branch)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	}
	for _, input := range []hubspot.UpdateObjectInput{
		{ObjectType: hubspot.ObjectTypeDeals, ObjectID: "9001"},
		{ObjectType: hubspot.ObjectTypeDeals, ObjectID: "9001/../1", Properties: map[string]string{"dealstage": "x"}},
	} {
		result, err := sdkgo.RunMutation(newStepDexContext("invalid-update"), client.UpdateObject(), hubspotConnection, input)
		require.NoError(t, err)
		require.Equal(t, hubspot.UpdateObjectBranchDefect, result.Branch)
	}
	require.Empty(t, provider.recordedRequests())
}

func TestUpdateObjectPatchesPropertiesAndClassifiesConflictAndNotFound(t *testing.T) {
	provider := newRecordingHubSpot(t, func(response http.ResponseWriter, request recordedRequest) {
		switch {
		case strings.HasSuffix(request.path, "/404"):
			writeJSON(t, response, http.StatusNotFound, `{"category":"OBJECT_NOT_FOUND"}`)
		case strings.HasSuffix(request.path, "/409"):
			writeJSON(t, response, http.StatusConflict, `{"category":"CONFLICT","message":"`+providerMessageSentinel+`"}`)
		default:
			writeJSON(t, response, http.StatusOK, `{"id":"9001","properties":{"dealstage":"qualifiedtobuy"},"archived":false}`)
		}
	})
	client := newStaticTokenClient(t, provider.URL, hubspot.Config{})
	updated, err := sdkgo.RunMutation(newStepDexContext("update-deal"), client.UpdateObject(), hubspotConnection, hubspot.UpdateObjectInput{
		ObjectType: hubspot.ObjectTypeDeals, ObjectID: "9001", Properties: map[string]string{"dealstage": "qualifiedtobuy"},
	})
	require.NoError(t, err)
	require.Equal(t, hubspot.UpdateObjectBranchUpdated, updated.Branch)
	require.Equal(t, "qualifiedtobuy", updated.Value.Properties["dealstage"])
	requests := provider.recordedRequests()
	require.Equal(t, http.MethodPatch, requests[0].method)
	require.Equal(t, "/crm/objects/2026-09/deals/9001", requests[0].path)
	require.JSONEq(t, `{"properties":{"dealstage":"qualifiedtobuy"}}`, string(requests[0].body))

	for objectID, wantBranch := range map[string]sdkgo.BranchID{"404": hubspot.UpdateObjectBranchNotFound, "409": hubspot.UpdateObjectBranchConflict} {
		result, err := sdkgo.RunMutation(newStepDexContext("update-"+objectID), client.UpdateObject(), hubspotConnection, hubspot.UpdateObjectInput{
			ObjectType: hubspot.ObjectTypeContacts, ObjectID: objectID, Properties: map[string]string{"email": "taken@example.com"},
		})
		require.NoError(t, err)
		require.Equal(t, wantBranch, result.Branch)
		require.NotContains(t, result.Failure.Message, providerMessageSentinel)
	}
}

func TestMutationTransportFailureAfterDispatchIsRetriedNotUncertain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		hijacker, ok := response.(http.Hijacker)
		require.True(t, ok)
		connection, _, err := hijacker.Hijack()
		require.NoError(t, err)
		require.NoError(t, connection.Close())
	}))
	defer server.Close()
	client := newStaticTokenClient(t, server.URL, hubspot.Config{})
	_, err := sdkgo.RunMutation(newStepDexContext("dropped-update"), client.UpdateObject(), hubspotConnection, hubspot.UpdateObjectInput{
		ObjectType: hubspot.ObjectTypeDeals, ObjectID: "9001", Properties: map[string]string{"dealstage": "qualifiedtobuy"},
	})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
}

func TestOversizedResponseSelectsInvalidResponse(t *testing.T) {
	provider := newRecordingHubSpot(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{"id":"501","properties":{"notes":"`+strings.Repeat("x", 128)+`"}}`)
	})
	client := newStaticTokenClient(t, provider.URL, hubspot.Config{MaxResponseBytes: 64})
	result, err := sdkgo.RunQuery(newStepDexContext("oversized"), client.GetObject(), hubspotConnection, hubspot.GetObjectInput{
		ObjectType: hubspot.ObjectTypeContacts, ObjectID: "501",
	})
	require.NoError(t, err)
	require.Equal(t, hubspot.GetObjectBranchInvalidResponse, result.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, result.Failure.Kind)
}

func TestRedirectIsNotFollowedWithTheAccessToken(t *testing.T) {
	isRedirectFollowed := false
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { isRedirectFollowed = true }))
	defer destination.Close()
	provider := newRecordingHubSpot(t, func(response http.ResponseWriter, _ recordedRequest) {
		response.Header().Set("Location", destination.URL)
		response.WriteHeader(http.StatusTemporaryRedirect)
	})
	client := newStaticTokenClient(t, provider.URL, hubspot.Config{})
	result, err := sdkgo.RunQuery(newStepDexContext("redirect"), client.GetObject(), hubspotConnection, hubspot.GetObjectInput{
		ObjectType: hubspot.ObjectTypeContacts, ObjectID: "501",
	})
	require.NoError(t, err)
	require.Equal(t, hubspot.GetObjectBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureProtocol, result.Failure.Kind)
	require.False(t, isRedirectFollowed)
}

func TestMissingCredentialsSelectDefectWithoutCallingHubSpot(t *testing.T) {
	provider := newRecordingHubSpot(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	client, err := hubspot.New(hubspot.Config{Endpoint: provider.URL}, sdkgo.StaticCredentialProvider[hubspot.Credentials]{
		hubspotConnection: {AuthMethodID: hubspot.PrivateAppTokenAuthMethodID, AccessToken: sdkgo.NewSecretString("token with spaces")},
	})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(newStepDexContext("bad-token"), client.GetObject(), hubspotConnection, hubspot.GetObjectInput{
		ObjectType: hubspot.ObjectTypeContacts, ObjectID: "501",
	})
	require.NoError(t, err)
	require.Equal(t, hubspot.GetObjectBranchDefect, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
	require.Empty(t, provider.recordedRequests())
}

func TestExpiredOAuthTokenIsRefreshedBeforeTheCallAndPersisted(t *testing.T) {
	var tokenForms []url.Values
	provider := newRecordingHubSpot(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.authorization != "Bearer refreshed-access-token" {
			writeJSON(t, response, http.StatusUnauthorized, `{"category":"EXPIRED_AUTHENTICATION"}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"id":"501","properties":{"email":"ada@example.com"}}`)
	})
	httpClient := &http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, request *http.Request) {
		require.NoError(t, request.ParseForm())
		tokenForms = append(tokenForms, request.PostForm)
		writeJSON(t, response, http.StatusOK, `{"token_type":"bearer","access_token":"refreshed-access-token","expires_in":1800,
			"hub_id":1234567,"scopes":["oauth","crm.objects.contacts.read","crm.objects.contacts.write","crm.objects.companies.read",
			"crm.objects.companies.write","crm.objects.deals.read","crm.objects.deals.write","crm.objects.owners.read"]}`)
	}}}
	path := writeOAuthConnectionsFile(t, provider.URL, "expired-access-token", time.Now().Add(-time.Minute))
	client := newLocalOAuthClient(t, path, httpClient)
	result, err := sdkgo.RunQuery(newStepDexContext("get-with-refresh"), client.GetObject(), hubspotConnection, hubspot.GetObjectInput{
		ObjectType: hubspot.ObjectTypeContacts, ObjectID: "501",
	})
	require.NoError(t, err)
	require.Equal(t, hubspot.GetObjectBranchFound, result.Branch)
	require.Len(t, provider.recordedRequests(), 1, "the expired token is refreshed before it is sent")
	require.Len(t, tokenForms, 1)
	require.Equal(t, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {"stored-refresh-token"},
		"client_id": {"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}, "client_secret": {"oauth-client-secret"},
	}, tokenForms[0])
	stored := readStoredCredentials(t, path)
	require.Equal(t, "refreshed-access-token", stored["access_token"])
	require.Equal(t, "stored-refresh-token", stored["refresh_token"], "HubSpot omitted a replacement, so the prior refresh token is kept")
	require.Equal(t, hubspot.OAuthAuthMethodID, stored["auth_method"])
}

func TestRejectedOAuthTokenIsRefreshedOnceAndResent(t *testing.T) {
	tokenRequests := 0
	provider := newRecordingHubSpot(t, func(response http.ResponseWriter, request recordedRequest) {
		if request.authorization != "Bearer rotated-access-token" {
			writeJSON(t, response, http.StatusUnauthorized, `{"category":"INVALID_AUTHENTICATION"}`)
			return
		}
		writeJSON(t, response, http.StatusOK, `{"id":"9001","properties":{"dealstage":"qualifiedtobuy"}}`)
	})
	httpClient := &http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
		tokenRequests++
		writeJSON(t, response, http.StatusOK, `{"token_type":"bearer","access_token":"rotated-access-token","refresh_token":"rotated-refresh-token","expires_in":1800}`)
	}}}
	path := writeOAuthConnectionsFile(t, provider.URL, "revoked-but-unexpired-token", time.Now().Add(time.Hour))
	client := newLocalOAuthClient(t, path, httpClient)
	result, err := sdkgo.RunMutation(newStepDexContext("update-after-rejection"), client.UpdateObject(), hubspotConnection, hubspot.UpdateObjectInput{
		ObjectType: hubspot.ObjectTypeDeals, ObjectID: "9001", Properties: map[string]string{"dealstage": "qualifiedtobuy"},
	})
	require.NoError(t, err)
	require.Equal(t, hubspot.UpdateObjectBranchUpdated, result.Branch)
	require.Equal(t, 1, tokenRequests)
	require.Len(t, provider.recordedRequests(), 2, "the rejected update is resent once with the replacement token")
	require.Equal(t, "rotated-refresh-token", readStoredCredentials(t, path)["refresh_token"])
}

func TestRevokedOAuthGrantSelectsProviderRejectedAndStopsRefreshing(t *testing.T) {
	tokenRequests := 0
	provider := newRecordingHubSpot(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	httpClient := &http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
		tokenRequests++
		writeJSON(t, response, http.StatusBadRequest, `{"error":"invalid_grant","status":"BAD_REFRESH_TOKEN","message":"`+providerMessageSentinel+`"}`)
	}}}
	path := writeOAuthConnectionsFile(t, provider.URL, "expired-access-token", time.Now().Add(-time.Minute))
	client := newLocalOAuthClient(t, path, httpClient)
	for attempt := 0; attempt < 2; attempt++ {
		result, err := sdkgo.RunQuery(newStepDexContext("get-revoked"), client.GetObject(), hubspotConnection, hubspot.GetObjectInput{
			ObjectType: hubspot.ObjectTypeContacts, ObjectID: "501",
		})
		require.NoError(t, err)
		require.Equal(t, hubspot.GetObjectBranchProviderRejected, result.Branch)
		require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
		require.NotContains(t, result.Failure.Message, providerMessageSentinel)
	}
	require.Equal(t, 1, tokenRequests, "reauthorization_required stops automatic refresh")
	require.Empty(t, provider.recordedRequests())
}

func TestTransientOAuthRefreshFailureIsRetried(t *testing.T) {
	provider := newRecordingHubSpot(t, func(response http.ResponseWriter, _ recordedRequest) {
		writeJSON(t, response, http.StatusOK, `{}`)
	})
	httpClient := &http.Client{Transport: tokenEndpointTransport{tokenHandler: func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(t, response, http.StatusBadGateway, `{"error":"invalid_grant"}`)
	}}}
	path := writeOAuthConnectionsFile(t, provider.URL, "expired-access-token", time.Now().Add(-time.Minute))
	client := newLocalOAuthClient(t, path, httpClient)
	_, err := sdkgo.RunQuery(newStepDexContext("get-refresh-outage"), client.GetObject(), hubspotConnection, hubspot.GetObjectInput{
		ObjectType: hubspot.ObjectTypeContacts, ObjectID: "501",
	})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
	require.Equal(t, "expired-access-token", readStoredCredentials(t, path)["access_token"], "a failed refresh leaves the file unchanged")
	require.Empty(t, provider.recordedRequests())
}

func newLocalOAuthClient(t *testing.T, path string, httpClient *http.Client) *hubspot.Client {
	t.Helper()
	store, err := localconfig.LoadFile(path)
	require.NoError(t, err)
	var config hubspot.Config
	require.NoError(t, store.DecodeConfiguration(hubspot.ConnectorID, hubspotConnection.Name, &config))
	credentials := localconfig.NewRefreshingCredentialProvider(store, hubspot.ConnectorID, hubspotConnection.Name,
		hubspot.DecodeCredentialsJSON, func(credentials hubspot.Credentials) (json.RawMessage, error) {
			return hubspot.EncodeCredentialsJSON(credentials)
		})
	client, err := hubspot.New(config, credentials, hubspot.WithHTTPClient(httpClient))
	require.NoError(t, err)
	return client
}
