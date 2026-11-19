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

func TestProjectDomainOperationsUseDocumentedEndpoints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "team_123", request.URL.Query().Get("teamId"))
		require.Equal(t, "Bearer test-accesstoken", request.Header.Get("Authorization"))
		switch request.Method + " " + request.URL.Path {
		case "GET /v9/projects/prj_meridian1/domains":
			_, err := writer.Write([]byte(`{"domains":[{"name":"meridian.example.com","projectId":"prj_meridian1","verified":true}],"pagination":{"next":null}}`))
			require.NoError(t, err)
		case "GET /v9/projects/prj_meridian1/domains/meridian.example.com":
			_, err := writer.Write([]byte(`{"name":"meridian.example.com","projectId":"prj_meridian1","verified":true}`))
			require.NoError(t, err)
		case "POST /v10/projects/prj_meridian1/domains":
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"name":"meridian.example.com"}`, string(body))
			_, err = writer.Write([]byte(`{"name":"meridian.example.com","projectId":"prj_meridian1","verified":false}`))
			require.NoError(t, err)
		case "PATCH /v9/projects/prj_meridian1/domains/meridian.example.com":
			body, err := io.ReadAll(request.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"gitBranch":"staging"}`, string(body))
			_, err = writer.Write([]byte(`{"name":"meridian.example.com","projectId":"prj_meridian1","gitBranch":"staging","verified":true}`))
			require.NoError(t, err)
		case "DELETE /v9/projects/prj_meridian1/domains/meridian.example.com":
			_, err := writer.Write([]byte(`{}`))
			require.NoError(t, err)
		case "POST /v9/projects/prj_meridian1/domains/meridian.example.com/verify":
			_, err := writer.Write([]byte(`{"name":"meridian.example.com","projectId":"prj_meridian1","verified":true}`))
			require.NoError(t, err)
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	domainInput := ProjectDomainInput{ProjectID: "prj_meridian1", Domain: "meridian.example.com", TeamID: "team_123"}
	listed, err := sdkgo.RunQuery(testContext{}, client.ListProjectDomains(), testConnection, ListProjectDomainsInput{ProjectID: domainInput.ProjectID, TeamID: domainInput.TeamID})
	require.NoError(t, err)
	require.Len(t, listed.Value.Domains, 1)
	found, err := sdkgo.RunQuery(testContext{}, client.GetProjectDomain(), testConnection, domainInput)
	require.NoError(t, err)
	require.Equal(t, domainInput.Domain, found.Value.Name)
	added, err := sdkgo.RunMutation(&projectTestContext{}, client.AddProjectDomain(), testConnection, domainInput)
	require.NoError(t, err)
	require.Equal(t, AddProjectDomainBranchAdded, added.Branch)
	updated, err := sdkgo.RunMutation(testContext{}, client.UpdateProjectDomain(), testConnection, UpdateProjectDomainInput{ProjectID: domainInput.ProjectID, Domain: domainInput.Domain, GitBranch: "staging", TeamID: domainInput.TeamID})
	require.NoError(t, err)
	require.Equal(t, "staging", updated.Value.GitBranch)
	removed, err := sdkgo.RunMutation(testContext{}, client.RemoveProjectDomain(), testConnection, domainInput)
	require.NoError(t, err)
	require.Equal(t, RemoveProjectDomainBranchRemoved, removed.Branch)
	verified, err := sdkgo.RunMutation(testContext{}, client.VerifyProjectDomain(), testConnection, domainInput)
	require.NoError(t, err)
	require.True(t, verified.Value.Verified)
}
