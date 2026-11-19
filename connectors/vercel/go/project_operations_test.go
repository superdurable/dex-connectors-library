// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package vercel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestProjectOperationsUseDocumentedEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "team_123", request.URL.Query().Get("teamId"))
		require.Equal(t, "Bearer test-accesstoken", request.Header.Get("Authorization"))
		switch request.Method + " " + request.URL.Path {
		case "GET /v9/projects":
			_, err := writer.Write([]byte(`{"projects":[{"id":"prj_meridian1","name":"meridian-web"}],"pagination":{"next":null}}`))
			require.NoError(t, err)
		case "GET /v9/projects/prj_meridian1":
			_, err := writer.Write([]byte(`{"id":"prj_meridian1","name":"meridian-web"}`))
			require.NoError(t, err)
		case "POST /v9/projects":
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"name":"meridian-web"}`, string(body))
			writer.WriteHeader(http.StatusCreated)
			_, err = writer.Write([]byte(`{"id":"prj_meridian1","name":"meridian-web"}`))
			require.NoError(t, err)
		case "PATCH /v9/projects/prj_meridian1":
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"name":"meridian-renamed"}`, string(body))
			_, err = writer.Write([]byte(`{"id":"prj_meridian1","name":"meridian-renamed"}`))
			require.NoError(t, err)
		case "DELETE /v9/projects/prj_meridian1":
			writer.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	listed, err := sdkgo.RunQuery(testContext{}, client.ListProjects(), testConnection, ListProjectsInput{TeamID: "team_123"})
	require.NoError(t, err)
	require.Len(t, listed.Value.Projects, 1)
	found, err := sdkgo.RunQuery(testContext{}, client.GetProject(), testConnection, GetProjectInput{ProjectID: "prj_meridian1", TeamID: "team_123"})
	require.NoError(t, err)
	require.Equal(t, "meridian-web", found.Value.Name)
	created, err := sdkgo.RunMutation(&projectTestContext{}, client.CreateProject(), testConnection, CreateProjectInput{Name: "meridian-web", TeamID: "team_123"})
	require.NoError(t, err)
	require.Equal(t, CreateProjectBranchCreated, created.Branch)
	updated, err := sdkgo.RunMutation(testContext{}, client.UpdateProject(), testConnection, UpdateProjectInput{ProjectID: "prj_meridian1", Name: "meridian-renamed", TeamID: "team_123"})
	require.NoError(t, err)
	require.Equal(t, "meridian-renamed", updated.Value.Name)
	deleted, err := sdkgo.RunMutation(testContext{}, client.DeleteProject(), testConnection, DeleteProjectInput{ProjectID: "prj_meridian1", TeamID: "team_123"})
	require.NoError(t, err)
	require.Equal(t, DeleteProjectBranchDeleted, deleted.Branch)
}
