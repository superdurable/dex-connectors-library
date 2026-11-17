// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package acrobatsign

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListAgreementsAndVisibilityUseRegionalHost(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer sample-acrobat-sign-integration-key", request.Header.Get("Authorization"))
		switch request.URL.Path {
		case "/api/rest/v6/baseUris":
			_, err := fmt.Fprintf(writer, `{"apiAccessPoint":%q}`, server.URL+"/")
			require.NoError(t, err)
		case "/api/rest/v6/agreements":
			require.Equal(t, http.MethodGet, request.Method)
			require.Equal(t, "2", request.URL.Query().Get("pageSize"))
			require.Equal(t, "opaque-cursor", request.URL.Query().Get("cursor"))
			_, err := writer.Write([]byte(`{"userAgreementList":[{"id":"3AAABLblqZNOTREALAGREEMENTID5_BjiH","name":"Example agreement","status":"SIGNED","displayParticipantSetInfos":[{"email":"private@example.com"}]}],"page":{"nextCursor":"next"}}`))
			require.NoError(t, err)
		case "/api/rest/v6/agreements/3AAABLblqZNOTREALAGREEMENTID5_BjiH/me/visibility":
			require.Equal(t, http.MethodPut, request.Method)
			require.Equal(t, "application/json", request.Header.Get("Content-Type"))
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"visibility":"HIDE"}`, string(body))
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/rest/v6", 0)
	page, err := sdkgo.RunQuery(queryContext{}, client.ListAgreements(), testConnection, ListAgreementsInput{Cursor: "opaque-cursor", PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, ListAgreementsBranchListed, page.Branch)
	require.Equal(t, "next", page.Value.NextCursor)
	require.Len(t, page.Value.Agreements, 1)
	require.Equal(t, "SIGNED", page.Value.Agreements[0].Status)
	visibility, err := sdkgo.RunMutation(queryContext{}, client.UpdateAgreementVisibility(), testConnection, UpdateAgreementVisibilityInput{AgreementID: page.Value.Agreements[0].ID, Visibility: "HIDE"})
	require.NoError(t, err)
	require.Equal(t, UpdateAgreementVisibilityBranchUpdated, visibility.Branch)
	require.Equal(t, "HIDE", visibility.Value.Visibility)
}

func TestAgreementExpansionClassifiesOutcomes(t *testing.T) {
	for _, test := range []struct {
		name             string
		status           int
		body             string
		listBranch       sdkgo.BranchID
		visibilityBranch sdkgo.BranchID
		retry            bool
	}{
		{"rejected", 403, `{}`, ListAgreementsBranchProviderRejected, UpdateAgreementVisibilityBranchProviderRejected, false},
		{"malformed", 200, `{}`, ListAgreementsBranchInvalidResponse, UpdateAgreementVisibilityBranchUpdated, false},
		{"rate limit", 429, `{}`, "", "", true},
		{"unavailable", 503, `{}`, "", "", true},
		{"unknown", 500, `{}`, "", "", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/api/rest/v6/baseUris" {
					_, err := fmt.Fprintf(writer, `{"apiAccessPoint":%q}`, server.URL+"/")
					require.NoError(t, err)
					return
				}
				writer.WriteHeader(test.status)
				_, err := writer.Write([]byte(test.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			client := newTestClient(t, server.URL+"/api/rest/v6", 0)
			page, listErr := sdkgo.RunQuery(queryContext{}, client.ListAgreements(), testConnection, ListAgreementsInput{})
			visibility, visibilityErr := sdkgo.RunMutation(queryContext{}, client.UpdateAgreementVisibility(), testConnection, UpdateAgreementVisibilityInput{AgreementID: "3AAABLblqZNOTREALAGREEMENTID5_BjiH", Visibility: "SHOW"})
			if test.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, listErr, &retry)
				require.ErrorAs(t, visibilityErr, &retry)
			} else {
				require.NoError(t, listErr)
				require.NoError(t, visibilityErr)
				require.Equal(t, test.listBranch, page.Branch)
				require.Equal(t, test.visibilityBranch, visibility.Branch)
			}
		})
	}
}

func TestAgreementExpansionRejectsInvalidInputs(t *testing.T) {
	client := newTestClient(t, "https://api.adobesign.com/api/rest/v6", 0)
	page, err := sdkgo.RunQuery(queryContext{}, client.ListAgreements(), testConnection, ListAgreementsInput{PageSize: 101})
	require.NoError(t, err)
	require.Equal(t, ListAgreementsBranchDefect, page.Branch)
	visibility, err := sdkgo.RunMutation(queryContext{}, client.UpdateAgreementVisibility(), testConnection, UpdateAgreementVisibilityInput{AgreementID: "bad/ID", Visibility: "HIDE"})
	require.NoError(t, err)
	require.Equal(t, UpdateAgreementVisibilityBranchDefect, visibility.Branch)
}
