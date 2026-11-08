// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package azuredevops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testPersonalAccessToken = "synthetic-azure-devops-pat"

var testConnection = sdkgo.ConnectionRef{Provider: "azure-devops", Name: "azure-devops"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "azure-devops-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func TestListProjectsBasicAuthenticationAndPrivacy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/example/_apis/projects", r.URL.Path)
		require.Equal(t, "%24skip=20&%24top=2&api-version=7.1", r.URL.RawQuery)
		username, personalAccessToken, ok := r.BasicAuth()
		require.True(t, ok)
		require.Empty(t, username)
		require.Equal(t, testPersonalAccessToken, personalAccessToken)
		_, err := w.Write([]byte(`{"count":1,"value":[{"id":"eb6e4656-77fc-42a1-9181-4c6d8e9da5d1","name":"Example","state":"wellFormed","url":"not-persisted"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL + "/example"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {PersonalAccessToken: sdkgo.NewSecretString(testPersonalAccessToken)}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListProjects(), testConnection, ListProjectsInput{Top: 2, Skip: 20})
	require.NoError(t, err)
	require.Equal(t, ListProjectsBranchListed, result.Branch)
	require.Equal(t, []ProjectMetadata{{ID: "eb6e4656-77fc-42a1-9181-4c6d8e9da5d1", Name: "Example", State: "wellFormed"}}, result.Value.Projects)
	require.Equal(t, 2, result.Value.Top)
	require.Equal(t, 20, result.Value.Skip)
}
