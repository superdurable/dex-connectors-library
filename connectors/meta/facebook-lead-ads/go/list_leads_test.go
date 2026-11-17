// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package facebookleadads

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListLeadsReadsIdentityPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodGet, request.Method)
		require.Equal(t, "/123456789012345/leads", request.URL.Path)
		require.Equal(t, "cursorABC", request.URL.Query().Get("after"))
		require.Equal(t, "id,created_time,form_id,ad_id", request.URL.Query().Get("fields"))
		require.Equal(t, "Bearer "+testPageToken, request.Header.Get("Authorization"))
		_, err := writer.Write([]byte(`{"data":[{"id":"987654321012345","form_id":"123456789012345","ad_id":"112233445566778","created_time":"2026-11-08T10:00:00+0000","field_data":[{"name":"email","values":["private@example.com"]}]}],"paging":{"cursors":{"after":"cursorDEF"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListLeads(), testConnection, ListLeadsInput{FormID: "123456789012345", After: "cursorABC"})
	require.NoError(t, err)
	require.Equal(t, ListLeadsBranchListed, result.Branch)
	require.Equal(t, "987654321012345", result.Value.Leads[0].ID)
	require.Equal(t, "cursorDEF", result.Value.NextCursor)
}

func TestListLeadsClassifiesOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		status int
		branch sdkgo.BranchID
		retry  bool
	}{{404, ListLeadsBranchNotFound, false}, {403, ListLeadsBranchProviderRejected, false}, {200, ListLeadsBranchInvalidResponse, false}, {429, "", true}, {503, "", true}} {
		t.Run(http.StatusText(testCase.status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(`{}`))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL).ListLeads(), testConnection, ListLeadsInput{FormID: "123456789012345", After: "cursorABC"})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}

func TestListLeadsRejectsInvalidFormID(t *testing.T) {
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, "https://graph.facebook.com/v25.0").ListLeads(), testConnection, ListLeadsInput{FormID: "../bad"})
	require.NoError(t, err)
	require.Equal(t, ListLeadsBranchDefect, result.Branch)
}
