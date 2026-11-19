// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sell

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestSellRecordFamiliesCoverCRUD(t *testing.T) {
	cases := []struct {
		resource SellResource
		fields   map[string]json.RawMessage
		body     string
	}{
		{SellContacts, map[string]json.RawMessage{"last_name": json.RawMessage(`"Lovelace"`), "email": json.RawMessage(`"ada@example.com"`)}, `{"id":144,"first_name":"Ada","last_name":"Lovelace","email":"ada@example.com"}`},
		{SellDeals, map[string]json.RawMessage{"name": json.RawMessage(`"Renewal"`), "value": json.RawMessage(`"1000.50"`)}, `{"id":144,"name":"Renewal","value":"1000.50"}`},
		{SellTasks, map[string]json.RawMessage{"content": json.RawMessage(`"Call Ada"`), "completed": json.RawMessage(`false`)}, `{"id":144,"content":"Call Ada","completed":false}`},
		{SellNotes, map[string]json.RawMessage{"content": json.RawMessage(`"Met at event"`), "resource_type": json.RawMessage(`"lead"`), "resource_id": json.RawMessage(`144`)}, `{"id":144,"content":"Met at event","resource_type":"lead","resource_id":144}`},
	}
	for _, testCase := range cases {
		t.Run(string(testCase.resource), func(t *testing.T) {
			var calls []string
			provider := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				require.Equal(t, "Bearer "+testAccessToken, request.Header.Get("Authorization"))
				calls = append(calls, request.Method+" "+request.URL.Path)
				switch request.Method {
				case http.MethodGet:
					if strings.HasSuffix(request.URL.Path, "/144") {
						_, err := writer.Write([]byte(`{"data":` + testCase.body + `}`))
						require.NoError(t, err)
					} else {
						_, err := writer.Write([]byte(`{"items":[{"data":` + testCase.body + `}]}`))
						require.NoError(t, err)
					}
				case http.MethodPost, http.MethodPut:
					var envelope map[string]map[string]json.RawMessage
					require.NoError(t, json.NewDecoder(request.Body).Decode(&envelope))
					require.Equal(t, testCase.fields, envelope["data"])
					_, err := writer.Write([]byte(`{"data":` + testCase.body + `}`))
					require.NoError(t, err)
				case http.MethodDelete:
					writer.WriteHeader(http.StatusNoContent)
				}
			}))
			defer provider.Close()
			client := newSellWriteClient(t, provider.URL)
			page, err := sdkgo.RunQuery(queryContext{}, client.ListRecords(), testConnection, ListRecordsInput{Resource: testCase.resource, Page: 1, PerPage: 20})
			require.NoError(t, err)
			require.Equal(t, ListRecordsBranchListed, page.Branch)
			require.Len(t, page.Value.Records, 1)
			found, err := sdkgo.RunQuery(queryContext{}, client.GetRecord(), testConnection, GetRecordInput{Resource: testCase.resource, RecordID: "144"})
			require.NoError(t, err)
			require.Equal(t, GetRecordBranchFound, found.Branch)
			created, err := sdkgo.RunMutation(newSellWriteContext(), client.CreateRecord(), testConnection, CreateRecordInput{Resource: testCase.resource, Fields: testCase.fields})
			require.NoError(t, err)
			require.Equal(t, CreateRecordBranchCreated, created.Branch)
			updated, err := sdkgo.RunMutation(newSellWriteContext(), client.UpdateRecord(), testConnection, UpdateRecordInput{Resource: testCase.resource, RecordID: "144", Fields: testCase.fields})
			require.NoError(t, err)
			require.Equal(t, UpdateRecordBranchUpdated, updated.Branch)
			deleted, err := sdkgo.RunMutation(newSellWriteContext(), client.DeleteRecord(), testConnection, DeleteRecordInput{Resource: testCase.resource, RecordID: "144"})
			require.NoError(t, err)
			require.Equal(t, DeleteRecordBranchDeleted, deleted.Branch)
			require.Equal(t, []string{
				"GET /v2/" + string(testCase.resource),
				"GET /v2/" + string(testCase.resource) + "/144",
				"POST /v2/" + string(testCase.resource),
				"PUT /v2/" + string(testCase.resource) + "/144",
				"DELETE /v2/" + string(testCase.resource) + "/144",
			}, calls)
		})
	}
}

func TestSellRecordFieldValidationRejectsUnknownKeys(t *testing.T) {
	client := newSellWriteClient(t, "http://127.0.0.1:12345")
	result, err := sdkgo.RunMutation(newSellWriteContext(), client.CreateRecord(), testConnection, CreateRecordInput{
		Resource: SellContacts, Fields: map[string]json.RawMessage{"access_token": json.RawMessage(`"secret"`)},
	})
	require.NoError(t, err)
	require.Equal(t, CreateRecordBranchDefect, result.Branch)
	require.NotContains(t, result.Failure.Message, "secret")
}
