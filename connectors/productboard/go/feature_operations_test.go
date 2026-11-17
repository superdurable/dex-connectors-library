// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package productboard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const testFeatureID = "123e4567-e89b-12d3-a456-426614174000"
const testComponentID = "223e4567-e89b-12d3-a456-426614174000"

func TestGetFeatureReadsOnlyName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/entities/"+testFeatureID, request.URL.Path)
		require.Equal(t, "name", request.URL.Query().Get("fields[]"))
		_, err := writer.Write([]byte(`{"data":{"id":"` + testFeatureID + `","type":"feature","fields":{"name":"Roadmap","description":"private"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).GetFeature(), testConnection, GetFeatureInput{FeatureID: testFeatureID})
	require.NoError(t, err)
	require.Equal(t, GetFeatureBranchFound, result.Branch)
	require.Equal(t, FeatureMetadata{ID: testFeatureID, Name: "Roadmap"}, result.Value)
}

func TestFeatureWritesUseProviderShapes(t *testing.T) {
	for _, testCase := range []struct {
		name, method, path, body string
		invoke                   func(*Client) (sdkgo.BranchID, error)
	}{
		{"create", http.MethodPost, "/entities", `{"data":{"fields":{"name":"Roadmap"},"type":"feature","relationships":[{"type":"parent","target":{"type":"component","id":"` + testComponentID + `"}}]}}`, func(client *Client) (sdkgo.BranchID, error) {
			result, err := sdkgo.RunMutation(singledispatchtest.NewContext("productboard-feature", "create-feature"), client.CreateFeature(), testConnection, CreateFeatureInput{Name: "Roadmap", ParentComponentID: testComponentID})
			return result.Branch, err
		}},
		{"rename", http.MethodPatch, "/entities/" + testFeatureID, `{"data":{"fields":{"name":"Roadmap"}}}`, func(client *Client) (sdkgo.BranchID, error) {
			result, err := sdkgo.RunMutation(queryContext{}, client.RenameFeature(), testConnection, RenameFeatureInput{FeatureID: testFeatureID, Name: "Roadmap"})
			return result.Branch, err
		}},
		{"delete", http.MethodDelete, "/entities/" + testFeatureID, "", func(client *Client) (sdkgo.BranchID, error) {
			result, err := sdkgo.RunMutation(queryContext{}, client.DeleteFeature(), testConnection, DeleteFeatureInput{FeatureID: testFeatureID})
			return result.Branch, err
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				require.Equal(t, testCase.method, request.Method)
				require.Equal(t, testCase.path, request.URL.Path)
				require.Equal(t, "Bearer sample-productboard-token", request.Header.Get("Authorization"))
				if testCase.method != http.MethodDelete {
					payload, err := io.ReadAll(request.Body)
					require.NoError(t, err)
					require.JSONEq(t, testCase.body, string(payload))
				}
				if testCase.method == http.MethodDelete {
					writer.WriteHeader(http.StatusNoContent)
					return
				}
				if testCase.method == http.MethodPost {
					writer.WriteHeader(http.StatusCreated)
				}
				_, err := writer.Write([]byte(`{"data":{"id":"` + testFeatureID + `","type":"feature"}}`))
				require.NoError(t, err)
			}))
			defer server.Close()
			branch, err := testCase.invoke(newTestClient(t, server.URL, 0))
			require.NoError(t, err)
			require.Contains(t, []sdkgo.BranchID{CreateFeatureBranchCreated, RenameFeatureBranchRenamed, DeleteFeatureBranchDeleted}, branch)
		})
	}
}
