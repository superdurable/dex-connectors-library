// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package azuredevops

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestQueryWorkItemsSendsBoundedWIQLAndReturnsIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/example/ProjectA/_apis/wit/wiql", request.URL.Path)
		require.Equal(t, "20", request.URL.Query().Get("$top"))
		require.Equal(t, "7.1", request.URL.Query().Get("api-version"))
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var payload map[string]string
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		require.Equal(t, "SELECT [System.Id] FROM WorkItems", payload["query"])
		_, err := writer.Write([]byte(`{"queryType":"flat","workItems":[{"id":144}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL + "/example"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {PersonalAccessToken: sdkgo.NewSecretString(testPersonalAccessToken)}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.QueryWorkItems(), testConnection, QueryWorkItemsInput{Project: "ProjectA", WIQL: "SELECT [System.Id] FROM WorkItems", Top: 20})
	require.NoError(t, err)
	require.Equal(t, QueryWorkItemsBranchListed, result.Branch)
	require.Equal(t, []int64{144}, result.Value.IDs)
}
