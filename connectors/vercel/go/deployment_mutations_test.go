// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vercel

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestDeploymentMutationsUseDocumentedEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "team_123", request.URL.Query().Get("teamId"))
		require.Equal(t, "Bearer test-accesstoken", request.Header.Get("Authorization"))
		switch request.Method {
		case http.MethodPatch:
			require.Equal(t, "/v12/deployments/dpl_meridian1/cancel", request.URL.Path)
			_, err := writer.Write([]byte(`{"id":"dpl_meridian1","readyState":"CANCELED"}`))
			require.NoError(t, err)
		case http.MethodDelete:
			require.Equal(t, "/v13/deployments/dpl_meridian1", request.URL.Path)
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected method %s", request.Method)
		}
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	canceled, err := sdkgo.RunMutation(testContext{}, client.CancelDeployment(), testConnection, CancelDeploymentInput{DeploymentID: "dpl_meridian1", TeamID: "team_123"})
	require.NoError(t, err)
	require.Equal(t, CancelDeploymentBranchCanceled, canceled.Branch)
	require.Equal(t, "dpl_meridian1", canceled.Value.ID)
	deleted, err := sdkgo.RunMutation(testContext{}, client.DeleteDeployment(), testConnection, DeleteDeploymentInput{DeploymentID: "dpl_meridian1", TeamID: "team_123"})
	require.NoError(t, err)
	require.Equal(t, DeleteDeploymentBranchDeleted, deleted.Branch)
	require.Equal(t, "dpl_meridian1", deleted.Value.DeploymentID)
}
